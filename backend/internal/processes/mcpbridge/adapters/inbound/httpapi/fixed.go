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

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/orz-i/mender/backend/internal/processes/mcpbridge/application"
)

var acceptedRunSchema = json.RawMessage(`{"type":"object","additionalProperties":false,"required":["run_id","submission_state","replayed","currency","reserved_micro"],"properties":{"run_id":{"type":"string"},"submission_state":{"const":"accepted"},"replayed":{"type":"boolean"},"currency":{"type":"string"},"reserved_micro":{"type":"string"}}}`)

var reservedDirectFields = map[string]bool{
	"_mender": true, "tool_id": true, "tool_version": true, "toolset_id": true,
	"connection_id": true, "price_version_id": true, "budget_id": true,
	"deployment_revision": true, "currency": true, "max_charge_micro": true,
	"idempotency_key": true,
}

type directControl struct {
	IdempotencyKey string `json:"idempotency_key"`
	Currency       string `json:"currency"`
	MaxChargeMicro string `json:"max_charge_micro"`
}

type FixedHandler struct{ service *application.FixedService }

func NewFixed(service *application.FixedService) (*FixedHandler, error) {
	if service == nil {
		return nil, application.ErrUnavailable
	}
	return &FixedHandler{service: service}, nil
}

func fixedWorkspace(path string) (workspaceID, toolsetID string, ok bool) {
	if !strings.HasPrefix(path, basePath) {
		return "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(path, basePath), "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] != "toolsets" || parts[2] == "" {
		return "", "", false
	}
	return parts[0], parts[2], true
}

func directSchema(tool application.DirectTool) (json.RawMessage, error) {
	var published jsonschema.Schema
	if err := json.Unmarshal([]byte(tool.InputSchema), &published); err != nil {
		return nil, application.ErrUnavailable
	}
	// No Loader is supplied: any nested remote $ref fails closed and can never
	// trigger network access from tools/list or tools/call.
	if _, err := published.Resolve(nil); err != nil {
		return nil, application.ErrUnavailable
	}
	var schema map[string]any
	if err := json.Unmarshal([]byte(tool.InputSchema), &schema); err != nil || schema == nil || schema["type"] != "object" {
		return nil, application.ErrUnavailable
	}
	if _, exists := schema["$ref"]; exists {
		return nil, application.ErrUnavailable
	}
	for _, key := range []string{"allOf", "anyOf", "oneOf", "not"} {
		if _, exists := schema[key]; exists {
			return nil, application.ErrUnavailable
		}
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		if schema["properties"] != nil {
			return nil, application.ErrUnavailable
		}
		properties = map[string]any{}
		schema["properties"] = properties
	}
	for name := range properties {
		if reservedDirectFields[name] {
			return nil, application.ErrUnavailable
		}
	}
	properties["_mender"] = map[string]any{
		"type": "object", "additionalProperties": false,
		"required": []string{"idempotency_key", "currency", "max_charge_micro"},
		"properties": map[string]any{
			"idempotency_key":  map[string]any{"type": "string", "minLength": 8, "maxLength": 200},
			"currency":         map[string]any{"type": "string", "pattern": "^[A-Z]{3}$"},
			"max_charge_micro": map[string]any{"type": "string", "pattern": "^(0|[1-9][0-9]{0,18})$"},
		},
	}
	required := []any{}
	if existing, exists := schema["required"]; exists {
		list, ok := existing.([]any)
		if !ok {
			return nil, application.ErrUnavailable
		}
		required = append(required, list...)
	}
	for _, item := range required {
		name, ok := item.(string)
		if !ok || reservedDirectFields[name] {
			return nil, application.ErrUnavailable
		}
	}
	required = append(required, "_mender")
	schema["required"] = required
	b, err := json.Marshal(schema)
	if err != nil || len(b) > 1<<20 {
		return nil, application.ErrUnavailable
	}
	return b, nil
}

func rawObject(raw any) (map[string]json.RawMessage, error) {
	b, ok := raw.(json.RawMessage)
	if !ok || len(b) < 2 || len(b) > 1<<20 {
		return nil, application.ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(b))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, application.ErrInvalid
	}
	result := map[string]json.RawMessage{}
	for d.More() {
		name, err := d.Token()
		key, ok := name.(string)
		if err != nil || !ok {
			return nil, application.ErrInvalid
		}
		if _, exists := result[key]; exists {
			return nil, application.ErrInvalid
		}
		var value json.RawMessage
		if err = d.Decode(&value); err != nil {
			return nil, application.ErrInvalid
		}
		result[key] = append(json.RawMessage(nil), value...)
	}
	if token, err = d.Token(); err != nil || token != json.Delim('}') {
		return nil, application.ErrInvalid
	}
	var trailing any
	if err = d.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, application.ErrInvalid
	}
	return result, nil
}

func directArguments(raw any) (directControl, []byte, error) {
	object, err := rawObject(raw)
	if err != nil {
		return directControl{}, nil, err
	}
	controlRaw, exists := object["_mender"]
	if !exists {
		return directControl{}, nil, application.ErrInvalid
	}
	var control directControl
	if err = decodeStrict(controlRaw, &control); err != nil {
		return directControl{}, nil, err
	}
	delete(object, "_mender")
	for name := range object {
		if reservedDirectFields[name] {
			return directControl{}, nil, application.ErrInvalid
		}
	}
	arguments, err := json.Marshal(object)
	if err != nil || len(arguments) > 65536 {
		return directControl{}, nil, application.ErrInvalid
	}
	return control, arguments, nil
}

