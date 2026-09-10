//go:build integration

package integration_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/bootstrap"
	"github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/keycodec"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	admit "github.com/orz-i/mender/backend/internal/processes/admission/application"
	"github.com/orz-i/mender/backend/migrations"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Only resolver and authorization policy are fixtures. All quota, Run, job, Outbox,
// RLS, uniqueness, locks, deferred constraints and transaction operations below are real SQL.
type admissionAuth struct{ denied atomic.Bool }

func (a *admissionAuth) Authorize(ctx context.Context, c admit.Caller, q admit.Request) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if a.denied.Load() || c.SubjectID != "sa_a" || c.CredentialID == "" || (c.WorkspaceID != "ws_a" && c.WorkspaceID != "ws_b") {
		return admit.ErrForbidden
	}
	return nil
}

type admissionResolver struct{}

func (admissionResolver) Resolve(ctx context.Context, _ admit.Caller, q admit.Request, _ string) (admit.Plan, error) {
	if e := ctx.Err(); e != nil {
		return admit.Plan{}, e
	}
	return admit.Plan{ToolVersionID: q.ToolVersionID, ToolsetVersionID: q.ToolsetVersionID, ConnectionID: q.ConnectionID, PriceVersionID: "price_v1", DeploymentRevision: "deployment_v1", Currency: q.Currency, ReserveMicro: 60, ValidUntil: time.Now().Add(time.Hour)}, nil
}

