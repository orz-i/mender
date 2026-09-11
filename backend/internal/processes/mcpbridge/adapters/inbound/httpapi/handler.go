package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/orz-i/mender/backend/internal/processes/mcpbridge/application"
)

const (
	ProtocolVersion = "2026-07-28"
	basePath        = "/mcp/v1/workspaces/"
	maxRequestBody  = 1 << 20
)

type callerKey struct{}

type Handler struct {
	service *application.Service
	mcp     http.Handler
}

var (
	startSchema    = json.RawMessage(`{"type":"object","additionalProperties":false,"required":["idempotency_key","tool_id","tool_version","toolset_id","connection_id","arguments","currency","max_charge_micro"],"properties":{"idempotency_key":{"type":"string","minLength":8,"maxLength":200},"tool_id":{"type":"string","minLength":1,"maxLength":128},"tool_version":{"type":"string","minLength":1,"maxLength":128},"toolset_id":{"type":"string","minLength":1,"maxLength":128},"connection_id":{"type":"string","minLength":1,"maxLength":128},"arguments":{"type":"object"},"currency":{"type":"string","pattern":"^[A-Z]{3}$"},"max_charge_micro":{"type":"string","pattern":"^(0|[1-9][0-9]{0,18})$"}}}`)
	runSchema      = json.RawMessage(`{"type":"object","additionalProperties":false,"required":["run_id"],"properties":{"run_id":{"type":"string","minLength":1,"maxLength":128}}}`)
	cancelSchema   = json.RawMessage(`{"type":"object","additionalProperties":false,"required":["run_id"],"properties":{"run_id":{"type":"string","minLength":1,"maxLength":128},"reason":{"type":"string","maxLength":500}}}`)
	artifactSchema = json.RawMessage(`{"type":"object","additionalProperties":false,"required":["run_id","artifact_id"],"properties":{"run_id":{"type":"string","minLength":1,"maxLength":128},"artifact_id":{"type":"string","minLength":1,"maxLength":132}}}`)
)

type startInput struct {
	IdempotencyKey string          `json:"idempotency_key"`
	ToolID         string          `json:"tool_id"`
	ToolVersion    string          `json:"tool_version"`
	ToolsetID      string          `json:"toolset_id"`
	ConnectionID   string          `json:"connection_id"`
	Arguments      json.RawMessage `json:"arguments"`
	Currency       string          `json:"currency"`
	MaxChargeMicro string          `json:"max_charge_micro"`
}
type runInput struct {
	RunID string `json:"run_id"`
}
type cancelInput struct {
	RunID  string `json:"run_id"`
	Reason string `json:"reason,omitempty"`
}
type artifactInput struct {
	RunID      string `json:"run_id"`
	ArtifactID string `json:"artifact_id"`
}

func decodeStrict(raw any, out any) error {
	b, ok := raw.(json.RawMessage)
	if !ok || len(b) < 2 || len(b) > 1<<20 || b[0] != '{' {
		return application.ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return application.ErrInvalid
	}
	var trailing any
	if err := d.Decode(&trailing); !errors.Is(err, io.EOF) {
		return application.ErrInvalid
	}
	return nil
}

func toolError(err error) *mcp.CallToolResult {
	code := "UNAVAILABLE"
	switch {
	case errors.Is(err, application.ErrInvalid):
		code = "INVALID_ARGUMENT"
	case errors.Is(err, application.ErrUnauthenticated):
		code = "UNAUTHENTICATED"
	case errors.Is(err, application.ErrForbidden):
		code = "FORBIDDEN"
	case errors.Is(err, application.ErrNotFound):
		code = "NOT_FOUND"
	case errors.Is(err, application.ErrConflict):
		code = "IDEMPOTENCY_CONFLICT"
	case errors.Is(err, application.ErrBudgetExceeded):
		code = "BUDGET_EXCEEDED"
	case errors.Is(err, application.ErrOutcomeUnconfirmed):
		code = "OUTCOME_UNCONFIRMED"
	case errors.Is(err, application.ErrCommitUnconfirmed):
		code = "COMMIT_UNCONFIRMED"
	}
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: code}}}
}

func success(value map[string]any) *mcp.CallToolResult {
	b, _ := json.Marshal(value)
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}, StructuredContent: value}
}

func requestCaller(ctx context.Context) (application.Caller, error) {
	c, ok := ctx.Value(callerKey{}).(application.Caller)
	if !ok || c.WorkspaceID == "" || c.SubjectID == "" || c.CredentialID == "" {
		return application.Caller{}, application.ErrUnauthenticated
	}
	return c, nil
}