func validateBusinessArguments(schemaJSON string, arguments []byte) error {
	var schema jsonschema.Schema
	if err := json.Unmarshal([]byte(schemaJSON), &schema); err != nil {
		return application.ErrUnavailable
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return application.ErrUnavailable
	}
	decoder := json.NewDecoder(bytes.NewReader(arguments))
	var instance any
	if err = decoder.Decode(&instance); err != nil {
		return application.ErrInvalid
	}
	if err = resolved.Validate(instance); err != nil {
		return application.ErrInvalid
	}
	return nil
}

func pointer(value bool) *bool { return &value }

func directToolDefinition(tool application.DirectTool, inputSchema json.RawMessage) *mcp.Tool {
	readOnly := tool.SideEffect == "read_only"
	destructive := !readOnly
	openWorld := true
	idempotent := tool.Idempotency == "safe_read" || tool.Idempotency == "idempotent"
	var declared any
	_ = json.Unmarshal([]byte(tool.OutputSchema), &declared)
	return &mcp.Tool{
		Name: tool.Name, Title: tool.Title,
		Description: tool.Description + "\n\nStarts an asynchronous Mender Run. The result is a Run receipt; use the Mender Run/Artifact meta-tools to inspect completion.",
		InputSchema: inputSchema, OutputSchema: acceptedRunSchema,
		Annotations: &mcp.ToolAnnotations{Title: tool.Title, ReadOnlyHint: readOnly, IdempotentHint: idempotent, DestructiveHint: pointer(destructive), OpenWorldHint: pointer(openWorld)},
		Meta:        mcp.Meta{"mender/resultMode": "async_run_artifact", "mender/declaredResultSchema": declared, "mender/toolVersion": tool.ToolVersion},
	}
}

func (h *FixedHandler) server(ctx context.Context, caller application.Caller, toolsetID string) (*mcp.Server, error) {
	tools, err := h.service.ListTools(ctx, caller, toolsetID)
	if err != nil {
		return nil, err
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "mender-fixed-toolset", Version: "v0.1.0"}, &mcp.ServerOptions{Capabilities: &mcp.ServerCapabilities{}})
	for _, tool := range tools {
		tool := tool
		schema, err := directSchema(tool)
		if err != nil {
			return nil, err
		}
		server.AddTool(directToolDefinition(tool, schema), func(callCtx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			requestCaller, err := requestCaller(callCtx)
			if err != nil {
				return toolError(err), nil
			}
			if req == nil || req.Params == nil {
				return toolError(application.ErrInvalid), nil
			}
			control, arguments, err := directArguments(req.Params.Arguments)
			if err != nil {
				return toolError(err), nil
			}
			if err = validateBusinessArguments(tool.InputSchema, arguments); err != nil {
				return toolError(err), nil
			}
			receipt, err := h.service.StartTool(callCtx, requestCaller, toolsetID, tool.Name, control.IdempotencyKey, control.Currency, control.MaxChargeMicro, arguments)
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return nil, err
				}
				return toolError(err), nil
			}
			return success(map[string]any{"run_id": receipt.RunID, "submission_state": "accepted", "currency": receipt.Currency, "reserved_micro": strconv.FormatInt(receipt.ReservedMicro, 10), "replayed": receipt.Replayed}), nil
		})
	}
	return server, nil
}

func (h *FixedHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if h == nil || h.service == nil {
		failure(w, http.StatusServiceUnavailable, "MCP_UNAVAILABLE")
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		failure(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED")
		return
	}
	workspaceID, toolsetID, ok := fixedWorkspace(r.URL.Path)
	if !ok || r.URL.RawQuery != "" {
		failure(w, http.StatusNotFound, "MCP_ENDPOINT_NOT_FOUND")
		return
	}
	if r.Header.Get("Mcp-Protocol-Version") != ProtocolVersion {
		failure(w, http.StatusBadRequest, "MCP_PROTOCOL_VERSION_REQUIRED")
		return
	}
	token, ok := bearer(r.Header)
	if !ok {
		failure(w, http.StatusUnauthorized, "UNAUTHENTICATED")
		return
	}
	caller, err := h.service.Authenticate(r.Context(), token, workspaceID)
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
	server, err := h.server(r.Context(), caller, toolsetID)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			failure(w, http.StatusRequestTimeout, "REQUEST_CANCELED")
			return
		}
		if errors.Is(err, application.ErrUnauthenticated) {
			failure(w, http.StatusUnauthorized, "UNAUTHENTICATED")
			return
		}
		if errors.Is(err, application.ErrForbidden) {
			failure(w, http.StatusForbidden, "FORBIDDEN")
			return
		}
		failure(w, http.StatusServiceUnavailable, "MCP_TOOLSET_UNAVAILABLE")
		return
	}
	transport := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, MaxRequestBodyBytes: maxRequestBody, PropagateRequestCancellation: true})
	r = r.WithContext(context.WithValue(r.Context(), callerKey{}, caller))
	transport.ServeHTTP(w, r)
}
