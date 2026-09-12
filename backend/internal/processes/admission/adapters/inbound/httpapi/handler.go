package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/orz-i/mender/backend/internal/processes/admission/application"
)

const maxStartBody = 72 * 1024

type Handler struct {
	service       *application.Service
	authenticator application.Authenticator
}

func New(service *application.Service, authenticator application.Authenticator) (*Handler, error) {
	if service == nil || authenticator == nil {
		return nil, application.ErrUnavailable
	}
	return &Handler{service: service, authenticator: authenticator}, nil
}

func (h *Handler) Register(router *gin.Engine) {
	h.RegisterAt(router, "/api/v1")
}

func (h *Handler) RegisterAt(router *gin.Engine, prefix string) {
	router.POST(prefix+"/workspaces/:workspace_id/runs", h.startRun)
}

type toolRef struct{ ToolID, Version string }
type money struct{ Currency, AmountMicro string }
type startRequest struct {
	ToolRef                 toolRef
	ToolsetID, ConnectionID string
	Arguments               json.RawMessage
	MaxCharge               money
	WaitMS                  int
}

func strictFields(raw []byte, allowed map[string]func(json.RawMessage) error) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	start, err := d.Token()
	if err != nil || start != json.Delim('{') {
		return application.ErrInvalid
	}
	seen := make(map[string]bool, len(allowed))
	for d.More() {
		keyToken, err := d.Token()
		if err != nil {
			return application.ErrInvalid
		}
		key, ok := keyToken.(string)
		fn, permitted := allowed[key]
		if !ok || !permitted || seen[key] {
			return application.ErrInvalid
		}
		seen[key] = true
		var value json.RawMessage
		if err = d.Decode(&value); err != nil || len(value) == 0 {
			return application.ErrInvalid
		}
		if err = fn(value); err != nil {
			return err
		}
	}
	end, err := d.Token()
	if err != nil || end != json.Delim('}') {
		return application.ErrInvalid
	}
	var extra any
	if err = d.Decode(&extra); err != io.EOF {
		return application.ErrInvalid
	}
	return nil
}

func decodeString(raw json.RawMessage, target *string) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, target) != nil {
		return application.ErrInvalid
	}
	return nil
}

func decodeToolRef(raw json.RawMessage, target *toolRef) error {
	seen := map[string]bool{}
	err := strictFields(raw, map[string]func(json.RawMessage) error{
		"tool_id": func(v json.RawMessage) error { seen["tool_id"] = true; return decodeString(v, &target.ToolID) },
		"version": func(v json.RawMessage) error { seen["version"] = true; return decodeString(v, &target.Version) },
	})
	if err != nil || !seen["tool_id"] || !seen["version"] {
		return application.ErrInvalid
	}
	return nil
}

func decodeMoney(raw json.RawMessage, target *money) error {
	seen := map[string]bool{}
	err := strictFields(raw, map[string]func(json.RawMessage) error{
		"currency": func(v json.RawMessage) error { seen["currency"] = true; return decodeString(v, &target.Currency) },
		"amount_micro": func(v json.RawMessage) error {
			seen["amount_micro"] = true
			return decodeString(v, &target.AmountMicro)
		},
	})
	if err != nil || !seen["currency"] || !seen["amount_micro"] {
		return application.ErrInvalid
	}
	return nil
}

func decodeStart(raw []byte) (startRequest, error) {
	var q startRequest
	seen := map[string]bool{}
	err := strictFields(raw, map[string]func(json.RawMessage) error{
		"tool_ref":      func(v json.RawMessage) error { seen["tool_ref"] = true; return decodeToolRef(v, &q.ToolRef) },
		"toolset_id":    func(v json.RawMessage) error { seen["toolset_id"] = true; return decodeString(v, &q.ToolsetID) },
		"connection_id": func(v json.RawMessage) error { seen["connection_id"] = true; return decodeString(v, &q.ConnectionID) },
		"arguments": func(v json.RawMessage) error {
			seen["arguments"] = true
			q.Arguments = append(q.Arguments[:0], v...)
			return nil
		},
		"max_charge": func(v json.RawMessage) error { seen["max_charge"] = true; return decodeMoney(v, &q.MaxCharge) },
		"wait_ms": func(v json.RawMessage) error {
			seen["wait_ms"] = true
			if bytes.Equal(bytes.TrimSpace(v), []byte("null")) || json.Unmarshal(v, &q.WaitMS) != nil || q.WaitMS < 0 || q.WaitMS > 5000 {
				return application.ErrInvalid
			}
			return nil
		},
	})
	if err != nil {
		return startRequest{}, err
	}
	for _, key := range []string{"tool_ref", "toolset_id", "connection_id", "arguments", "max_charge"} {
		if !seen[key] {
			return startRequest{}, application.ErrInvalid
		}
	}
	return q, nil
}

func failure(c *gin.Context, requestID string, status int, code, message string) {
	if status == http.StatusUnauthorized {
		c.Header("WWW-Authenticate", `Bearer realm="mender"`)
	}
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}, "meta": gin.H{"request_id": requestID, "trace_id": requestID}})
}