func registerTools(server *mcp.Server, service *application.Service) {
	server.AddTool(&mcp.Tool{Name: "mender_run_start", Title: "Start Mender Run", Description: "Durably starts an authorized Mender tool run. Reuse idempotency_key when retrying the same logical operation.", InputSchema: startSchema}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		caller, err := requestCaller(ctx)
		if err != nil {
			return toolError(err), nil
		}
		var in startInput
		if req == nil || req.Params == nil || decodeStrict(req.Params.Arguments, &in) != nil {
			return toolError(application.ErrInvalid), nil
		}
		receipt, err := service.Start(ctx, caller, application.StartRequest{IdempotencyKey: in.IdempotencyKey, ToolID: in.ToolID, ToolVersion: in.ToolVersion, ToolsetVersionID: in.ToolsetID, ConnectionID: in.ConnectionID, Arguments: append([]byte(nil), in.Arguments...), Currency: in.Currency, MaxChargeMicro: in.MaxChargeMicro})
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			return toolError(err), nil
		}
		return success(map[string]any{"run_id": receipt.RunID, "execution_state": "queued", "billing_state": "reserved", "currency": receipt.Currency, "reserved_micro": strconv.FormatInt(receipt.ReservedMicro, 10), "replayed": receipt.Replayed}), nil
	})

	server.AddTool(&mcp.Tool{Name: "mender_run_get", Title: "Get Mender Run", Description: "Reads the current authorized state of a Mender run.", InputSchema: runSchema}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		caller, err := requestCaller(ctx)
		if err != nil {
			return toolError(err), nil
		}
		var in runInput
		if req == nil || req.Params == nil || decodeStrict(req.Params.Arguments, &in) != nil {
			return toolError(application.ErrInvalid), nil
		}
		run, err := service.GetRun(ctx, caller, in.RunID)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			return toolError(err), nil
		}
		return success(map[string]any{"run_id": run.RunID, "execution_state": run.State, "version": strconv.FormatUint(run.Version, 10), "created_at": run.CreatedAt.UTC().Format(time.RFC3339Nano), "updated_at": run.UpdatedAt.UTC().Format(time.RFC3339Nano)}), nil
	})

	server.AddTool(&mcp.Tool{Name: "mender_run_cancel", Title: "Cancel Mender Run", Description: "Requests cancellation using Mender's existing coordinated/provider cancellation semantics; acceptance does not claim a remote provider has already stopped.", InputSchema: cancelSchema}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		caller, err := requestCaller(ctx)
		if err != nil {
			return toolError(err), nil
		}
		var in cancelInput
		if req == nil || req.Params == nil || decodeStrict(req.Params.Arguments, &in) != nil {
			return toolError(application.ErrInvalid), nil
		}
		run, err := service.CancelRun(ctx, caller, in.RunID, in.Reason)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			return toolError(err), nil
		}
		return success(map[string]any{"run_id": run.RunID, "execution_state": run.State, "version": strconv.FormatUint(run.Version, 10), "updated_at": run.UpdatedAt.UTC().Format(time.RFC3339Nano)}), nil
	})

	server.AddTool(&mcp.Tool{Name: "mender_artifact_get", Title: "Get Mender Artifact", Description: "Reads a bounded inline JSON result Artifact after rechecking run:read authorization.", InputSchema: artifactSchema}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		caller, err := requestCaller(ctx)
		if err != nil {
			return toolError(err), nil
		}
		var in artifactInput
		if req == nil || req.Params == nil || decodeStrict(req.Params.Arguments, &in) != nil {
			return toolError(application.ErrInvalid), nil
		}
		artifact, err := service.GetArtifact(ctx, caller, in.RunID, in.ArtifactID)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			return toolError(err), nil
		}
		return success(map[string]any{"artifact_id": artifact.ArtifactID, "kind": artifact.Kind, "media_type": artifact.MediaType, "size_bytes": strconv.FormatInt(artifact.SizeBytes, 10), "created_at": artifact.CreatedAt.UTC().Format(time.RFC3339Nano), "content_json": artifact.ContentJSON}), nil
	})
}

func New(service *application.Service) (*Handler, error) {
	if service == nil {
		return nil, application.ErrUnavailable
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "mender", Version: "v0.1.0"}, &mcp.ServerOptions{Capabilities: &mcp.ServerCapabilities{}})
	registerTools(server, service)
	transport := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		Stateless: true, JSONResponse: true, MaxRequestBodyBytes: maxRequestBody, PropagateRequestCancellation: true,
	})
	return &Handler{service: service, mcp: transport}, nil
}

func workspace(path string) (string, bool) {
	if !strings.HasPrefix(path, basePath) {
		return "", false
	}
	v := strings.TrimPrefix(path, basePath)
	return v, v != "" && !strings.Contains(v, "/")
}

func bearer(header http.Header) (string, bool) {
	values := header.Values("Authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		return "", false
	}
	token := strings.TrimPrefix(values[0], "Bearer ")
	return token, token != "" && !strings.ContainsAny(token, " \t\r\n")
}

func failure(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code}})
}

func callerFromContext(r *http.Request) (application.Caller, bool) {
	c, ok := r.Context().Value(callerKey{}).(application.Caller)
	return c, ok
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if h == nil || h.service == nil || h.mcp == nil {
		failure(w, http.StatusServiceUnavailable, "MCP_UNAVAILABLE")
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		failure(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED")
		return
	}
	ws, ok := workspace(r.URL.Path)
	if !ok || r.URL.RawQuery != "" {
		failure(w, http.StatusNotFound, "MCP_ENDPOINT_NOT_FOUND")
		return
	}
	// Stable SDK v1.7.0 supports older MCP revisions for compatibility but does
	// not expose a server-side SupportedProtocolVersions option. Mender's first
	// certified HTTP surface intentionally accepts only the 2026-07-28 header.
	if r.Header.Get("Mcp-Protocol-Version") != ProtocolVersion {
		failure(w, http.StatusBadRequest, "MCP_PROTOCOL_VERSION_REQUIRED")
		return
	}
	token, ok := bearer(r.Header)
	if !ok {
		failure(w, http.StatusUnauthorized, "UNAUTHENTICATED")
		return
	}
	caller, err := h.service.Authenticate(r.Context(), token, ws)
	if err != nil {
		switch {
		case errors.Is(err, application.ErrUnauthenticated):
			failure(w, http.StatusUnauthorized, "UNAUTHENTICATED")
		case errors.Is(err, application.ErrForbidden), errors.Is(err, application.ErrInvalid):
			failure(w, http.StatusForbidden, "FORBIDDEN")
		default:
			failure(w, http.StatusServiceUnavailable, "MCP_UNAVAILABLE")
		}
		return
	}
	r = r.WithContext(context.WithValue(r.Context(), callerKey{}, caller))
	h.mcp.ServeHTTP(w, r)
}