func exerciseAdmission(t *testing.T, ctx context.Context, owner, runtime *pgxpool.Pool, runtimeDSN, keyA string) {
	t.Helper()
	_, suffix, password, e := (keycodec.Codec{}).Generate()
	must(t, e)
	role := "mender_admit_" + suffix
	_, e = owner.Exec(ctx, "CREATE ROLE "+pgx.Identifier{role}.Sanitize()+" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS PASSWORD '"+password+"'")
	must(t, e)
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, e := owner.Exec(c, "DROP OWNED BY "+pgx.Identifier{role}.Sanitize()); e != nil {
			t.Error("admission role grants cleanup failed")
		}
		if _, e := owner.Exec(c, "DROP ROLE "+pgx.Identifier{role}.Sanitize()); e != nil {
			t.Error("admission role cleanup failed")
		}
	}()
	must(t, migrations.GrantAdmission(ctx, owner, role))
	u, e := url.Parse(runtimeDSN)
	must(t, e)
	u.User = url.UserPassword(role, password)
	writer, e := database.Open(ctx, u.String())
	must(t, e)
	defer writer.Close()
	must(t, database.AdmissionRole(ctx, writer))
	auth := &admissionAuth{}
	svc, e := bootstrap.BuildAdmission(ctx, writer, auth, admissionResolver{})
	must(t, e)
	for _, pool := range []*pgxpool.Pool{owner, runtime} {
		if s, e := bootstrap.BuildAdmission(ctx, pool, auth, admissionResolver{}); e == nil || s != nil {
			t.Fatal("wrong database role accepted for admission")
		}
	}
	if s, e := bootstrap.BuildAdmission(ctx, writer, nil, admissionResolver{}); e == nil || s != nil {
		t.Fatal("missing real authority port silently supplied")
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	budget := func(workspace, id string, limit int64) {
		_, e := owner.Exec(ctx, `INSERT INTO commerce.budget_periods(workspace_id,budget_id,period_id,currency,starts_at,ends_at,limit_micro) VALUES($1,$2,'p1','USD',$3,$4,$5)`, workspace, id, at.Add(-time.Hour), at.Add(time.Hour), limit)
		must(t, e)
	}
	for _, w := range []string{"ws_a", "ws_b"} {
		budget(w, "budget_ok", 100000)
		budget(w, "budget_tight", 100)
	}
	caller := admit.Caller{WorkspaceID: "ws_a", SubjectID: "sa_a", CredentialID: "fixture_key"}
	req := func(key, budget string) admit.Request {
		return admit.Request{IdempotencyKey: key, ToolVersionID: "tool_v1", ToolsetVersionID: "set_v1", ConnectionID: "conn_a", BudgetID: budget, PeriodID: "p1", Currency: "USD", MaxChargeMicro: "100", Arguments: []byte(`{"n":9007199254740993,"secret":"fixture-not-a-real-secret"}`)}
	}
	counts := func() string {
		var s strings.Builder
		for _, tab := range []string{"commerce.reservations", "execution.runs", "execution.run_admissions", "execution.jobs", "execution.outbox"} {
			var n int64
			must(t, owner.QueryRow(ctx, "SELECT count(*) FROM "+tab).Scan(&n))
			fmt.Fprintf(&s, "%d/", n)
		}
		var n int64
		must(t, owner.QueryRow(ctx, "SELECT COALESCE(sum(reserved_micro),0) FROM commerce.budget_periods").Scan(&n))
		fmt.Fprintf(&s, "%d", n)
		return s.String()
	}
	t.Run("same logical key concurrent replay persists one complete admission", func(t *testing.T) {
		var wg sync.WaitGroup
		results := make(chan admit.Receipt, 12)
		errs := make(chan error, 12)
		for range 12 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r, e := svc.Admit(ctx, caller, req("idem_concurrent", "budget_ok"))
				results <- r
				errs <- e
			}()
		}
		wg.Wait()
		close(results)
		close(errs)
		for e := range errs {
			must(t, e)
		}
		ids := map[string]bool{}
		newCount := 0
		var got admit.Receipt
		for r := range results {
			got = r
			ids[r.RunID] = true
			if !r.Replayed {
				newCount++
			}
		}
		if len(ids) != 1 || newCount != 1 {
			t.Fatal("duplicate admission", len(ids), newCount)
		}
		var n int
		must(t, owner.QueryRow(ctx, `SELECT count(*) FROM commerce.reservations WHERE workspace_id='ws_a' AND run_id=$1`, got.RunID).Scan(&n))
		if n != 1 {
			t.Fatal("not exactly one reservation")
		}
		var state, reason string
		must(t, owner.QueryRow(ctx, `SELECT state,blocked_reason FROM execution.jobs WHERE workspace_id='ws_a' AND run_id=$1`, got.RunID).Scan(&state, &reason))
		if state != "blocked" || reason != "executor_not_configured" {
			t.Fatal("premature runnable job")
		}
		var payload, delivery string
		must(t, owner.QueryRow(ctx, `SELECT payload::text,delivery_state FROM execution.outbox WHERE workspace_id='ws_a' AND run_id=$1`, got.RunID).Scan(&payload, &delivery))
		if strings.Contains(payload, "secret") || delivery != "pending" {
			t.Fatal("unsafe event projection")
		}
		var canonical string
		must(t, owner.QueryRow(ctx, `SELECT canonical_arguments FROM execution.run_admissions WHERE workspace_id='ws_a' AND run_id=$1`, got.RunID).Scan(&canonical))
		if !strings.Contains(canonical, "9007199254740993") {
			t.Fatal("large integer lost")
		}
		before := counts()
		q := req("idem_concurrent", "budget_ok")
		q.MaxChargeMicro = "101"
		if _, e := svc.Admit(ctx, caller, q); !errors.Is(e, admit.ErrConflict) {
			t.Fatal(e)
		}
		if counts() != before {
			t.Fatal("conflict wrote state")
		}
		auth.denied.Store(true)
		if _, e := svc.Admit(ctx, caller, req("idem_concurrent", "budget_ok")); !errors.Is(e, admit.ErrForbidden) {
			t.Fatal("replay bypassed revocation")
		}
		auth.denied.Store(false)
		rotated := caller
		rotated.CredentialID = "rotated_key"
		r, e := svc.Admit(ctx, rotated, req("idem_concurrent", "budget_ok"))
		must(t, e)
		if !r.Replayed || r.RunID != got.RunID {
			t.Fatal("key rotation duplicated operation")
		}
		other := caller
		other.WorkspaceID = "ws_b"
		r, e = svc.Admit(ctx, other, req("idem_concurrent", "budget_ok"))
		must(t, e)
		if r.Replayed || r.RunID == got.RunID {
			t.Fatal("idempotency crossed tenant")
		}
		// Existing query/cancel endpoint remains safe while quota-release coordination is absent.
		h, closeIt, e := bootstrap.BuildAPI(ctx, bootstrap.APIConfig{RunAPIEnabled: true, DatabaseURL: runtimeDSN})
		must(t, e)
		defer closeIt()
		request := httptest.NewRequest("POST", "/api/v1/workspaces/ws_a/runs/"+got.RunID+"/cancel", strings.NewReader(`{}`))
		request.Header.Set("Authorization", "Bearer "+keyA)
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		h.ServeHTTP(response, request)
		if response.Code != 409 || !strings.Contains(response.Body.String(), "ADMISSION_CANCEL_UNAVAILABLE") {
			t.Fatal("legacy cancel bypassed quota/job coordination", response.Code, response.Body.String())
		}
		must(t, owner.QueryRow(ctx, `SELECT state FROM execution.runs WHERE workspace_id='ws_a' AND id=$1`, got.RunID).Scan(&state))
		if state != "queued" {
			t.Fatal("failed cancellation mutated run")
		}
	})
	t.Run("different keys contend for one bounded allowance without overspend", func(t *testing.T) {
		var wins, denials, failures atomic.Int32
		var wg sync.WaitGroup
		for i := range 16 {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, e := svc.Admit(ctx, caller, req(fmt.Sprintf("budget_race_%02d", i), "budget_tight"))
				switch {
				case e == nil:
					wins.Add(1)
				case errors.Is(e, admit.ErrBudgetExceeded):
					denials.Add(1)
				default:
					failures.Add(1)
				}
			}(i)
		}
		wg.Wait()
		if wins.Load() != 1 || denials.Load() != 15 || failures.Load() != 0 {
			t.Fatalf("wins=%d denied=%d failed=%d", wins.Load(), denials.Load(), failures.Load())
		}
		var held int64
		must(t, owner.QueryRow(ctx, `SELECT reserved_micro FROM commerce.budget_periods WHERE workspace_id='ws_a' AND budget_id='budget_tight'`).Scan(&held))
		if held != 60 {
			t.Fatal("quota penetrated", held)
		}
	})
	t.Run("every persistent write fault fully rolls back and allows same-key retry", func(t *testing.T) {
		for i, table := range []string{"commerce.budget_periods", "commerce.reservations", "execution.runs", "execution.run_admissions", "execution.jobs", "execution.outbox"} {
			t.Run(table, func(t *testing.T) {
				before := counts()
				_, e := owner.Exec(ctx, "ALTER TABLE "+table+" ADD CONSTRAINT reject_admission_test CHECK(false) NOT VALID")
				must(t, e)
				q := req(fmt.Sprintf("fault_step_%02d", i), "budget_ok")
				r, e := svc.Admit(ctx, caller, q)
				_, drop := owner.Exec(ctx, "ALTER TABLE "+table+" DROP CONSTRAINT reject_admission_test")
				must(t, drop)
				if e == nil || r.RunID != "" {
					t.Fatal("fault incorrectly reported success")
				}
				if counts() != before {
					t.Fatal("partial transaction persisted", table, before, counts())
				}
				r, e = svc.Admit(ctx, caller, q)
				must(t, e)
				if r.Replayed {
					t.Fatal("rolled-back idempotency survived")
				}
			})
		}
	})
	t.Run("deferred quota sum cannot be changed without a reservation", func(t *testing.T) {
		before := counts()
		tx, e := writer.Begin(ctx)
		must(t, e)
		defer func() { _ = tx.Rollback(context.Background()) }()
		_, e = tx.Exec(ctx, `SELECT set_config('mender.workspace_id','ws_a',true)`)
		must(t, e)
		_, e = tx.Exec(ctx, `UPDATE commerce.budget_periods SET reserved_micro=reserved_micro+1,revision=revision+1 WHERE workspace_id='ws_a' AND budget_id='budget_ok'`)
		must(t, e)
		if e = tx.Commit(ctx); e == nil {
			t.Fatal("unbalanced quota committed")
		}
		if counts() != before {
			t.Fatal("constraint failure did not roll back")
		}
	})
	t.Run("changing RLS scope cannot bypass the deferred quota constraint", func(t *testing.T) {
		before := counts()
		tx, e := writer.Begin(ctx)
		must(t, e)
		defer func() { _ = tx.Rollback(context.Background()) }()
		_, e = tx.Exec(ctx, `SELECT set_config('mender.workspace_id','ws_a',true)`)
		must(t, e)
		_, e = tx.Exec(ctx, `UPDATE commerce.budget_periods SET reserved_micro=reserved_micro+1,revision=revision+1 WHERE workspace_id='ws_a' AND budget_id='budget_ok'`)
		must(t, e)
		_, e = tx.Exec(ctx, `SELECT set_config('mender.workspace_id','ws_b',true)`)
		must(t, e)
		if e = tx.Commit(ctx); e == nil {
			t.Fatal("RLS context change bypassed invariant")
		}
		if counts() != before {
			t.Fatal("invalid reservation change persisted")
		}
	})
	t.Run("expired period and wrong currency fail without creating a run", func(t *testing.T) {
		budget("ws_a", "budget_expired", 100)
		_, e := owner.Exec(ctx, `UPDATE commerce.budget_periods SET ends_at=$1 WHERE workspace_id='ws_a' AND budget_id='budget_expired'`, at.Add(-time.Minute))
		must(t, e)
		before := counts()
		if _, e := svc.Admit(ctx, caller, req("expired_period", "budget_expired")); !errors.Is(e, admit.ErrBudgetUnavailable) {
			t.Fatal(e)
		}
		q := req("wrong_currency", "budget_ok")
		q.Currency = "EUR"
		if _, e := svc.Admit(ctx, caller, q); !errors.Is(e, admit.ErrBudgetUnavailable) {
			t.Fatal(e)
		}
		if counts() != before {
			t.Fatal("rejected budget wrote state")
		}
	})
	t.Run("writer and reader roles remain separated and RLS context never escapes", func(t *testing.T) {
		var n int
		for _, table := range []string{"commerce.budget_periods", "commerce.reservations", "execution.run_admissions"} {
			must(t, writer.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n))
			if n != 0 {
				t.Fatal("pooled workspace leaked", table)
			}
		}
		for _, sql := range []string{`UPDATE commerce.budget_periods SET limit_micro=1000000`, `DELETE FROM commerce.reservations`, `UPDATE execution.jobs SET state='blocked'`, `UPDATE execution.runs SET state='canceled'`, `DELETE FROM execution.outbox`} {
			if _, e := writer.Exec(ctx, sql); e == nil {
				t.Fatal("ungranted writer operation accepted", sql)
			}
		}
		if _, e := runtime.Exec(ctx, `UPDATE commerce.budget_periods SET reserved_micro=0`); e == nil {
			t.Fatal("read role obtained quota writes")
		}
		if _, e := runtime.Exec(ctx, `SELECT canonical_arguments FROM execution.run_admissions`); e == nil {
			t.Fatal("read role obtained arguments")
		}
		tx, e := writer.Begin(ctx)
		must(t, e)
		defer func() { _ = tx.Rollback(context.Background()) }()
		_, e = tx.Exec(ctx, `SELECT set_config('mender.workspace_id','ws_a',true)`)
		must(t, e)
		_, e = tx.Exec(ctx, `INSERT INTO execution.runs(workspace_id,id,state,version,created_at,updated_at) VALUES('ws_b','forbidden_admission','queued',1,now(),now())`)
		if e == nil {
			t.Fatal("cross-workspace Run insert succeeded")
		}
	})
	t.Run("coordinated cancellation with real quota and identity", func(t *testing.T) {
		exerciseCancellation(t, ctx, owner, runtime, writer, runtimeDSN, keyA, svc, caller)
	})
	t.Log("Real PostgreSQL atomic admission: idempotency/rotation/tenant isolation, quota race, six write-fault rollbacks, deferred totals, immutable blocked jobs, no-secret Outbox and legacy-cancel guard passed")
}
