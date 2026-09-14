package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mcphttp "github.com/orz-i/mender/backend/internal/processes/mcpbridge/adapters/inbound/httpapi"
	"github.com/orz-i/mender/backend/internal/processes/mcpbridge/application"
)

const selectedLegacyProtocol = "2025-11-25"

func initializeBody(version string) string {
	return `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"` + version + `","capabilities":{},"clientInfo":{"name":"g3-matrix","version":"v0.1.0"}}}`
}

func certifiedMatrixHandlers(t *testing.T, authCalls *int) map[string]http.Handler {
	t.Helper()
	auth := authFunc(func(_ context.Context, token string) (application.Caller, error) {
		*authCalls = *authCalls + 1
		if token != "matrix-secret" {
			return application.Caller{}, application.ErrUnauthenticated
		}
		return application.Caller{WorkspaceID: "ws_matrix", SubjectID: "sa_matrix", CredentialID: "key_matrix"}, nil
	})
	meta, err := mcphttp.New(service(t, auth))
	if err != nil {
		t.Fatal(err)
	}
	fixedService, err := application.NewFixed(auth, authorizeFunc(func(context.Context, application.Caller, string) error { return nil }), &captureStarter{}, &directRegistry{items: []application.DirectTool{reviewedDirectTool()}})
	if err != nil {
		t.Fatal(err)
	}
	fixed, err := mcphttp.NewFixed(fixedService)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]http.Handler{"meta_tools": meta, "fixed_toolset": fixed}
}

func TestCertifiedMCPDistributionCompatibilityMatrix(t *testing.T) {
	for name, path := range map[string]string{
		"meta_tools":    "/mcp/v1/workspaces/ws_matrix",
		"fixed_toolset": "/mcp/v1/workspaces/ws_matrix/toolsets/set_matrix_v1",
	} {
		t.Run(name, func(t *testing.T) {
			var authCalls int
			h := certifiedMatrixHandlers(t, &authCalls)[name]
			request := func(headerVersion, body, authorization string) *httptest.ResponseRecorder {
				r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
				if headerVersion != "" {
					r.Header.Set("Mcp-Protocol-Version", headerVersion)
				}
				if authorization != "" {
					r.Header.Set("Authorization", authorization)
				}
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				return w
			}

			for _, tc := range []struct {
				title, headerVersion, body, authorization, errorCode string
				status                                               int
			}{
				{"missing protocol header", "", initializeBody(mcphttp.ProtocolVersion), "Bearer matrix-secret", "MCP_PROTOCOL_VERSION_REQUIRED", http.StatusBadRequest},
				{"selected legacy protocol rejected", selectedLegacyProtocol, initializeBody(selectedLegacyProtocol), "Bearer matrix-secret", "MCP_PROTOCOL_VERSION_REQUIRED", http.StatusBadRequest},
				{"current header cannot mask legacy initialize body", mcphttp.ProtocolVersion, initializeBody(selectedLegacyProtocol), "Bearer matrix-secret", "MCP_PROTOCOL_VERSION_MISMATCH", http.StatusBadRequest},
				{"legacy header cannot mask current initialize body", selectedLegacyProtocol, initializeBody(mcphttp.ProtocolVersion), "Bearer matrix-secret", "MCP_PROTOCOL_VERSION_REQUIRED", http.StatusBadRequest},
				{"authentication remains after protocol boundary", mcphttp.ProtocolVersion, initializeBody(mcphttp.ProtocolVersion), "Bearer wrong", "UNAUTHENTICATED", http.StatusUnauthorized},
			} {
				t.Run(tc.title, func(t *testing.T) {
					before := authCalls
					w := request(tc.headerVersion, tc.body, tc.authorization)
					if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.errorCode) {
						t.Fatal("compatibility matrix response drift", w.Code, w.Body.String())
					}
					if tc.errorCode == "UNAUTHENTICATED" {
						if authCalls != before+1 {
							t.Fatal("certified protocol request did not reach authentication exactly once", before, authCalls)
						}
					} else if authCalls != before {
						t.Fatal("unsupported protocol reached authentication", before, authCalls)
					}
				})
			}
		})
	}
}
