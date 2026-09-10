package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/orz-i/mender/backend/internal/contexts/execution/adapters/inbound/httpapi"
	"github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/cursor"
	"github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/identityaccess"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application/ports"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
	"github.com/orz-i/mender/backend/internal/contexts/identity/adapters/inbound/facade"
	"github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/keycodec"
	identityapp "github.com/orz-i/mender/backend/internal/contexts/identity/application"
)

type projection struct {
	rows  []domain.Snapshot
	calls int
}

func (p *projection) FetchRuns(_ context.Context, w ports.WorkspaceID, f ports.RunFilter) ([]domain.Snapshot, error) {
	p.calls++
	out := []domain.Snapshot{}
	for _, s := range p.rows {
		if s.WorkspaceID != w || f.State != "" && f.State != s.State {
			continue
		}
		if !f.BeforeCreated.IsZero() && (s.CreatedAt.After(f.BeforeCreated) || s.CreatedAt.Equal(f.BeforeCreated) && s.ID >= f.BeforeID) {
			continue
		}
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b domain.Snapshot) int { return strings.Compare(string(b.ID), string(a.ID)) })
	return out[:min(len(out), f.Size+1)], nil
}
func (p *projection) FetchEvents(_ context.Context, w ports.WorkspaceID, id ports.RunID, f ports.EventFilter) (ports.EventBatch, error) {
	p.calls++
	if id != "run_1" {
		return ports.EventBatch{}, ports.ErrNotFound
	}
	e := ports.Event{WorkspaceID: w, RunID: id, Version: 2, State: domain.Canceled, OccurredAt: p.rows[0].CreatedAt, SubjectID: "sa_a", Reason: "safe visible reason"}
	return ports.EventBatch{Through: 2, Items: []ports.Event{e}}, nil
}
func setupQueries(t *testing.T) (http.Handler, string, *credentialStore, *projection) {
	t.Helper()
	h, key, credentials, runs := setup(t)
	clock := testClock{credentials.credential.CreatedAt.Add(time.Hour)}
	id, e := identityapp.NewService(credentials, keycodec.Codec{}, clock)
	if e != nil {
		t.Fatal(e)
	}
	access := identityaccess.New(facade.New(id))
	codec, e := cursor.New(bytes.Repeat([]byte{1}, 32))
	if e != nil {
		t.Fatal(e)
	}
	rows := []domain.Snapshot{runs.run.Snapshot()}
	for _, name := range []string{"run_2", "run_3"} {
		r, e := domain.NewQueuedRun(domain.RunID(name), "ws_a", rows[0].CreatedAt)
		if e != nil {
			t.Fatal(e)
		}
		rows = append(rows, r.Snapshot())
	}
	store := &projection{rows: rows}
	queries, e := application.NewQueries(store, access, codec, clock)
	if e != nil {
		t.Fatal(e)
	}
	adapter, e := httpapi.NewQueries(queries, access)
	if e != nil {
		t.Fatal(e)
	}
	adapter.Register(h.(*gin.Engine))
	return h, key, credentials, store
}

type collectionResponse struct {
	Data []struct {
		RunID     string `json:"run_id"`
		Version   string `json:"version"`
		SubjectID string `json:"subject_id"`
		Reason    string `json:"reason"`
	} `json:"data"`
	Meta struct {
		Next    *string `json:"next_cursor"`
		Through string  `json:"through_version"`
	} `json:"meta"`
}

func collection(t *testing.T, w *httptest.ResponseRecorder) collectionResponse {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("code %d: %s", w.Code, w.Body.String())
	}
	var p collectionResponse
	if e := json.Unmarshal(w.Body.Bytes(), &p); e != nil {
		t.Fatal(e)
	}
	if p.Data == nil {
		t.Fatal("data must be an array")
	}
	return p
}

const listPath = "/api/v1/workspaces/ws_a/runs"

