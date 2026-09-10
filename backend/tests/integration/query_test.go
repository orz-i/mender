//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/bootstrap"
	runpg "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/postgres"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application/ports"
	"github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/keycodec"
	identitypg "github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/postgres"
	identitydomain "github.com/orz-i/mender/backend/internal/contexts/identity/domain"
)

// Runs inside TestPostgresRuntimeContract's owned database and restricted role.
func exerciseReadQueries(t *testing.T, ctx context.Context, owner, runtime *pgxpool.Pool, runtimeURL string) {
	t.Helper()
	at := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	identities := identitypg.New(owner)
	issue := func(w, s string) (string, string) {
		raw, id, digest, e := (keycodec.Codec{}).Generate()
		must(t, e)
		must(t, identities.Provision(ctx, identitydomain.Credential{ID: id, WorkspaceID: w, SubjectID: s, Digest: digest, Scopes: []string{"run:read", "run:cancel"}, CreatedAt: at, ExpiresAt: at.Add(time.Hour)}))
		return raw, id
	}
	keyA, idA := issue("ws_read_a", "sa_read_a")
	keyB, _ := issue("ws_read_b", "sa_read_b")
	seed := func(w, id string, created time.Time) {
		_, e := owner.Exec(ctx, "INSERT INTO execution.runs(workspace_id,id,state,version,created_at,updated_at) VALUES($1,$2,'queued',1,$3,$3)", w, id, created)
		must(t, e)
	}
	for _, id := range []string{"run_A", "run_b", "run_z"} {
		seed("ws_read_a", id, at)
		seed("ws_read_b", id, at)
	}
	h, closeIt, e := bootstrap.BuildAPI(ctx, bootstrap.APIConfig{RunAPIEnabled: true, RunReadAPIEnabled: true, CursorSigningKey: bytes.Repeat([]byte{77}, 32), DatabaseURL: runtimeURL})
	must(t, e)
	defer closeIt()
	request := func(key, path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Authorization", "Bearer "+key)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	type response struct {
		Data []struct {
			RunID   string `json:"run_id"`
			Version string `json:"version"`
		} `json:"data"`
		Meta struct {
			Next    *string `json:"next_cursor"`
			Through string  `json:"through_version"`
		} `json:"meta"`
	}
	decode := func(w *httptest.ResponseRecorder) response {
		if w.Code != 200 {
			t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
		}
		var p response
		must(t, json.Unmarshal(w.Body.Bytes(), &p))
		if p.Data == nil {
			t.Fatal("empty result must be array")
		}
		return p
	}
	base := "/api/v1/workspaces/ws_read_a/runs"
	p := decode(request(keyA, base+"?limit=1&state=queued"))
	if len(p.Data) != 1 || p.Data[0].RunID != "run_z" || p.Meta.Next == nil {
		t.Fatal("C-collated first page", p)
	}
	first := *p.Meta.Next
	seed("ws_read_a", "run_newer", at.Add(time.Second))
	for _, want := range []string{"run_b", "run_A"} {
		p = decode(request(keyA, base+"?limit=1&state=queued&cursor="+url.QueryEscape(*p.Meta.Next)))
		if len(p.Data) != 1 || p.Data[0].RunID != want {
			t.Fatal("keyset changed under insertion", p)
		}
	}
	if p.Meta.Next != nil {
		t.Fatal("keyset traversal did not terminate")
	}
	if p = decode(request(keyA, base+"?state=running")); len(p.Data) != 0 {
		t.Fatal("state filter ignored")
	}
	if w := request(keyB, base+"?limit=1&state=queued&cursor="+url.QueryEscape(first)); w.Code != 403 {
		t.Fatal("cross-workspace cursor bypass", w.Code)
	}
	if w := request(keyA, base+"?limit=2&state=queued&cursor="+url.QueryEscape(first)); w.Code != 400 {
		t.Fatal("cursor filter binding ignored", w.Code)
	}
	if p = decode(request(keyA, base+"/run_b/events")); len(p.Data) != 0 || p.Meta.Through != "1" {
		t.Fatal("empty existing timeline", p)
	}
	if w := request(keyA, base+"/missing/events"); w.Code != 404 {
		t.Fatal("missing run conflated with empty timeline", w.Code)
	}
	seed("ws_read_a", "run_events", at)
	repository := runpg.New(runtime)
	run, e := repository.Find(ctx, "ws_read_a", "run_events")
	must(t, e)
	actor := ports.Caller{WorkspaceID: "ws_read_a", SubjectID: "sa_read_a", CredentialID: idA}
	must(t, run.Start(at.Add(time.Second)))
	must(t, repository.Save(ctx, run, 1, ports.Change{Actor: actor, Reason: "start"}))
	must(t, run.WaitForInput(at.Add(2*time.Second)))
	must(t, repository.Save(ctx, run, 2, ports.Change{Actor: actor, Reason: "wait"}))
	must(t, run.Resume(at.Add(3*time.Second)))
	must(t, repository.Save(ctx, run, 3, ports.Change{Actor: actor, Reason: "resume"}))
	p = decode(request(keyA, base+"/run_events/events?limit=1"))
	if p.Meta.Through != "4" || p.Data[0].Version != "2" || p.Meta.Next == nil {
		t.Fatal("initial event watermark", p)
	}
	_, e = run.RequestCancel(at.Add(4 * time.Second))
	must(t, e)
	must(t, repository.Save(ctx, run, 4, ports.Change{Actor: actor, Reason: "later cancel intent"}))
	for _, want := range []string{"3", "4"} {
		p = decode(request(keyA, base+"/run_events/events?limit=1&cursor="+url.QueryEscape(*p.Meta.Next)))
		if p.Meta.Through != "4" || len(p.Data) != 1 || p.Data[0].Version != want {
			t.Fatal("later event escaped watermark", p)
		}
	}
	if p.Meta.Next != nil {
		t.Fatal("finite event traversal did not terminate")
	}
	w := request(keyA, base+"/run_events/events")
	p = decode(w)
	if p.Meta.Through != "5" || len(p.Data) != 4 || strings.Contains(w.Body.String(), idA) || strings.Contains(w.Body.String(), keyA) {
		t.Fatal("fresh timeline or redaction failed")
	}
	var visible int
	must(t, runtime.QueryRow(ctx, "SELECT count(*) FROM execution.run_events").Scan(&visible))
	if visible != 0 {
		t.Fatal("timeline query leaked pooled tenant context")
	}
	must(t, identities.Revoke(ctx, idA))
	if w = request(keyA, base+"?limit=1&state=queued&cursor="+url.QueryEscape(first)); w.Code != 401 {
		t.Fatal("revoked caller used a signed cursor", w.Code)
	}
}
