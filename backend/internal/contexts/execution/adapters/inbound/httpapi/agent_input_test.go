package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application/ports"
)

type agentInputAuthFake struct {
	actions []ports.Action
}

func (*agentInputAuthFake) Authenticate(context.Context, string) (ports.Caller, error) {
	return ports.Caller{WorkspaceID: "ws_agent", SubjectID: "sa_agent", CredentialID: "key_agent"}, nil
}

func (a *agentInputAuthFake) Authorize(_ context.Context, _ ports.Caller, action ports.Action, runID ports.RunID) error {
	a.actions = append(a.actions, action)
	if runID != "run_agent" {
		return ports.ErrForbidden
	}
	return nil
}

type agentInputRepoFake struct {
	state string
}

func (r *agentInputRepoFake) record() application.AgentInputSubmissionRecord {
	at := time.Date(2026, 9, 15, 4, 0, 0, 0, time.UTC)
	return application.AgentInputSubmissionRecord{WorkspaceID: "ws_agent", RunID: "run_agent", InputRequestID: "input.req.1", State: r.state, Prompt: "Choose region", InputSchemaJSON: `{"type":"object","properties":{"region":{"type":"string"}}}`, RequestedAt: at, UpdatedAt: at}
}

func (r *agentInputRepoFake) FindAgentInputPublic(context.Context, ports.WorkspaceID, ports.RunID) (application.AgentInputSubmissionRecord, error) {
	return r.record(), nil
}

func (r *agentInputRepoFake) FindAgentInputTarget(context.Context, ports.WorkspaceID, ports.RunID, string) (application.AgentInputSubmissionTarget, error) {
	return application.AgentInputSubmissionTarget{WorkspaceID: "ws_agent", RunID: "run_agent", AttemptNo: 1, ProviderID: "provider_agent", ProviderRequestID: "provider/request-internal", ExternalTaskID: "provider/task-internal", InputRequestID: "input.req.1", InputSchemaJSON: r.record().InputSchemaJSON, State: "pending"}, nil
}

func (r *agentInputRepoFake) ClaimAgentInput(_ context.Context, target application.AgentInputSubmissionTarget, prepared application.PreparedAgentInput, _ time.Time) (application.AgentInputClaim, error) {
	target.State, target.AnswerSHA256, target.SubmissionID = "sending", prepared.AnswerSHA256, prepared.SubmissionID
	r.state = "sending"
	record := r.record()
	return application.AgentInputClaim{Target: target, Record: record}, nil
}

func (r *agentInputRepoFake) RecordAgentInputAccepted(context.Context, application.AgentInputSubmissionTarget, application.PreparedAgentInput, time.Time) (application.AgentInputSubmissionRecord, error) {
	r.state = "submitted"
	return r.record(), nil
}

func (r *agentInputRepoFake) RecordAgentInputUnknown(context.Context, application.AgentInputSubmissionTarget, application.PreparedAgentInput, time.Time) (application.AgentInputSubmissionRecord, error) {
	r.state = "unknown"
	return r.record(), nil
}

type agentInputPreparerFake struct{}

func (agentInputPreparerFake) PrepareAgentInput(string, string, []byte) (application.PreparedAgentInput, error) {
	return application.PreparedAgentInput{AnswerJSON: `{"region":"eu"}`, AnswerSHA256: strings.Repeat("a", 64), SubmissionID: "input.submit.1"}, nil
}

type agentInputSourceFake struct{}

func (agentInputSourceFake) SendProviderInput(context.Context, application.AgentInputSubmissionTarget, application.PreparedAgentInput) (application.ProviderInputResult, error) {
	return application.ProviderInputResult{Disposition: application.ProviderInputAccepted, SubmissionID: "input.submit.1"}, nil
}

type agentInputClockFake struct{ at time.Time }

func (c *agentInputClockFake) Now() time.Time {
	c.at = c.at.Add(time.Second)
	return c.at
}

func TestAgentInputHTTPUsesExplicitScopesAndProjectsOnlySafeFacts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := &agentInputAuthFake{}
	repo := &agentInputRepoFake{state: "pending"}
	service, err := application.NewAgentInputSubmissions(auth, repo, agentInputPreparerFake{}, agentInputSourceFake{}, &agentInputClockFake{at: time.Date(2026, 9, 15, 4, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewAgentInput(service, auth)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	handler.Register(router)

	request := func(method, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/v1/workspaces/ws_agent/runs/run_agent/input", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer opaque-token")
		if method == http.MethodPost {
			req.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	get := request(http.MethodGet, "")
	if get.Code != http.StatusOK || len(auth.actions) != 1 || auth.actions[0] != ports.ReadRun {
		t.Fatal(get.Code, get.Body.String(), auth.actions)
	}
	for _, forbidden := range []string{"provider_request_id", "external_task_id", "answer_sha256", "submission_id", "provider/request-internal", "provider/task-internal"} {
		if strings.Contains(get.Body.String(), forbidden) {
			t.Fatal("GET leaked Agent input internal fact", forbidden, get.Body.String())
		}
	}

	post := request(http.MethodPost, `{"input_request_id":"input.req.1","answer":{"region":"eu"}}`)
	if post.Code != http.StatusOK || len(auth.actions) != 2 || auth.actions[1] != ports.InputRun {
		t.Fatal(post.Code, post.Body.String(), auth.actions)
	}
	var response map[string]any
	if json.Unmarshal(post.Body.Bytes(), &response) != nil {
		t.Fatal(post.Body.String())
	}
	for _, forbidden := range []string{"provider_request_id", "external_task_id", "answer_sha256", "submission_id", "input_endpoint_url"} {
		if strings.Contains(post.Body.String(), forbidden) {
			t.Fatal("POST leaked Agent input internal fact", forbidden, post.Body.String())
		}
	}
}