func failError(c *gin.Context, requestID string, err error) {
	switch {
	case errors.Is(err, application.ErrUnauthenticated):
		failure(c, requestID, 401, "UNAUTHENTICATED", "A valid Run credential is required.")
	case errors.Is(err, application.ErrForbidden):
		failure(c, requestID, 403, "FORBIDDEN", "Operation is not permitted.")
	case errors.Is(err, application.ErrConflict):
		failure(c, requestID, 409, "IDEMPOTENCY_CONFLICT", "Idempotency key was already used for a different request.")
	case errors.Is(err, application.ErrBudgetExceeded):
		failure(c, requestID, 429, "BUDGET_EXCEEDED", "The active budget cannot reserve this run.")
	case errors.Is(err, application.ErrBudgetUnavailable):
		failure(c, requestID, 409, "BUDGET_UNAVAILABLE", "No valid budget window is available for this run.")
	case errors.Is(err, application.ErrCommitUnconfirmed):
		failure(c, requestID, 503, "ADMISSION_COMMIT_UNCONFIRMED", "Admission commit is unconfirmed; retry with the same idempotency key.")
	case errors.Is(err, application.ErrInvalid):
		failure(c, requestID, 400, "INVALID_ARGUMENT", "Invalid run request.")
	case errors.Is(err, context.DeadlineExceeded):
		failure(c, requestID, 504, "TIMEOUT", "Request deadline exceeded.")
	default:
		failure(c, requestID, 503, "DEPENDENCY_UNAVAILABLE", "A required service is unavailable.")
	}
}

func beginRequest(c *gin.Context, authenticator application.Authenticator) (context.Context, context.CancelFunc, application.Caller, string, bool) {
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		failure(c, "", 503, "DEPENDENCY_UNAVAILABLE", "Request identifier unavailable.")
		return ctx, cancel, application.Caller{}, "", false
	}
	requestID := hex.EncodeToString(random[:])
	c.Header("X-Request-ID", requestID)
	values := c.Request.Header.Values("Authorization")
	if len(values) != 1 || len(values[0]) > 256 {
		failError(c, requestID, application.ErrUnauthenticated)
		return ctx, cancel, application.Caller{}, requestID, false
	}
	parts := strings.Fields(values[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		failError(c, requestID, application.ErrUnauthenticated)
		return ctx, cancel, application.Caller{}, requestID, false
	}
	caller, err := authenticator.Authenticate(ctx, parts[1])
	if err != nil {
		failError(c, requestID, err)
		return ctx, cancel, application.Caller{}, requestID, false
	}
	workspace := c.Param("workspace_id")
	if !application.ValidID(workspace) {
		failError(c, requestID, application.ErrInvalid)
		return ctx, cancel, application.Caller{}, requestID, false
	}
	if caller.WorkspaceID != workspace {
		failError(c, requestID, application.ErrForbidden)
		return ctx, cancel, application.Caller{}, requestID, false
	}
	return ctx, cancel, caller, requestID, true
}

func (h *Handler) startRun(c *gin.Context) {
	ctx, cancel, caller, requestID, ok := beginRequest(c, h.authenticator)
	defer cancel()
	if !ok {
		return
	}
	keys := c.Request.Header.Values("Idempotency-Key")
	if len(keys) != 1 {
		failError(c, requestID, application.ErrInvalid)
		return
	}
	media, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || media != "application/json" {
		failure(c, requestID, 415, "UNSUPPORTED_MEDIA_TYPE", "Use application/json.")
		return
	}
	body := http.MaxBytesReader(c.Writer, c.Request.Body, maxStartBody)
	defer body.Close()
	raw, err := io.ReadAll(body)
	if err != nil {
		var max *http.MaxBytesError
		if errors.As(err, &max) {
			failure(c, requestID, 413, "BODY_TOO_LARGE", "Request body exceeds limit.")
		} else {
			failError(c, requestID, application.ErrInvalid)
		}
		return
	}
	input, err := decodeStart(raw)
	if err != nil {
		failError(c, requestID, err)
		return
	}
	receipt, err := h.service.Admit(ctx, caller, application.Request{
		IdempotencyKey:   keys[0],
		ToolID:           input.ToolRef.ToolID,
		ToolVersion:      input.ToolRef.Version,
		ToolsetVersionID: input.ToolsetID,
		ConnectionID:     input.ConnectionID,
		Currency:         input.MaxCharge.Currency,
		MaxChargeMicro:   input.MaxCharge.AmountMicro,
		Arguments:        input.Arguments,
	})
	if err != nil {
		failError(c, requestID, err)
		return
	}
	status := http.StatusAccepted
	if receipt.Replayed {
		status = http.StatusOK
	}
	c.JSON(status, gin.H{
		"data": gin.H{
			"run_id":          receipt.RunID,
			"execution_state": "queued",
			"billing_state":   "reserved",
			"status_url":      "/api/v1/workspaces/" + receipt.WorkspaceID + "/runs/" + receipt.RunID,
		},
		"meta": gin.H{"request_id": requestID, "trace_id": requestID},
	})
}
