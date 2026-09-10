//go:build integration

package integration_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/orz-i/mender/backend/internal/bootstrap"
	runpg "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/postgres"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application/ports"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
	"github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/keycodec"
	identitypg "github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/postgres"
	identitydomain "github.com/orz-i/mender/backend/internal/contexts/identity/domain"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	"github.com/orz-i/mender/backend/migrations"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestPostgresRuntimeContract(t *testing.T) {
	dsn := os.Getenv("MENDER_TEST_DATABASE_URL")
	if dsn == "" || os.Getenv("MENDER_TEST_ALLOW_CREATE_DATABASE") != "true" {
		t.Fatal("real PostgreSQL verification requires MENDER_TEST_DATABASE_URL and MENDER_TEST_ALLOW_CREATE_DATABASE=true; no tests were skipped or treated as passed")
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme != "postgres" && u.Scheme != "postgresql" {
		t.Fatal("test database must be a PostgreSQL URL")
	}
	ip := net.ParseIP(u.Hostname())
	if !strings.EqualFold(u.Hostname(), "localhost") && (ip == nil || !ip.IsLoopback()) {
		t.Fatal("integration tests only create databases on an explicit loopback test server")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	admin, err := database.Open(ctx, dsn)
	must(t, err)
	defer admin.Close()
	_, suffix, password, err := (keycodec.Codec{}).Generate()
	must(t, err)
	dbname := "mender_test_" + suffix
	role := "mender_test_" + suffix
	_, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{dbname}.Sanitize())
	must(t, err)
	roleCreated := false
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		if _, e := admin.Exec(cleanup, "DROP DATABASE "+pgx.Identifier{dbname}.Sanitize()+" WITH (FORCE)"); e != nil {
			t.Error("temporary test database cleanup failed")
		}
		if roleCreated {
			if _, e := admin.Exec(cleanup, "DROP ROLE "+pgx.Identifier{role}.Sanitize()); e != nil {
				t.Error("temporary test role cleanup failed")
			}
		}
	}()
	// Password is newly generated hex, never a supplied credential or SQL fragment.
	_, err = admin.Exec(ctx, "CREATE ROLE "+pgx.Identifier{role}.Sanitize()+" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS PASSWORD '"+password+"'")
	must(t, err)
	roleCreated = true
	dbURL := *u
	dbURL.Path = "/" + dbname
	owner, err := database.Open(ctx, dbURL.String())
	must(t, err)
	defer owner.Close()
	must(t, migrations.Apply(ctx, owner))
	must(t, migrations.Apply(ctx, owner))
	must(t, migrations.Verify(ctx, owner))
	must(t, migrations.GrantRuntime(ctx, owner, role))
	runtimeURL := dbURL
	runtimeURL.User = url.UserPassword(role, password)
	runtime, err := database.Open(ctx, runtimeURL.String())
	must(t, err)
	defer runtime.Close()
	must(t, database.RuntimeRole(ctx, runtime))
	if err = database.RuntimeRole(ctx, owner); err == nil {
		t.Fatal("elevated owner accepted for API")
	}
	ids := identitypg.New(owner)
	at := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	issue := func(workspace, subject string, scopes []string) (string, string) {
		raw, id, digest, e := (keycodec.Codec{}).Generate()
		must(t, e)
		must(t, ids.Provision(ctx, identitydomain.Credential{ID: id, WorkspaceID: workspace, SubjectID: subject, Digest: digest, Scopes: scopes, CreatedAt: at, ExpiresAt: at.Add(time.Hour)}))
		return raw, id
	}
	keyA, idA := issue("ws_a", "sa_a", []string{"run:read", "run:cancel"})
	keyB, _ := issue("ws_b", "sa_b", []string{"run:read"})
	seed := func(workspace, id string) {
		_, e := owner.Exec(ctx, "INSERT INTO execution.runs(workspace_id,id,state,version,created_at,updated_at) VALUES($1,$2,'queued',1,$3,$3)", workspace, id, at)
		must(t, e)
	}
	seed("ws_a", "run_shared")
	seed("ws_b", "run_shared")
	seed("ws_a", "run_cas")
	seed("ws_a", "run_rollback")
	repo := runpg.New(runtime)
	actor := ports.Caller{SubjectID: "sa_a", WorkspaceID: "ws_a", CredentialID: idA}

	t.Run("composite tenant lookup and no pooled context leak", func(t *testing.T) {
		for _, workspace := range []domain.WorkspaceID{"ws_a", "ws_b", "ws_a"} {
			run, e := repo.Find(ctx, workspace, "run_shared")
			must(t, e)
			if run.Snapshot().WorkspaceID != workspace {
				t.Fatal("cross-tenant result")
			}
		}
		if _, e := repo.Find(ctx, "ws_missing", "run_shared"); !errors.Is(e, ports.ErrNotFound) {
			t.Fatal(e)
		}
		var n int
		must(t, runtime.QueryRow(ctx, "SELECT count(*) FROM execution.runs").Scan(&n))
		if n != 0 {
			t.Fatal("workspace setting escaped its transaction")
		}
	})
	t.Run("runtime grants cannot rewrite identities or immutable run fields", func(t *testing.T) {
		for _, sql := range []string{"UPDATE identity.api_keys SET revoked=false", "DELETE FROM execution.run_events", "UPDATE execution.runs SET id='run_other'", "INSERT INTO execution.runs(workspace_id,id,state,version,created_at,updated_at) VALUES('ws_a','injected','queued',1,now(),now())"} {
			if _, e := runtime.Exec(ctx, sql); e == nil {
				t.Fatal("privileged write was granted")
			}
		}
	})
	t.Run("real concurrent CAS has one winner and one actor-bearing event", func(t *testing.T) {
		run, e := repo.Find(ctx, "ws_a", "run_cas")
		must(t, e)
		_, e = run.RequestCancel(at.Add(time.Second))
		must(t, e)
		var wins, conflicts, unexpected atomic.Int32
		var group sync.WaitGroup
		for range 16 {
			group.Add(1)
			go func() {
				defer group.Done()
				e := repo.Save(ctx, run, 1, ports.Change{Actor: actor, Reason: "CAS test"})
				if e == nil {
					wins.Add(1)
				} else if errors.Is(e, ports.ErrConflict) {
					conflicts.Add(1)
				} else {
					unexpected.Add(1)
				}
			}()
		}
		group.Wait()
		if wins.Load() != 1 || conflicts.Load() != 15 || unexpected.Load() != 0 {
			t.Fatalf("wins=%d conflicts=%d errors=%d", wins.Load(), conflicts.Load(), unexpected.Load())
		}
		var count int
		must(t, owner.QueryRow(ctx, "SELECT count(*) FROM execution.run_events WHERE workspace_id='ws_a' AND run_id='run_cas' AND subject_id='sa_a' AND credential_id=$1 AND reason='CAS test'", idA).Scan(&count))
		if count != 1 {
			t.Fatal("missing/duplicate event")
		}
	})
	t.Run("event failure rolls back state mutation", func(t *testing.T) {
		_, e := owner.Exec(ctx, "ALTER TABLE execution.run_events ADD CONSTRAINT test_reject_reason CHECK (reason <> 'force_failure')")
		must(t, e)
		run, e := repo.Find(ctx, "ws_a", "run_rollback")
		must(t, e)
		_, e = run.RequestCancel(at.Add(time.Second))
		must(t, e)
		if e = repo.Save(ctx, run, 1, ports.Change{Actor: actor, Reason: "force_failure"}); !errors.Is(e, ports.ErrUnavailable) {
			t.Fatal(e)
		}
		stored, e := repo.Find(ctx, "ws_a", "run_rollback")
		must(t, e)
		if stored.Snapshot().Version != 1 || stored.Snapshot().State != domain.Queued {
			t.Fatal("failed audit write left a state change")
		}
		var n int
		must(t, owner.QueryRow(ctx, "SELECT count(*) FROM execution.run_events WHERE run_id='run_rollback'").Scan(&n))
		if n != 0 {
			t.Fatal("failed transaction emitted event")
		}
		_, e = owner.Exec(ctx, "ALTER TABLE execution.run_events DROP CONSTRAINT test_reject_reason")
		must(t, e)
	})
	t.Run("RLS with check refuses another workspace", func(t *testing.T) {
		tx, e := runtime.Begin(ctx)
		must(t, e)
		defer func() { _ = tx.Rollback(context.Background()) }()
		_, e = tx.Exec(ctx, "SELECT set_config('mender.workspace_id','ws_a',true)")
		must(t, e)
		tag, e := tx.Exec(ctx, "UPDATE execution.runs SET updated_at=updated_at WHERE workspace_id='ws_b'")
		must(t, e)
		if tag.RowsAffected() != 0 {
			t.Fatal("cross-tenant update visible")
		}
		_, e = tx.Exec(ctx, "INSERT INTO execution.run_events(workspace_id,run_id,version,state,subject_id,credential_id,occurred_at) VALUES('ws_b','run_shared',2,'canceled','sa_a',$1,now())", idA)
		if e == nil {
			t.Fatal("RLS accepted cross-tenant audit insert")
		}
	})
	t.Run("keyset lists and finite timelines use real RLS and durable events", func(t *testing.T) {
		exerciseReadQueries(t, ctx, owner, runtime, runtimeURL.String())
	})
	t.Run("atomic admission across commerce and execution", func(t *testing.T) {
		exerciseAdmission(t, ctx, owner, runtime, runtimeURL.String(), keyA)
	})
	t.Run("protected HTTP uses durable storage and rechecks key revocation", func(t *testing.T) {
		h, closeAPI, e := bootstrap.BuildAPI(ctx, bootstrap.APIConfig{RunAPIEnabled: true, DatabaseURL: runtimeURL.String()})
		must(t, e)
		defer closeAPI()
		req := func(key, method, path, body string) *httptest.ResponseRecorder {
			r := httptest.NewRequest(method, path, strings.NewReader(body))
			if key != "" {
				r.Header.Set("Authorization", "Bearer "+key)
			}
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			return w
		}
		for _, tc := range []struct {
			key, method, path, body string
			code                    int
		}{
			{"", http.MethodGet, "/readyz", "", 200}, {"", http.MethodGet, "/api/v1/workspaces/ws_a/runs/run_shared", "", 401},
			{keyB, http.MethodGet, "/api/v1/workspaces/ws_a/runs/run_shared", "", 403},
			{keyB, http.MethodPost, "/api/v1/workspaces/ws_b/runs/run_shared/cancel", "{}", 403},
			{keyA, http.MethodGet, "/api/v1/workspaces/ws_a/runs/run_shared", "", 200},
			{keyA, http.MethodPost, "/api/v1/workspaces/ws_a/runs/run_shared/cancel", `{"reason":"integration stop"}`, 200},
		} {
			w := req(tc.key, tc.method, tc.path, tc.body)
			if w.Code != tc.code {
				t.Fatalf("%s %s: got %d want %d", tc.method, tc.path, w.Code, tc.code)
			}
		}
		stored, e := repo.Find(ctx, "ws_a", "run_shared")
		must(t, e)
		if stored.Snapshot().State != domain.Canceled {
			t.Fatal("HTTP cancellation not persisted")
		}
		other, e := repo.Find(ctx, "ws_b", "run_shared")
		must(t, e)
		if other.Snapshot().State != domain.Queued {
			t.Fatal("other workspace changed")
		}
		must(t, ids.Revoke(ctx, idA))
		if w := req(keyA, http.MethodGet, "/api/v1/workspaces/ws_a/runs/run_shared", ""); w.Code != 401 {
			t.Fatal("revoked key remained authenticated", w.Code)
		}
		closeAPI()
		if w := req(keyB, http.MethodGet, "/api/v1/workspaces/ws_b/runs/run_shared", ""); w.Code != 503 {
			t.Fatal("closed database fell back", w.Code)
		}
	})
	t.Run("checksum drift and elevated runtime are rejected", func(t *testing.T) {
		if h, _, e := bootstrap.BuildAPI(ctx, bootstrap.APIConfig{RunAPIEnabled: true, DatabaseURL: dbURL.String()}); e == nil || h != nil {
			t.Fatal("migration owner accepted by API")
		}
		_, e := owner.Exec(ctx, "UPDATE mender_meta.schema_migrations SET sha256=repeat('0',64) WHERE name='0001_identity.sql'")
		must(t, e)
		if e = migrations.Verify(ctx, runtime); e == nil {
			t.Fatal("schema checksum drift ignored")
		}
		if e = migrations.Apply(ctx, owner); e == nil {
			t.Fatal("changed migration was re-applied")
		}
	})
	t.Log(fmt.Sprintf("PostgreSQL migration, role, RLS, CAS, rollback and protected HTTP suite exercised in isolated database %s", dbname))
}
