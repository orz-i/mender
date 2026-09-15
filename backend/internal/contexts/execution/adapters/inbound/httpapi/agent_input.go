package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application/ports"
)

type AgentInputHandler struct {
	service       *application.AgentInputSubmissions
	authenticator ports.Authenticator
}

func NewAgentInput(service *application.AgentInputSubmissions, authenticator ports.Authenticator) (*AgentInputHandler, error) {
	if service == nil || authenticator == nil {
		return nil, errors.New("Agent input HTTP adapter requires use cases and authentication")
	}
	return &AgentInputHandler{service: service, authenticator: authenticator}, nil
}

func (h *AgentInputHandler) Register(router *gin.Engine) { h.RegisterAt(router, "/api/v1") }

func (h *AgentInputHandler) RegisterAt(router *gin.Engine, prefix string) {
	base := prefix + "/workspaces/:workspace_id/runs/:run_id/input"
	router.GET(base, h.get)
	router.POST(base, h.submit)
}

type agentInputDTO struct {
	InputRequestID string          `json:"input_request_id"`
	State          string          `json:"state"`
	Prompt         string          `json:"prompt"`
	InputSchema    json.RawMessage `json:"input_schema"`
	RequestedAt    time.Time       `json:"requested_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

func projectAgentInput(record application.AgentInputSubmissionRecord) (agentInputDTO, error) {
	schema := json.RawMessage(record.InputSchemaJSON)
	if record.InputRequestID == "" || record.Prompt == "" || len(schema) < 2 || !json.Valid(schema) || record.RequestedAt.IsZero() || record.UpdatedAt.Before(record.RequestedAt) {
		return agentInputDTO{}, application.ErrAgentInputUnavailable
	}
	var object map[string]any
	if json.Unmarshal(schema, &object) != nil || object == nil {
		return agentInputDTO{}, application.ErrAgentInputUnavailable
	}
	switch record.State {
	case "pending", "sending", "unknown", "submitted":
	default:
		return agentInputDTO{}, application.ErrAgentInputUnavailable
	}
	return agentInputDTO{InputRequestID: record.InputRequestID, State: record.State, Prompt: record.Prompt, InputSchema: schema, RequestedAt: record.RequestedAt, UpdatedAt: record.UpdatedAt}, nil
}

func failAgentInput(c *gin.Context, requestID string, err error) {
	switch {
	case errors.Is(err, application.ErrNoAgentInput):
		failure(c, requestID, http.StatusNotFound, "AGENT_INPUT_NOT_FOUND", "No supplemental input request is available for this Run.")
	case errors.Is(err, application.ErrAgentInputInvalid):
		failure(c, requestID, http.StatusBadRequest, "INVALID_AGENT_INPUT", "Supplemental input is invalid for this request.")
	case errors.Is(err, application.ErrAgentInputConflict):
		failure(c, requestID, http.StatusConflict, "AGENT_INPUT_CONFLICT", "Supplemental input no longer matches the current Run request.")
	case errors.Is(err, application.ErrAgentInputOutcomeUnknown):
		failure(c, requestID, http.StatusConflict, "AGENT_INPUT_OUTCOME_UNKNOWN", "Supplemental input delivery outcome is unknown; do not resubmit automatically.")
	case errors.Is(err, application.ErrAgentInputUnavailable):
		failure(c, requestID, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "Supplemental input service is unavailable.")
	default:
		failError(c, requestID, err)
	}
}

func (h *AgentInputHandler) get(c *gin.Context) {
	ctx, done, caller, requestID, ok := authenticateRequest(c, h.authenticator)
	defer done()
	if !ok {
		return
	}
	runID := ports.RunID(c.Param("run_id"))
	if !runID.IsValid() {
		failAgentInput(c, requestID, application.ErrAgentInputInvalid)
		return
	}
	record, err := h.service.Get(ctx, caller, runID)
	if err != nil {
		failAgentInput(c, requestID, err)
		return
	}
	dto, err := projectAgentInput(record)
	if err != nil {
		failAgentInput(c, requestID, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": dto, "meta": gin.H{"request_id": requestID}})
}

func decodeAgentInputBody(body io.Reader) (string, []byte, error) {
	decoder := json.NewDecoder(body)
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return "", nil, application.ErrAgentInputInvalid
	}
	seen := map[string]bool{}
	var requestID string
	var answer json.RawMessage
	for decoder.More() {
		token, tokenErr := decoder.Token()
		key, ok := token.(string)
		if tokenErr != nil || !ok || seen[key] {
			return "", nil, application.ErrAgentInputInvalid
		}
		seen[key] = true
		switch key {
		case "input_request_id":
			if decoder.Decode(&requestID) != nil {
				return "", nil, application.ErrAgentInputInvalid
			}
		case "answer":
			if decoder.Decode(&answer) != nil {
				return "", nil, application.ErrAgentInputInvalid
			}
		default:
			return "", nil, application.ErrAgentInputInvalid
		}
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') || len(seen) != 2 || requestID == "" || len(answer) < 2 || len(answer) > 64<<10 || !json.Valid(answer) {
		return "", nil, application.ErrAgentInputInvalid
	}
	trimmed := bytes.TrimSpace(answer)
	if len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' || bytes.Equal(trimmed, []byte("null")) {
		return "", nil, application.ErrAgentInputInvalid
	}
	var extra any
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return "", nil, application.ErrAgentInputInvalid
	}
	return requestID, append([]byte(nil), trimmed...), nil
}

func (h *AgentInputHandler) submit(c *gin.Context) {
	ctx, done, caller, requestID, ok := authenticateRequest(c, h.authenticator)
	defer done()
	if !ok {
		return
	}
	runID := ports.RunID(c.Param("run_id"))
	if !runID.IsValid() {
		failAgentInput(c, requestID, application.ErrAgentInputInvalid)
		return
	}
	media, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || media != "application/json" {
		failure(c, requestID, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "Use application/json.")
		return
	}
	body := http.MaxBytesReader(c.Writer, c.Request.Body, 70<<10)
	defer body.Close()
	inputRequestID, answer, err := decodeAgentInputBody(body)
	if err != nil {
		var max *http.MaxBytesError
		if errors.As(err, &max) {
			failure(c, requestID, http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE", "Request body exceeds limit.")
		} else {
			failAgentInput(c, requestID, application.ErrAgentInputInvalid)
		}
		return
	}
	record, err := h.service.Submit(ctx, caller, runID, inputRequestID, answer)
	if err != nil {
		failAgentInput(c, requestID, err)
		return
	}
	dto, err := projectAgentInput(record)
	if err != nil {
		failAgentInput(c, requestID, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": dto, "meta": gin.H{"request_id": requestID}})
}
