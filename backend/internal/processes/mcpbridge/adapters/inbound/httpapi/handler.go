package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

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

func New(service *application.Service) (*Handler, error) {
	if service == nil {
		return nil, application.ErrUnavailable
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "mender", Version: "v0.1.0"}, &mcp.ServerOptions{Capabilities: &mcp.ServerCapabilities{}})
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