func TestReadHTTPPagesAreScopedSignedAndNonCaching(t *testing.T) {
	h, key, credentials, store := setupQueries(t)
	w := request(h, key, "GET", listPath+"?limit=1&state=queued", "")
	p := collection(t, w)
	if len(p.Data) != 1 || p.Data[0].RunID != "run_3" || p.Meta.Next == nil || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(p, w.Header())
	}
	token := *p.Meta.Next
	p = collection(t, request(h, key, "GET", listPath+"?limit=1&state=queued&cursor="+url.QueryEscape(token), ""))
	if p.Data[0].RunID != "run_2" {
		t.Fatal(p)
	}
	for _, path := range []string{listPath + "?limit=2&state=queued&cursor=" + token, listPath + "?limit=1&cursor=" + token, listPath + "?limit=1&state=queued&cursor=" + token + "x", listPath + "/run_1/events?limit=1&cursor=" + token} {
		before := store.calls
		w = request(h, key, "GET", path, "")
		if w.Code != 400 || store.calls != before || !strings.Contains(w.Body.String(), "INVALID_CURSOR") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	credentials.credential.Revoked = true
	before := store.calls
	w = request(h, key, "GET", listPath+"?limit=1&state=queued&cursor="+token, "")
	if w.Code != 401 || store.calls != before {
		t.Fatal("cursor bypassed revoked credential")
	}
}

func TestReadHTTPRejectsUnknownDuplicateAndMalformedQueries(t *testing.T) {
	for _, query := range []string{"?limit=0", "?limit=101", "?limit=-1", "?limit=01", "?limit=+1", "?limit=1&limit=2", "?limit=", "?cursor=", "?state=QUEUED", "?state=queued&state=queued", "?workspace_id=ws_b", "?offset=1", "?Limit=1", "?cursor=%ZZ", "?state=queued;limit=1"} {
		h, key, _, store := setupQueries(t)
		w := request(h, key, "GET", listPath+query, "")
		if w.Code != 400 || store.calls != 0 {
			t.Fatal(query, w.Code, w.Body.String())
		}
	}
	h, key, _, store := setupQueries(t)
	w := request(h, key, "GET", listPath+"/run_1/events?state=canceled", "")
	if w.Code != 400 || store.calls != 0 {
		t.Fatal(w.Code)
	}
}

func TestReadHTTPAuthorizationPrecedesProjectionAndEventsHideCredentialIDs(t *testing.T) {
	h, key, c, store := setupQueries(t)
	for _, path := range []string{listPath, listPath + "/run_1/events"} {
		if w := request(h, "", "GET", path, ""); w.Code != 401 {
			t.Fatal(w.Code)
		}
	}
	if w := request(h, key, "GET", "/api/v1/workspaces/ws_b/runs", ""); w.Code != 403 || store.calls != 0 {
		t.Fatal(w.Code)
	}
	c.credential.Scopes = []string{"run:cancel"}
	for _, path := range []string{listPath, listPath + "/run_1/events"} {
		if w := request(h, key, "GET", path, ""); w.Code != 403 || store.calls != 0 {
			t.Fatal(w.Code)
		}
	}
	c.credential.Scopes = []string{"run:read"}
	w := request(h, key, "GET", listPath+"/run_1/events", "")
	p := collection(t, w)
	if p.Meta.Through != "2" || p.Data[0].Version != "2" || p.Data[0].SubjectID != "sa_a" {
		t.Fatal(p)
	}
	for _, secret := range []string{key, c.credential.ID, c.credential.Digest, "credential_id", "billing_state"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("unneeded secret or unimplemented field in timeline")
		}
	}
	if w = request(h, key, "GET", listPath+"/missing/events", ""); w.Code != 404 {
		t.Fatal(w.Code)
	}
	if w = request(h, key, "POST", listPath, `{}`); w.Code != 404 {
		t.Fatal("new read surface exposed admission")
	}
}
