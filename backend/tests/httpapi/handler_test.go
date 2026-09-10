package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/orz-i/mender/backend/internal/contexts/execution/adapters/inbound/httpapi"
	"github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/identityaccess"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application/ports"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
	"github.com/orz-i/mender/backend/internal/contexts/identity/adapters/inbound/facade"
	"github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/keycodec"
	identityapp "github.com/orz-i/mender/backend/internal/contexts/identity/application"
	identitydomain "github.com/orz-i/mender/backend/internal/contexts/identity/domain"
)

type testClock struct{ at time.Time }

func (c testClock) Now() time.Time { return c.at }

type credentialStore struct {
	credential identitydomain.Credential
	err        error
}

func (s *credentialStore) FindCredential(_ context.Context, id string) (identitydomain.Credential, error) {
	if s.err != nil {
		return identitydomain.Credential{}, s.err
	}
	if s.credential.ID != id {
		return identitydomain.Credential{}, identityapp.ErrNotFound
	}
	return s.credential, nil
}

type runStore struct {
	mu           sync.Mutex
	run          domain.Run
	finds, saves int
	change       ports.Change
	err          error
}

func (s *runStore) Find(_ context.Context, w domain.WorkspaceID, id domain.RunID) (domain.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.finds++
	if s.err != nil {
		return domain.Run{}, s.err
	}
	v := s.run.Snapshot()
	if v.ID != id || v.WorkspaceID != w {
		return domain.Run{}, ports.ErrNotFound
	}
	return s.run, nil
}
func (s *runStore) Save(_ context.Context, r domain.Run, expected uint64, change ports.Change) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	if s.run.Snapshot().Version != expected {
		return ports.ErrConflict
	}
	s.run = r
	s.change = change
	s.saves++
	return nil
}

