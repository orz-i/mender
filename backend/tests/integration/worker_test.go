//go:build integration

package integration_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	runpg "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/postgres"
	runapp "github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/keycodec"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	"github.com/orz-i/mender/backend/migrations"
)

type integrationWorkerClock struct{ at time.Time }

func (c *integrationWorkerClock) Now() time.Time { return c.at }

func exerciseWorkerLeases(t *testing.T, ctx context.Context, owner, runtime *pgxpool.Pool, runtimeDSN string) {
	t.Helper()
	_, suffix, password, err := (keycodec.Codec{}).Generate()
	must(t, err)
	role := "mender_worker_" + suffix
	_, err = owner.Exec(ctx, "CREATE ROLE "+pgx.Identifier{role}.Sanitize()+" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS PASSWORD '"+password+"'")
	must(t, err)
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, e := owner.Exec(cleanup, "DROP OWNED BY "+pgx.Identifier{role}.Sanitize()); e != nil {
			t.Error("worker role grants cleanup failed")
		}
		if _, e := owner.Exec(cleanup, "DROP ROLE "+pgx.Identifier{role}.Sanitize()); e != nil {
			t.Error("worker role cleanup failed")
		}
	}()
	must(t, migrations.GrantWorker(ctx, owner, role))
	u, err := url.Parse(runtimeDSN)
	must(t, err)
	u.User = url.UserPassword(role, password)
	worker, err := database.Open(ctx, u.String())
	must(t, err)
	defer worker.Close()
	must(t, database.WorkerRole(ctx, worker))
	if err = database.WorkerRole(ctx, owner); err == nil {
		t.Fatal("owner accepted as worker role")
	}
	if err = database.WorkerRole(ctx, runtime); err == nil {
		t.Fatal("runtime reader accepted as worker role")
	}

	at := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	seed := func(workspace, run, deployment string, priority int) {
		_, e := owner.Exec(ctx, `INSERT INTO execution.runs(workspace_id,id,state,version,created_at,updated_at) VALUES($1,$2,'queued',1,$3,$3)`, workspace, run, at)
		must(t, e)
		_, e = owner.Exec(ctx, `INSERT INTO execution.run_admissions(workspace_id,run_id,subject_id,credential_id,idempotency_key,request_hash,reservation_id,tool_version_id,toolset_version_id,connection_id,price_version_id,deployment_revision,budget_id,period_id,currency,reserved_micro,canonical_arguments,created_at)
VALUES($1,$2,'sa_worker','fixture_key_worker',$2||'_idem',repeat('a',64),'res_'||$2,'tool_worker','set_worker','conn_worker','price_worker',$3,'budget_worker','period_worker','USD',0,'{}',$4)`, workspace, run, deployment, at)
		must(t, e)
		_, e = owner.Exec(ctx, `INSERT INTO execution.jobs(workspace_id,run_id,state,blocked_reason,available_at,priority,created_at,updated_at) VALUES($1,$2,'blocked','executor_not_configured',$3,$4,$3,$3)`, workspace, run, at, priority)
		must(t, e)
	}
	seed("ws_a", "run_worker_main", "deploy_worker", 20)
	seed("ws_a", "run_worker_other", "deploy_other", 10)
	seed("ws_b", "run_worker_main", "deploy_worker", 20)

	repo := runpg.NewWorkers(worker)
	clock := &integrationWorkerClock{at: at.Add(10 * time.Second)}
	control, err := runapp.NewWorkerControl(repo, clock)
	must(t, err)

	t.Run("worker role is local control only and RLS does not leak", func(t *testing.T) {
		for _, sql := range []string{
			`SELECT canonical_arguments FROM execution.run_admissions`,
			`SELECT * FROM identity.api_keys`,
			`SELECT * FROM commerce.budget_periods`,
			`UPDATE execution.runs SET state='running'`,
			`UPDATE execution.jobs SET max_attempts=99`,
			`DELETE FROM execution.run_attempts`,
		} {
			if _, e := worker.Exec(ctx, sql); e == nil {
				t.Fatal("worker role accepted unrelated operation", sql)
			}
		}
		var n int
		must(t, worker.QueryRow(ctx, `SELECT count(*) FROM execution.jobs`).Scan(&n))
		if n != 0 {
			t.Fatal("workspace RLS leaked without scope", n)
		}
	})

	t.Run("activation is workspace and deployment revision scoped", func(t *testing.T) {
		count, e := control.Activate(ctx, "ws_a", []string{"deploy_worker"}, 10)
		must(t, e)
		if count != 1 {
			t.Fatal("unexpected activation count", count)
		}
		var state string
		must(t, owner.QueryRow(ctx, `SELECT state FROM execution.jobs WHERE workspace_id='ws_a' AND run_id='run_worker_main'`).Scan(&state))
		if state != "queued" {
			t.Fatal("allowed job not activated", state)
		}
		must(t, owner.QueryRow(ctx, `SELECT state FROM execution.jobs WHERE workspace_id='ws_a' AND run_id='run_worker_other'`).Scan(&state))
		if state != "blocked" {
			t.Fatal("unconfigured deployment activated", state)
		}
		must(t, owner.QueryRow(ctx, `SELECT state FROM execution.jobs WHERE workspace_id='ws_b' AND run_id='run_worker_main'`).Scan(&state))
		if state != "blocked" {
			t.Fatal("other workspace activated", state)
		}
	})

	t.Run("concurrent lease has one winner and one immutable attempt", func(t *testing.T) {
		var wins, empty, failed atomic.Int32
		leases := make(chan runapp.Lease, 16)
		var group sync.WaitGroup
		for i := range 16 {
			group.Add(1)
			go func(i int) {
				defer group.Done()
				lease, e := control.LeaseOne(ctx, "ws_a", fmt.Sprintf("worker_%02d", i), 30*time.Second)
				switch {
				case e == nil:
					wins.Add(1)
					leases <- lease
				case errors.Is(e, runapp.ErrNoWork):
					empty.Add(1)
				default:
					failed.Add(1)
				}
			}(i)
		}
		group.Wait()
		close(leases)
		if wins.Load() != 1 || empty.Load() != 15 || failed.Load() != 0 {
			t.Fatalf("wins=%d empty=%d failed=%d", wins.Load(), empty.Load(), failed.Load())
		}
		lease := <-leases
		if lease.Generation != 1 || lease.AttemptNo != 1 {
			t.Fatal(lease)
		}
		var attempts int
		var state, ownerID string
		var generation int64
		must(t, owner.QueryRow(ctx, `SELECT count(*) FROM execution.run_attempts WHERE workspace_id='ws_a' AND run_id='run_worker_main'`).Scan(&attempts))
		must(t, owner.QueryRow(ctx, `SELECT state,lease_owner,lease_generation FROM execution.jobs WHERE workspace_id='ws_a' AND run_id='run_worker_main'`).Scan(&state, &ownerID, &generation))
		if attempts != 1 || state != "leased" || ownerID != lease.WorkerID || generation != 1 {
			t.Fatal(attempts, state, ownerID, generation, lease)
		}

		clock.at = clock.at.Add(10 * time.Second)
		renewed, e := control.Heartbeat(ctx, lease, 30*time.Second)
		must(t, e)
		if !renewed.LeaseUntil.After(lease.LeaseUntil) {
			t.Fatal("heartbeat did not extend lease")
		}
		clock.at = clock.at.Add(time.Second)
		must(t, control.ReleaseBeforeSubmit(ctx, renewed, 0))
		var attemptState string
		must(t, owner.QueryRow(ctx, `SELECT state FROM execution.run_attempts WHERE workspace_id='ws_a' AND run_id='run_worker_main' AND attempt_no=1`).Scan(&attemptState))
		if attemptState != "released" {
			t.Fatal(attemptState)
		}

		clock.at = clock.at.Add(time.Second)
		second, e := control.LeaseOne(ctx, "ws_a", "worker_second", 30*time.Second)
		must(t, e)
		if second.Generation != 2 || second.AttemptNo != 2 {
			t.Fatal(second)
		}
		clock.at = clock.at.Add(time.Second)
		if _, e = control.Heartbeat(ctx, renewed, 30*time.Second); !errors.Is(e, runapp.ErrWorkerLeaseLost) {
			t.Fatal("stale generation changed newer lease", e)
		}

		clock.at = second.LeaseUntil
		recovered, e := control.RecoverExpired(ctx, "ws_a", 10)
		must(t, e)
		if recovered != 1 {
			t.Fatal("expired lease was not recovered", recovered)
		}
		must(t, owner.QueryRow(ctx, `SELECT state FROM execution.run_attempts WHERE workspace_id='ws_a' AND run_id='run_worker_main' AND attempt_no=2`).Scan(&attemptState))
		if attemptState != "expired" {
			t.Fatal("attempt expiry not durable", attemptState)
		}
		clock.at = clock.at.Add(time.Second)
		third, e := control.LeaseOne(ctx, "ws_a", "worker_third", 30*time.Second)
		must(t, e)
		if third.Generation != 3 {
			t.Fatal("fencing generation did not advance", third)
		}
		clock.at = clock.at.Add(time.Second)
		if _, e = control.Heartbeat(ctx, second, 30*time.Second); !errors.Is(e, runapp.ErrWorkerLeaseLost) {
			t.Fatal("expired worker overwrote generation 3", e)
		}
	})

	t.Log("real PostgreSQL worker control verified: deployment/workspace activation, SKIP LOCKED single winner, Attempt bundle, heartbeat, release, expiry recovery, monotonic fencing, stale-worker rejection and least-privilege role")
}
