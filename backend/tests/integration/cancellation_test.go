//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/bootstrap"
	"github.com/orz-i/mender/backend/internal/contexts/identity/adapters/inbound/facade"
	"github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/keycodec"
	identitypg "github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/postgres"
	identityapp "github.com/orz-i/mender/backend/internal/contexts/identity/application"
	identitydomain "github.com/orz-i/mender/backend/internal/contexts/identity/domain"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	"github.com/orz-i/mender/backend/internal/processes/admission/adapters/outbound/identitycancel"
	admit "github.com/orz-i/mender/backend/internal/processes/admission/application"
	"github.com/orz-i/mender/backend/migrations"
)

type cancellationClock struct{}

func (cancellationClock) Now() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

// Uses the parent suite's owned database and real machine identity. Only plan
// resolution and creation authorization are inherited admission fixtures.
func exerciseCancellation(t *testing.T, ctx context.Context, owner, runtime, writer *pgxpool.Pool, runtimeDSN, keyA string, admissions *admit.Service, creationCaller admit.Caller) {
	t.Helper()
	_, suffix, password, e := (keycodec.Codec{}).Generate()
	must(t, e)
	role := "mender_cancel_" + suffix
	_, e = owner.Exec(ctx, "CREATE ROLE "+pgx.Identifier{role}.Sanitize()+" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS PASSWORD '"+password+"'")
	must(t, e)
	defer func() {
		c, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if _, e := owner.Exec(c, "DROP OWNED BY "+pgx.Identifier{role}.Sanitize()); e != nil {
			t.Error("cancellation grants cleanup failed")
		}
		if _, e := owner.Exec(c, "DROP ROLE "+pgx.Identifier{role}.Sanitize()); e != nil {
			t.Error("cancellation role cleanup failed")
		}
	}()
	var operatorOut, operatorErr bytes.Buffer
	must(t, bootstrap.RunOperator(ctx, []string{"grant-cancellation", "--role", role}, func(key string) string {
		if key == "MENDER_ADMIN_DATABASE_URL" {
			return owner.Config().ConnString()
		}
		return ""
	}, &operatorOut, &operatorErr))
	if !strings.Contains(operatorOut.String(), "Restricted cancellation grants applied") || operatorErr.Len() != 0 || strings.Contains(operatorOut.String(), password) {
		t.Fatal("operator grant did not produce a secret-free success message")
	}
	// Re-applying the same explicit grants is safe and does not create a role.
	must(t, migrations.GrantCancellation(ctx, owner, role))
	u, e := url.Parse(runtimeDSN)
	must(t, e)
	u.User = url.UserPassword(role, password)
	cancelPool, e := database.Open(ctx, u.String())
	must(t, e)
	defer cancelPool.Close()
	must(t, database.CancellationRole(ctx, cancelPool))
	identity, e := identityapp.NewService(identitypg.New(runtime), keycodec.Codec{}, cancellationClock{})
	must(t, e)
	pub := facade.New(identity)
	principal, e := pub.Authenticate(ctx, keyA)
	must(t, e)
	caller := admit.Caller{WorkspaceID: principal.WorkspaceID, SubjectID: principal.SubjectID, CredentialID: principal.CredentialID}
	cancelService, e := bootstrap.BuildCancellation(ctx, cancelPool, identitycancel.New(pub))
	must(t, e)
	for _, p := range []*pgxpool.Pool{owner, runtime, writer} {
		if s, e := bootstrap.BuildCancellation(ctx, p, identitycancel.New(pub)); e == nil || s != nil {
			t.Fatal("wrong cancellation role accepted")
		}
	}
	if s, e := bootstrap.BuildCancellation(ctx, cancelPool, nil); e == nil || s != nil {
		t.Fatal("missing authority silently supplied")
	}
	request := func(key, budget string) admit.Request {
		return admit.Request{IdempotencyKey: key, ToolVersionID: "tool_v1", ToolsetVersionID: "set_v1", ConnectionID: "conn_a", BudgetID: budget, PeriodID: "p1", Currency: "USD", MaxChargeMicro: "100", Arguments: []byte(`{"query":"cancel-test"}`)}
	}
	create := func(key string) admit.Receipt {
		r, e := admissions.Admit(ctx, creationCaller, request(key, "budget_ok"))
		must(t, e)
		return r
	}
	issue := func(w, subject string, scopes []string) (string, string) {
		raw, id, digest, e := (keycodec.Codec{}).Generate()
		must(t, e)
		at := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
		must(t, identitypg.New(owner).Provision(ctx, identitydomain.Credential{ID: id, WorkspaceID: w, SubjectID: subject, Digest: digest, Scopes: scopes, CreatedAt: at, ExpiresAt: at.Add(time.Hour)}))
		return raw, id
	}
	state := func(r admit.Receipt) string {
		var s string
		must(t, owner.QueryRow(ctx, `SELECT jsonb_build_object('run',(SELECT to_jsonb(x) FROM execution.runs x WHERE workspace_id=$1 AND id=$2),'job',(SELECT to_jsonb(x) FROM execution.jobs x WHERE workspace_id=$1 AND run_id=$2),'reservation',(SELECT to_jsonb(x) FROM commerce.reservations x WHERE workspace_id=$1 AND id=$3),'period',(SELECT to_jsonb(b) FROM commerce.budget_periods b JOIN commerce.reservations z USING(workspace_id,budget_id,period_id) WHERE z.workspace_id=$1 AND z.id=$3),'events',(SELECT jsonb_agg(x ORDER BY version) FROM execution.run_events x WHERE workspace_id=$1 AND run_id=$2),'outbox',(SELECT jsonb_agg(x ORDER BY event_type) FROM execution.outbox x WHERE workspace_id=$1 AND run_id=$2),'receipt',(SELECT to_jsonb(x) FROM execution.run_cancellations x WHERE workspace_id=$1 AND run_id=$2))::text`, r.WorkspaceID, r.RunID, r.ReservationID).Scan(&s))
		return s
	}
	assertCanceled := func(r admit.Receipt) {
		var ok bool
		must(t, owner.QueryRow(ctx, `SELECT r.state='canceled' AND r.version=2 AND j.state='canceled' AND j.stopped_at=r.updated_at AND z.state='released' AND z.released_at=r.updated_at AND c.occurred_at=r.updated_at AND c.released_micro=z.amount_micro AND a.delivery_state='suppressed' AND o.delivery_state='pending' FROM execution.runs r JOIN execution.jobs j ON (j.workspace_id,j.run_id)=(r.workspace_id,r.id) JOIN commerce.reservations z ON (z.workspace_id,z.run_id)=(r.workspace_id,r.id) JOIN execution.run_cancellations c ON (c.workspace_id,c.run_id)=(r.workspace_id,r.id) JOIN execution.outbox a ON (a.workspace_id,a.run_id)=(r.workspace_id,r.id) AND a.event_type='run.admitted' JOIN execution.outbox o ON (o.workspace_id,o.run_id)=(r.workspace_id,r.id) AND o.event_type='run.canceled' WHERE r.workspace_id=$1 AND r.id=$2`, r.WorkspaceID, r.RunID).Scan(&ok))
		if !ok {
			t.Fatal("partial cancellation")
		}
		var n int
		must(t, owner.QueryRow(ctx, `SELECT count(*) FROM execution.run_events WHERE workspace_id=$1 AND run_id=$2 AND state='canceled'`, r.WorkspaceID, r.RunID).Scan(&n))
		if n != 1 {
			t.Fatal("duplicate/missing state event", n)
		}
	}
	t.Run("concurrent duplicate cancellation releases once and preserves first reason", func(t *testing.T) {
		r := create("cancel_concurrent_01")
		results := make(chan admit.CancelResult, 16)
		errs := make(chan error, 16)
		var wg sync.WaitGroup
		for range 16 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				v, e := cancelService.Cancel(ctx, caller, r.RunID, "first reason")
				results <- v
				errs <- e
			}()
		}
		wg.Wait()
		close(results)
		close(errs)
		for e := range errs {
			must(t, e)
		}
		fresh := 0
		for v := range results {
			if !v.Found || v.Run.RunID != r.RunID || v.Run.State != "canceled" {
				t.Fatal(v)
			}
			if !v.Run.Replayed {
				fresh++
			}
		}
		if fresh != 1 {
			t.Fatal("cancellation mutated more than once", fresh)
		}
		assertCanceled(r)
		before := state(r)
		v, e := cancelService.Cancel(ctx, caller, r.RunID, "different retry reason")
		must(t, e)
		if !v.Run.Replayed || state(r) != before {
			t.Fatal("retry overwrote cancellation facts")
		}
		replayed, e := admissions.Admit(ctx, creationCaller, request("cancel_concurrent_01", "budget_ok"))
		must(t, e)
		if !replayed.Replayed || replayed.RunID != r.RunID || state(r) != before {
			t.Fatal("admission replay recreated canceled Run")
		}
	})
	t.Run("all eight write faults roll back then a retry succeeds", func(t *testing.T) {
		faults := []struct{ table, check string }{{"commerce.budget_periods", "false"}, {"commerce.reservations", "false"}, {"execution.jobs", "false"}, {"execution.runs", "false"}, {"execution.run_events", "false"}, {"execution.outbox", "delivery_state<>'suppressed'"}, {"execution.outbox", "event_type<>'run.canceled'"}, {"execution.run_cancellations", "false"}}
		for i, f := range faults {
			t.Run(fmt.Sprint(i, "_", f.table), func(t *testing.T) {
				r := create(fmt.Sprintf("cancel_fault_%02d", i))
				before := state(r)
				_, e := owner.Exec(ctx, "ALTER TABLE "+f.table+" ADD CONSTRAINT test_cancel_write_fault CHECK("+f.check+") NOT VALID")
				must(t, e)
				v, e := cancelService.Cancel(ctx, caller, r.RunID, "fault test")
				_, drop := owner.Exec(ctx, "ALTER TABLE "+f.table+" DROP CONSTRAINT test_cancel_write_fault")
				must(t, drop)
				if e == nil || v.Found {
					t.Fatal("fault returned success")
				}
				if state(r) != before {
					t.Fatal("partial cancellation persisted at", f.table)
				}
				v, e = cancelService.Cancel(ctx, caller, r.RunID, "successful retry")
				must(t, e)
				if v.Run.Replayed {
					t.Fatal("rollback preserved a replay receipt")
				}
				assertCanceled(r)
			})
		}
	})
	t.Run("expired inactive original period releases without crediting new period", func(t *testing.T) {
		at := time.Now().UTC().Truncate(time.Microsecond)
		_, e := owner.Exec(ctx, `INSERT INTO commerce.budget_periods(workspace_id,budget_id,period_id,currency,starts_at,ends_at,limit_micro) VALUES('ws_a','cancel_period','p1','USD',$1,$2,100),('ws_a','cancel_period','p2','USD',$1,$2,100)`, at.Add(-time.Hour), at.Add(time.Hour))
		must(t, e)
		r, e := admissions.Admit(ctx, creationCaller, request("cancel_old_period", "cancel_period"))
		must(t, e)
		_, e = owner.Exec(ctx, `UPDATE commerce.budget_periods SET active=false,ends_at=$1 WHERE workspace_id='ws_a' AND budget_id='cancel_period' AND period_id='p1'`, at.Add(-time.Minute))
		must(t, e)
		_, e = cancelService.Cancel(ctx, caller, r.RunID, "period expired")
		must(t, e)
		assertCanceled(r)
		var reserved, revision int64
		must(t, owner.QueryRow(ctx, `SELECT reserved_micro,revision FROM commerce.budget_periods WHERE workspace_id='ws_a' AND budget_id='cancel_period' AND period_id='p2'`).Scan(&reserved, &revision))
		if reserved != 0 || revision != 1 {
			t.Fatal("new period modified")
		}
	})
	t.Run("unsafe running and reconciling outcomes never release quota", func(t *testing.T) {
		for i, phase := range []string{"running", "reconciling"} {
			r := create(fmt.Sprintf("cancel_unsafe_%d", i))
			// Fixture time must not precede the persisted host-generated revision when
			// Docker's clock is behind. Keep the production monotonic-time constraint.
			_, e := owner.Exec(ctx, `UPDATE execution.runs SET state=$1,version=2,updated_at=GREATEST(updated_at,clock_timestamp()) WHERE workspace_id=$2 AND id=$3`, phase, r.WorkspaceID, r.RunID)
			must(t, e)
			before := state(r)
			v, e := cancelService.Cancel(ctx, caller, r.RunID, "must not release")
			if !errors.Is(e, admit.ErrCancelUnsafe) || v.Found {
				t.Fatal("unsafe run canceled", e)
			}
			if before != state(r) {
				t.Fatal("unsafe cancellation wrote state")
			}
		}
	})
	t.Run("cancellation and new admissions serialize on the original quota row", func(t *testing.T) {
		at := time.Now().UTC()
		_, e := owner.Exec(ctx, `INSERT INTO commerce.budget_periods(workspace_id,budget_id,period_id,currency,starts_at,ends_at,limit_micro) VALUES('ws_a','cancel_race','p1','USD',$1,$2,60)`, at.Add(-time.Hour), at.Add(time.Hour))
		must(t, e)
		r, e := admissions.Admit(ctx, creationCaller, request("cancel_race_existing", "cancel_race"))
		must(t, e)
		var wg sync.WaitGroup
		errch := make(chan error, 17)
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := cancelService.Cancel(ctx, caller, r.RunID, "release race")
			errch <- e
		}()
		for i := range 16 {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, e := admissions.Admit(ctx, creationCaller, request(fmt.Sprintf("cancel_new_race_%d", i), "cancel_race"))
				if errors.Is(e, admit.ErrBudgetExceeded) {
					e = nil
				}
				errch <- e
			}(i)
		}
		wg.Wait()
		close(errch)
		for e := range errch {
			must(t, e)
		}
		assertCanceled(r)
		var held int64
		var count int
		must(t, owner.QueryRow(ctx, `SELECT reserved_micro FROM commerce.budget_periods WHERE workspace_id='ws_a' AND budget_id='cancel_race'`).Scan(&held))
		must(t, owner.QueryRow(ctx, `SELECT count(*) FROM commerce.reservations WHERE workspace_id='ws_a' AND budget_id='cancel_race' AND state='held'`).Scan(&count))
		if count > 1 || held != int64(count)*60 {
			t.Fatal("release/admission over-allocation", held, count)
		}
	})
	t.Run("released quota cannot commit without matching execution cancellation", func(t *testing.T) {
		r := create("cancel_partial_release")
		before := state(r)
		tx, e := cancelPool.Begin(ctx)
		must(t, e)
		defer func() { _ = tx.Rollback(context.Background()) }()
		_, e = tx.Exec(ctx, `SELECT set_config('mender.workspace_id','ws_a',true)`)
		must(t, e)
		_, e = tx.Exec(ctx, `UPDATE commerce.budget_periods SET reserved_micro=reserved_micro-60,revision=revision+1 WHERE workspace_id='ws_a' AND budget_id='budget_ok' AND period_id='p1'`)
		must(t, e)
		// Reach the deferred proof under test, rather than failing its earlier
		// non-decreasing timestamp check because host/container clocks differ.
		_, e = tx.Exec(ctx, `UPDATE commerce.reservations SET state='released',released_at=GREATEST(created_at,clock_timestamp()) WHERE workspace_id='ws_a' AND id=$1`, r.ReservationID)
		must(t, e)
		if e = tx.Commit(ctx); e == nil {
			t.Fatal("quota release bypassed execution receipt")
		}
		if state(r) != before {
			t.Fatal("incomplete release committed")
		}
	})
	t.Run("real protected HTTP cancels managed and legacy runs and rejects revoked scopes", func(t *testing.T) {
		h, closeAPI, e := bootstrap.BuildAPI(ctx, bootstrap.APIConfig{RunAPIEnabled: true, CoordinatedCancelEnabled: true, DatabaseURL: runtimeDSN, CancellationDatabaseURL: u.String()})
		must(t, e)
		defer closeAPI()
		call := func(key, path, body string) *httptest.ResponseRecorder {
			q := httptest.NewRequest("POST", path, strings.NewReader(body))
			q.Header.Set("Authorization", "Bearer "+key)
			q.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, q)
			return w
		}
		r := create("cancel_http_managed")
		path := "/api/v1/workspaces/ws_a/runs/" + r.RunID + "/cancel"
		readKey, _ := issue("ws_a", "sa_reader", []string{"run:read"})
		otherKey, _ := issue("ws_b", "sa_b", []string{"run:cancel"})
		for _, key := range []string{readKey, otherKey} {
			before := state(r)
			w := call(key, path, `{}`)
			if w.Code != 403 || before != state(r) {
				t.Fatal("denied cancellation mutated state", w.Code)
			}
		}
		if w := call(keyA, path, `{"reason":"actual HTTP cancellation"}`); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		assertCanceled(r)
		rotated, id := issue("ws_a", "sa_a", []string{"run:cancel"})
		before := state(r)
		if w := call(rotated, path, `{"reason":"replay"}`); w.Code != 200 || before != state(r) {
			t.Fatal("rotated authorized replay changed facts", w.Code)
		}
		must(t, identitypg.New(owner).Revoke(ctx, id))
		if w := call(rotated, path, `{}`); w.Code != 401 {
			t.Fatal("revoked replay authorized", w.Code)
		}
		_, e = owner.Exec(ctx, `INSERT INTO execution.runs(workspace_id,id,state,version,created_at,updated_at) VALUES('ws_a','legacy_cancel_phase5','queued',1,clock_timestamp(),clock_timestamp())`)
		must(t, e)
		if w := call(keyA, "/api/v1/workspaces/ws_a/runs/legacy_cancel_phase5/cancel", `{}`); w.Code != 200 {
			t.Fatal("legacy fallback failed", w.Code, w.Body.String())
		}
		if w := call(keyA, "/api/v1/workspaces/ws_a/runs/missing_cancel_phase5/cancel", `{}`); w.Code != 404 {
			t.Fatal("missing object not hidden", w.Code)
		}
	})
	t.Run("cancellation writer cannot admit read inputs alter identities or bypass row scope", func(t *testing.T) {
		for _, sql := range []string{`SELECT canonical_arguments FROM execution.run_admissions`, `SELECT digest FROM identity.api_keys`, `UPDATE commerce.budget_periods SET limit_micro=999999`, `UPDATE commerce.reservations SET amount_micro=0`, `INSERT INTO execution.runs(workspace_id,id,state,version,created_at,updated_at) VALUES('ws_a','injected','queued',1,now(),now())`, `DELETE FROM execution.run_cancellations`, `UPDATE execution.jobs SET blocked_reason='executor_not_configured'`} {
			if _, e := cancelPool.Exec(ctx, sql); e == nil {
				t.Fatal("ungranted operation accepted", sql)
			}
		}
		var n int
		for _, table := range []string{"commerce.reservations", "execution.run_cancellations", "execution.jobs"} {
			must(t, cancelPool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n))
			if n != 0 {
				t.Fatal("transaction workspace leaked")
			}
		}
		_, e = owner.Exec(ctx, "GRANT UPDATE(reason) ON execution.run_cancellations TO "+pgx.Identifier{role}.Sanitize())
		must(t, e)
		if e = database.CancellationRole(ctx, cancelPool); e == nil {
			t.Fatal("extra column grant not rejected")
		}
		_, e = owner.Exec(ctx, "REVOKE UPDATE(reason) ON execution.run_cancellations FROM "+pgx.Identifier{role}.Sanitize())
		must(t, e)
		must(t, database.CancellationRole(ctx, cancelPool))
	})
	t.Log("Real PostgreSQL coordinated cancellation: exact-once release, eight write rollbacks, old-period release, unsafe-state refusal, admission competition, deferred proofs, HTTP identity and dedicated role verified")
}
