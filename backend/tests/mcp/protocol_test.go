package mcp_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	mcphttp "github.com/orz-i/mender/backend/internal/processes/mcpbridge/adapters/inbound/httpapi"
	"github.com/orz-i/mender/backend/internal/processes/mcpbridge/application"
)

type authFunc func(context.Context, string) (application.Caller, error)

func (f authFunc) Authenticate(ctx context.Context, token string) (application.Caller, error) {
	return f(ctx, token)
}

type starterFunc func(context.Context, application.Caller, application.StartRequest) (application.StartReceipt, error)

func (f starterFunc) Start(ctx context.Context, c application.Caller, q application.StartRequest) (application.StartReceipt, error) {
	return f(ctx, c, q)
}

type runsStub struct{}

func (runsStub) GetRun(context.Context, application.Caller, string) (application.Run, error) {
	return application.Run{}, application.ErrNotFound
}
func (runsStub) CancelRun(context.Context, application.Caller, string, string) (application.Run, error) {
	return application.Run{}, application.ErrNotFound
}
func (runsStub) GetArtifact(context.Context, application.Caller, string, string) (application.Artifact, error) {
	return application.Artifact{}, application.ErrNotFound
}

func service(t *testing.T, auth application.Authenticator) *application.Service {
	t.Helper()
	s, err := application.New(auth, starterFunc(func(context.Context, application.Caller, application.StartRequest) (application.StartReceipt, error) {
		return application.StartReceipt{}, nil
	}), runsStub{})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestBridgeAuthenticationBindsMachineCredentialToWorkspace(t *testing.T) {
	var calls int
	s := service(t, authFunc(func(_ context.Context, token string) (application.Caller, error) {
		calls++
		if token != "secret" {
			return application.Caller{}, application.ErrUnauthenticated
		}
		return application.Caller{WorkspaceID: "ws_a", SubjectID: "sa_a", CredentialID: "key_a"}, nil
	}))
	caller, err := s.Authenticate(context.Background(), "secret", "ws_a")
	if err != nil || caller.WorkspaceID != "ws_a" || calls != 1 {
		t.Fatal(caller, err, calls)
	}
	if _, err = s.Authenticate(context.Background(), "secret", "ws_b"); !errors.Is(err, application.ErrForbidden) || calls != 2 {
		t.Fatal("cross-workspace credential accepted", err, calls)
	}
	for _, tc := range []struct{ token, workspace string }{{"", "ws_a"}, {"secret", "../ws"}, {"secret", "ws/a"}} {
		before := calls
		if _, err = s.Authenticate(context.Background(), tc.token, tc.workspace); !errors.Is(err, application.ErrInvalid) || calls != before {
			t.Fatal("invalid endpoint reached authenticator", tc, err, calls, before)
		}
	}
}

type bearerTransport struct {
	base  http.RoundTripper
	token string
}

func (t bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header = req.Header.Clone()
	clone.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(clone)
}

func TestStatelessOfficialSDKNegotiates20260728AfterMenderAuthentication(t *testing.T) {
	s := service(t, authFunc(func(_ context.Context, token string) (application.Caller, error) {
		if token != "machine-secret" {
			return application.Caller{}, application.ErrUnauthenticated
		}
		return application.Caller{WorkspaceID: "ws_a", SubjectID: "sa_a", CredentialID: "key_a"}, nil
	}))
	h, err := mcphttp.New(s)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h)
	defer ts.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "mender-test", Version: "v0.0.1"}, nil)
	httpClient := &http.Client{Transport: bearerTransport{base: http.DefaultTransport, token: "machine-secret"}}
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: ts.URL + "/mcp/v1/workspaces/ws_a", HTTPClient: httpClient, DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if init := session.InitializeResult(); init == nil || init.ProtocolVersion != mcphttp.ProtocolVersion || init.ServerInfo == nil || init.ServerInfo.Name != "mender" {
		t.Fatal("unexpected MCP discovery result", init)
	}
}

func TestMCPHTTPFailsClosedBeforeSDKForAuthWorkspaceAndProtocol(t *testing.T) {
	var authCalls int
	s := service(t, authFunc(func(_ context.Context, token string) (application.Caller, error) {
		authCalls++
		if token != "ok" {
			return application.Caller{}, application.ErrUnauthenticated
		}
		return application.Caller{WorkspaceID: "ws_a", SubjectID: "sa_a", CredentialID: "key_a"}, nil
	}))
	h, err := mcphttp.New(s)
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, version, auth string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(`{"not":"mcp"}`))
		if version != "" {
			r.Header.Set("Mcp-Protocol-Version", version)
		}
		if auth != "" {
			r.Header.Set("Authorization", auth)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := request(http.MethodGet, "/mcp/v1/workspaces/ws_a", mcphttp.ProtocolVersion, "Bearer ok"); w.Code != http.StatusMethodNotAllowed {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request(http.MethodPost, "/mcp/v1/workspaces/ws_a", "2025-11-25", "Bearer ok"); w.Code != http.StatusBadRequest || authCalls != 0 {
		t.Fatal(w.Code, authCalls, w.Body.String())
	}
	if w := request(http.MethodPost, "/mcp/v1/workspaces/ws_a", mcphttp.ProtocolVersion, "Bearer wrong"); w.Code != http.StatusUnauthorized || authCalls != 1 {
		t.Fatal(w.Code, authCalls, w.Body.String())
	}
	if w := request(http.MethodPost, "/mcp/v1/workspaces/ws_b", mcphttp.ProtocolVersion, "Bearer ok"); w.Code != http.StatusForbidden || authCalls != 2 {
		t.Fatal(w.Code, authCalls, w.Body.String())
	}
	if w := request(http.MethodPost, "/mcp/v1/workspaces/ws_a/extra", mcphttp.ProtocolVersion, "Bearer ok"); w.Code != http.StatusNotFound || authCalls != 2 {
		t.Fatal(w.Code, authCalls, w.Body.String())
	}
}
