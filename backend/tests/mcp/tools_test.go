package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	mcphttp "github.com/orz-i/mender/backend/internal/processes/mcpbridge/adapters/inbound/httpapi"
	"github.com/orz-i/mender/backend/internal/processes/mcpbridge/application"
)

type captureStarter struct {
	caller application.Caller
	input  application.StartRequest
	calls  int
}

func (s *captureStarter) Start(_ context.Context, c application.Caller, q application.StartRequest) (application.StartReceipt, error) {
	s.calls++
	s.caller, s.input = c, q
	return application.StartReceipt{WorkspaceID: c.WorkspaceID, RunID: "run_meta", ReservationID: "res_run_meta", Currency: q.Currency, ReservedMicro: 70000, Replayed: s.calls > 1}, nil
}

type captureRuns struct {
	getCaller, cancelCaller, artifactCaller application.Caller
	cancelReason                            string
}

func (r *captureRuns) GetRun(_ context.Context, c application.Caller, id string) (application.Run, error) {
	r.getCaller = c
	return application.Run{RunID: id, WorkspaceID: c.WorkspaceID, State: "succeeded", Version: 7, CreatedAt: time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 9, 11, 9, 1, 0, 0, time.UTC)}, nil
}
func (r *captureRuns) CancelRun(_ context.Context, c application.Caller, id, reason string) (application.Run, error) {
	r.cancelCaller, r.cancelReason = c, reason
	return application.Run{RunID: id, WorkspaceID: c.WorkspaceID, State: "cancel_requested", Version: 8, UpdatedAt: time.Date(2026, 9, 11, 9, 2, 0, 0, time.UTC)}, nil
}
func (r *captureRuns) GetArtifact(_ context.Context, c application.Caller, runID, artifactID string) (application.Artifact, error) {
	r.artifactCaller = c
	return application.Artifact{ArtifactID: artifactID, Kind: "provider_result", MediaType: "application/json", SizeBytes: 28, CreatedAt: time.Date(2026, 9, 11, 9, 1, 0, 0, time.UTC), ContentJSON: `{"answer":9007199254740993}`}, nil
}

func connectTools(t *testing.T, starter application.Starter, runs application.Runs) (*mcp.ClientSession, func()) {
	t.Helper()
	service, err := application.New(authFunc(func(_ context.Context, token string) (application.Caller, error) {
		if token != "machine-secret" {
			return application.Caller{}, application.ErrUnauthenticated
		}
		return application.Caller{WorkspaceID: "ws_a", SubjectID: "sa_a", CredentialID: "key_a"}, nil
	}), starter, runs)
	if err != nil {
		t.Fatal(err)
	}
	h, err := mcphttp.New(service)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h)
	client := mcp.NewClient(&mcp.Implementation{Name: "mender-tools-test", Version: "v0.0.1"}, nil)
	httpClient := &http.Client{Transport: bearerTransport{base: http.DefaultTransport, token: "machine-secret"}}
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: ts.URL + "/mcp/v1/workspaces/ws_a", HTTPClient: httpClient, DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err != nil {
		ts.Close()
		t.Fatal(err)
	}
	return session, func() { _ = session.Close(); ts.Close() }
}

func TestMetaToolCatalogIsSmallStableAndDoesNotAdvertiseInternalControls(t *testing.T) {
	session, close := connectTools(t, &captureStarter{}, &captureRuns{})
	defer close()
	result, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(result.Tools))
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
		encoded, _ := json.Marshal(tool)
		for _, forbidden := range []string{"provider_request_id", "external_task_id", "canonical_arguments", "credential_id", "settlement", "secret"} {
			if strings.Contains(string(encoded), forbidden) {
				t.Fatalf("tool %s leaked internal field %s", tool.Name, forbidden)
			}
		}
	}
	slices.Sort(names)
	want := []string{"mender_artifact_get", "mender_run_cancel", "mender_run_get", "mender_run_start"}
	if !slices.Equal(names, want) {
		t.Fatal(names)
	}
}