func setup(t *testing.T) (http.Handler, string, *credentialStore, *runStore) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	at := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	raw, id, digest, err := (keycodec.Codec{}).Generate()
	if err != nil {
		t.Fatal(err)
	}
	credentials := &credentialStore{credential: identitydomain.Credential{ID: id, WorkspaceID: "ws_a", SubjectID: "sa_a", Digest: digest, Scopes: []string{"run:read", "run:cancel"}, CreatedAt: at.Add(-time.Hour), ExpiresAt: at.Add(time.Hour)}}
	identity, err := identityapp.NewService(credentials, keycodec.Codec{}, testClock{at})
	if err != nil {
		t.Fatal(err)
	}
	access := identityaccess.New(facade.New(identity))
	run, err := domain.NewQueuedRun("run_1", "ws_a", at.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	runs := &runStore{run: run}
	service, err := application.NewService(runs, access, testClock{at})
	if err != nil {
		t.Fatal(err)
	}
	h, err := httpapi.New(service, access)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	h.Register(router)
	return router, raw, credentials, runs
}
func request(h http.Handler, token, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if method == "POST" {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

const runPath = "/api/v1/workspaces/ws_a/runs/run_1"

func TestProtectedRunQueryAndAuditedIdempotentCancel(t *testing.T) {
	h, key, credentials, runs := setup(t)
	w := request(h, key, "GET", runPath, "")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || len(w.Header().Get("X-Request-ID")) != 32 {
		t.Fatal(w.Code, w.Body.String())
	}
	var response struct {
		Data struct {
			Version string `json:"version"`
			State   string `json:"execution_state"`
		}
		Meta struct {
			RequestID string `json:"request_id"`
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Data.Version != "1" || response.Meta.RequestID == "" {
		t.Fatal(err, w.Body.String())
	}
	w = request(h, key, "POST", runPath+"/cancel", `{"reason":"operator requested stop"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if runs.saves != 1 || runs.change.Actor.CredentialID != credentials.credential.ID || runs.change.Actor.SubjectID != "sa_a" || runs.change.Reason != "operator requested stop" {
		t.Fatal("audit provenance missing")
	}
	w = request(h, key, "POST", runPath+"/cancel", `{}`)
	if w.Code != 200 || runs.saves != 1 {
		t.Fatal("duplicate cancel wrote again")
	}
	if strings.Contains(w.Body.String(), key) || strings.Contains(w.Body.String(), credentials.credential.Digest) {
		t.Fatal("credential leaked")
	}
}

func TestAuthenticationAndWorkspaceFailBeforeRunLookup(t *testing.T) {
	for name, configure := range map[string]func(*credentialStore){"unknown key": func(s *credentialStore) { s.err = identityapp.ErrNotFound }, "revoked": func(s *credentialStore) { s.credential.Revoked = true }, "expired": func(s *credentialStore) { s.credential.ExpiresAt = s.credential.CreatedAt }, "disabled workspace": func(s *credentialStore) { s.credential.WorkspaceDisabled = true }} {
		t.Run(name, func(t *testing.T) {
			h, key, c, runs := setup(t)
			configure(c)
			w := request(h, key, "GET", runPath, "")
			if w.Code != 401 || runs.finds != 0 || w.Header().Get("WWW-Authenticate") == "" {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
	h, key, _, runs := setup(t)
	for _, token := range []string{"", "bad", strings.Repeat("x", 1000)} {
		w := request(h, token, "GET", runPath, "")
		if w.Code != 401 {
			t.Fatal(w.Code)
		}
	}
	w := request(h, key, "GET", "/api/v1/workspaces/ws_b/runs/run_1", "")
	if w.Code != 403 || runs.finds != 0 {
		t.Fatal("cross-tenant lookup occurred", w.Code)
	}
	r := httptest.NewRequest("GET", runPath, nil)
	r.Header.Add("Authorization", "Bearer "+key)
	r.Header.Add("Authorization", "Bearer "+key)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("duplicate authentication accepted")
	}
}

func TestScopesUnknownRunsAndDependencyErrors(t *testing.T) {
	h, key, c, runs := setup(t)
	c.credential.Scopes = []string{"run:read"}
	w := request(h, key, "POST", runPath+"/cancel", `{}`)
	if w.Code != 403 || runs.finds != 0 {
		t.Fatal(w.Code)
	}
	w = request(h, key, "GET", "/api/v1/workspaces/ws_a/runs/missing", "")
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
	runs.err = errors.New("postgres private-secret connection failed")
	w = request(h, key, "GET", runPath, "")
	if w.Code != 503 || strings.Contains(w.Body.String(), "private-secret") {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(h, key, "POST", "/api/v1/workspaces/ws_a/runs", `{}`)
	if w.Code != 404 {
		t.Fatal("StartRun exposed")
	}
}

func TestCancellationOnlyRecordsIntentAndUnknownOutcomeRemainsUnknown(t *testing.T) {
	h, key, _, runs := setup(t)
	at := runs.run.Snapshot().CreatedAt
	if err := runs.run.Start(at); err != nil {
		t.Fatal(err)
	}
	w := request(h, key, "POST", runPath+"/cancel", `{}`)
	if w.Code != 202 || runs.run.Snapshot().State != domain.CancelRequested {
		t.Fatal(w.Code)
	}
	if err := runs.run.MarkOutcomeUnconfirmed(runs.run.Snapshot().UpdatedAt); err != nil {
		t.Fatal(err)
	}
	w = request(h, key, "POST", runPath+"/cancel", `{}`)
	if w.Code != 409 || runs.saves != 1 {
		t.Fatal(w.Code)
	}
}

func TestCancellationBodyValidationAndNoClientIdentityOverride(t *testing.T) {
	for _, body := range []string{"", "null", `{"subject_id":"sa_admin"}`, `{"workspace_id":"ws_b"}`, `{} {}`, `{"reason":"` + strings.Repeat("x", 501) + `"}`, `{"reason":"\u0000"}`, `{"reason":null}`, `{"Reason":"case bypass"}`, `{"reason":"first","reason":"second"}`, `{"reason":123}`} {
		h, key, _, runs := setup(t)
		w := request(h, key, "POST", runPath+"/cancel", body)
		if w.Code != 400 || runs.saves != 0 {
			t.Fatal(w.Code, body)
		}
	}
	h, key, _, runs := setup(t)
	w := request(h, key, "POST", runPath+"/cancel", `{"reason":"`+strings.Repeat("x", 5000)+`"}`)
	if w.Code != 413 || runs.saves != 0 {
		t.Fatal(w.Code)
	}
	r := httptest.NewRequest("POST", runPath+"/cancel", strings.NewReader(`{}`))
	r.Header.Set("Authorization", "Bearer "+key)
	r.Header.Set("Content-Type", "text/plain")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 415 {
		t.Fatal(w.Code)
	}
	r = httptest.NewRequest("GET", runPath, nil)
	r.Header.Set("Authorization", "Bearer "+key)
	r.Header.Set("X-Workspace-ID", "ws_b")
	r.Header.Set("X-Subject-ID", "admin")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || strings.Contains(w.Body.String(), "ws_b") {
		t.Fatal("client identity header was trusted")
	}
}