func TestMetaToolsReuseAuthenticatedCallerAndPreserveStartArgumentsPrecision(t *testing.T) {
	starter, runs := &captureStarter{}, &captureRuns{}
	session, close := connectTools(t, starter, runs)
	defer close()
	ctx := context.Background()
	startArgs := map[string]any{
		"idempotency_key": "operation-0001", "tool_id": "tool_a", "tool_version": "1.0.0", "toolset_id": "set_a", "connection_id": "conn_a",
		"arguments": map[string]any{"large_integer": json.Number("9007199254740993")}, "currency": "USD", "max_charge_micro": "100000",
	}
	start, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "mender_run_start", Arguments: startArgs})
	if err != nil {
		t.Fatal(err)
	}
	if start.IsError || starter.calls != 1 || starter.caller.WorkspaceID != "ws_a" || !strings.Contains(string(starter.input.Arguments), "9007199254740993") || strings.Contains(string(starter.input.Arguments), "9007199254740992") {
		t.Fatal(start, starter.calls, starter.caller, string(starter.input.Arguments))
	}
	if !strings.Contains(start.Content[0].(*mcp.TextContent).Text, `"run_id":"run_meta"`) {
		t.Fatal(start.Content)
	}

	get, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "mender_run_get", Arguments: map[string]any{"run_id": "run_meta"}})
	if err != nil || get.IsError {
		t.Fatal(get, err)
	}
	if runs.getCaller.CredentialID != "key_a" || !strings.Contains(get.Content[0].(*mcp.TextContent).Text, `"version":"7"`) {
		t.Fatal(runs.getCaller, get.Content)
	}

	cancel, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "mender_run_cancel", Arguments: map[string]any{"run_id": "run_meta", "reason": "user requested"}})
	if err != nil || cancel.IsError {
		t.Fatal(cancel, err)
	}
	if runs.cancelCaller.SubjectID != "sa_a" || runs.cancelReason != "user requested" {
		t.Fatal(runs.cancelCaller, runs.cancelReason)
	}

	artifact, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "mender_artifact_get", Arguments: map[string]any{"run_id": "run_meta", "artifact_id": "art_run_meta"}})
	if err != nil || artifact.IsError {
		t.Fatal(artifact, err)
	}
	text := artifact.Content[0].(*mcp.TextContent).Text
	if runs.artifactCaller.WorkspaceID != "ws_a" || !strings.Contains(text, `9007199254740993`) {
		t.Fatal(runs.artifactCaller, text)
	}
	for _, forbidden := range []string{"provider_request_id", "external_task_id", "source_observation_id", "credential_id"} {
		if strings.Contains(text, forbidden) {
			t.Fatal("artifact output leak", forbidden, text)
		}
	}
}

func TestMetaToolInputIsStrictAndErrorsAreSanitized(t *testing.T) {
	session, close := connectTools(t, &captureStarter{}, &captureRuns{})
	defer close()
	for _, tc := range []struct {
		name string
		args any
	}{
		{"mender_run_get", map[string]any{"run_id": "run_a", "provider_request_id": "leak"}},
		{"mender_artifact_get", map[string]any{"run_id": "run_a", "artifact_id": "other"}},
		{"mender_run_start", map[string]any{"idempotency_key": "operation-0001", "tool_id": "tool", "tool_version": "1", "toolset_id": "set", "connection_id": "conn", "arguments": []any{1}, "currency": "USD", "max_charge_micro": "1"}},
	} {
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tc.name, Arguments: tc.args})
		if err != nil {
			t.Fatal(tc.name, err)
		}
		if !result.IsError || len(result.Content) != 1 || result.Content[0].(*mcp.TextContent).Text != "INVALID_ARGUMENT" {
			t.Fatal(tc.name, result)
		}
	}
}
