//go:build integration

package integration_test

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	runpg "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/postgres"
	runapp "github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
	"github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/keycodec"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	"github.com/orz-i/mender/backend/migrations"
)

func openTemporaryRole(t *testing.T, ctx context.Context, owner *pgxpool.Pool, baseDSN, prefix string, grant func(context.Context, *pgxpool.Pool, string) error) (*pgxpool.Pool, string) {
	t.Helper()
	_, suffix, password, err := (keycodec.Codec{}).Generate()
	must(t, err)
	role := prefix + suffix
	_, err = owner.Exec(ctx, "CREATE ROLE "+pgx.Identifier{role}.Sanitize()+" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS PASSWORD '"+password+"'")
	must(t, err)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = owner.Exec(cleanup, "DROP OWNED BY "+pgx.Identifier{role}.Sanitize())
		_, _ = owner.Exec(cleanup, "DROP ROLE "+pgx.Identifier{role}.Sanitize())
	})
	must(t, grant(ctx, owner, role))
	u, err := url.Parse(baseDSN)
	must(t, err)
	u.User = url.UserPassword(role, password)
	pool, err := database.Open(ctx, u.String())
	must(t, err)
	t.Cleanup(pool.Close)
	return pool, role
}

func exerciseProviderResults(t *testing.T, ctx context.Context, owner, runtime *pgxpool.Pool, runtimeDSN string) {
	t.Helper()
	worker, _ := openTemporaryRole(t, ctx, owner, runtimeDSN, "mender_result_worker_", migrations.GrantWorker)
	reconciler, _ := openTemporaryRole(t, ctx, owner, runtimeDSN, "mender_reconciler_", migrations.GrantReconciler)
	must(t, database.WorkerRole(ctx, worker))
	must(t, database.ReconcilerRole(ctx, reconciler))
	if database.ReconcilerRole(ctx, owner) == nil || database.ReconcilerRole(ctx, runtime) == nil {
		t.Fatal("privileged/runtime role accepted as reconciler")
	}

	at := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	seed := func(run, deployment string) {
		_, err := owner.Exec(ctx, `INSERT INTO execution.runs(workspace_id,id,state,version,created_at,updated_at) VALUES('ws_result',$1,'queued',1,$2,$2)`, run, at)
		must(t, err)
		_, err = owner.Exec(ctx, `INSERT INTO execution.run_admissions(workspace_id,run_id,subject_id,credential_id,idempotency_key,request_hash,reservation_id,tool_version_id,toolset_version_id,connection_id,price_version_id,deployment_revision,budget_id,period_id,currency,reserved_micro,canonical_arguments,created_at)
VALUES('ws_result',$1,'sa_result','key_result',$1||'_idem',repeat('a',64),'res_'||$1,'tool_result','set_result','conn_result','price_result',$2,'budget_result','period_result','USD',0,'{}',$3)`, run, deployment, at)
		must(t, err)
		_, err = owner.Exec(ctx, `INSERT INTO execution.jobs(workspace_id,run_id,state,blocked_reason,available_at,created_at,updated_at) VALUES('ws_result',$1,'blocked','executor_not_configured',$2,$2,$2)`, run, at)
		must(t, err)
	}
	seed("run_result_success", "deploy_result_success")
	seed("run_result_order", "deploy_result_order")

	clock := &integrationWorkerClock{at: at.Add(5 * time.Second)}
	control, err := runapp.NewWorkerControl(runpg.NewWorkers(worker), clock)
	must(t, err)
	accept := func(run, deployment, workerID, requestID string) runapp.SubmissionRecord {
		count, e := control.Activate(ctx, "ws_result", []string{deployment}, 1)
		must(t, e)
		if count != 1 {
			t.Fatal("result fixture was not activated", run, count)
		}
		lease, e := control.LeaseOne(ctx, "ws_result", workerID, 30*time.Second)
		must(t, e)
		clock.at = clock.at.Add(time.Second)
		intent, e := control.BeginSubmission(ctx, lease, "submit."+run+".1")
		must(t, e)
		clock.at = clock.at.Add(time.Second)
		record, e := control.RecordSubmitted(ctx, intent, "provider_result", requestID, "task-"+run)
		must(t, e)
		var state string
		var leaseOwner *string
		var leaseUntil *time.Time
		must(t, owner.QueryRow(ctx, `SELECT state,lease_owner,lease_until FROM execution.jobs WHERE workspace_id='ws_result' AND run_id=$1`, run).Scan(&state, &leaseOwner, &leaseUntil))
		if state != "provider_waiting" || leaseOwner != nil || leaseUntil != nil {
			t.Fatal("accepted result fixture retained worker lease", run, state, leaseOwner, leaseUntil)
		}
		return record
	}
	successSubmission := accept("run_result_success", "deploy_result_success", "worker_result_success", "provider/result-success")
	orderSubmission := accept("run_result_order", "deploy_result_order", "worker_result_order", "provider/result-order")
	var persistedRun, persistedJob, persistedAttempt, persistedProvider, persistedRequest, persistedTask string
	must(t, owner.QueryRow(ctx, `SELECT r.state,j.state,a.state,a.provider_id,a.provider_request_id,a.external_task_id FROM execution.runs r JOIN execution.jobs j ON (j.workspace_id,j.run_id)=(r.workspace_id,r.id) JOIN execution.run_attempts a ON (a.workspace_id,a.run_id,a.attempt_no)=(r.workspace_id,r.id,1) WHERE r.workspace_id='ws_result' AND r.id='run_result_success'`).Scan(&persistedRun, &persistedJob, &persistedAttempt, &persistedProvider, &persistedRequest, &persistedTask))
	if persistedRun != "running" || persistedJob != "provider_waiting" || persistedAttempt != "submitted" || persistedProvider != "provider_result" || persistedRequest != "provider/result-success" || persistedTask != "task-run_result_success" {
		t.Fatal("accepted provider-result fixture facts are inconsistent", persistedRun, persistedJob, persistedAttempt, persistedProvider, persistedRequest, persistedTask)
	}
	t.Run("reconciler SQL permissions and pending bundle are usable", func(t *testing.T) {
		tx, e := reconciler.Begin(ctx)
		must(t, e)
		defer func() { _ = tx.Rollback(ctx) }()
		_, e = tx.Exec(ctx, `SELECT set_config('mender.workspace_id','ws_result',true)`)
		if e != nil {
			t.Fatal("reconciler workspace scope failed", e)
		}
		var scratch string
		e = tx.QueryRow(ctx, `SELECT state FROM execution.runs WHERE workspace_id='ws_result' AND id='run_result_success' FOR UPDATE`).Scan(&scratch)
		if e != nil {
			t.Fatal("reconciler Run lock failed", e)
		}
		e = tx.QueryRow(ctx, `SELECT state FROM execution.jobs WHERE workspace_id='ws_result' AND run_id='run_result_success' FOR UPDATE`).Scan(&scratch)
		if e != nil {
			t.Fatal("reconciler Job lock failed", e)
		}
		e = tx.QueryRow(ctx, `SELECT state FROM execution.run_attempts WHERE workspace_id='ws_result' AND run_id='run_result_success' AND attempt_no=1`).Scan(&scratch)
		if e != nil {
			t.Fatal("reconciler Attempt read lock failed", e)
		}
		probeAt := successSubmission.Attempt.SubmittedAt.Add(time.Second)
		_, e = tx.Exec(ctx, `INSERT INTO execution.provider_observations(workspace_id,run_id,observation_id,attempt_no,provider_id,provider_request_id,external_task_id,state,result_json,error_code,observed_at) VALUES('ws_result','run_result_success','obs_permission_probe',1,'provider_result','provider/result-success','task-run_result_success','pending',NULL,NULL,$1)`, probeAt)
		if e != nil {
			t.Fatal("reconciler pending INSERT failed", e)
		}
		_, e = tx.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`)
		if e != nil {
			t.Fatal("reconciler pending deferred bundle failed", e)
		}
	})

	results, err := runapp.NewProviderResults(runpg.NewProviderResults(reconciler))
	must(t, err)
	pendingAt := successSubmission.Attempt.SubmittedAt.Add(2 * time.Second)
	pending := domain.ProviderObservation{WorkspaceID: "ws_result", RunID: "run_result_success", ObservationID: "obs_success_pending", AttemptNo: 1, ProviderID: "provider_result", ProviderRequestID: "provider/result-success", ExternalTaskID: "task-run_result_success", State: domain.ProviderPending, ObservedAt: pendingAt}
	record, err := results.Observe(ctx, pending)
	if err != nil {
		t.Fatalf("pending provider observation failed: %v", err)
	}
	if record.Run.State != domain.Running || record.Job.State != domain.JobProviderWaiting || record.Observation.State != domain.ProviderPending {
		t.Fatal("pending observation changed execution state", record)
	}

	terminalAt := pendingAt.Add(time.Second)
	succeeded := domain.ProviderObservation{WorkspaceID: "ws_result", RunID: "run_result_success", ObservationID: "obs_success_terminal", AttemptNo: 1, ProviderID: "provider_result", ProviderRequestID: "provider/result-success", ExternalTaskID: "task-run_result_success", State: domain.ProviderSucceeded, ResultJSON: `{"answer":42}`, ObservedAt: terminalAt}
	record, err = results.Observe(ctx, succeeded)
	if err != nil {
		t.Fatalf("terminal provider observation failed: %v", err)
	}
	if record.Run.State != domain.Succeeded || record.Job.State != domain.JobFinished || !record.Job.StoppedAt.Equal(terminalAt) {
		t.Fatal("terminal provider result did not finish Run/Job", record)
	}
	replayed, err := results.Observe(ctx, succeeded)
	must(t, err)
	if replayed.Run.Version != record.Run.Version || replayed.Observation.ObservationID != succeeded.ObservationID {
		t.Fatal("terminal provider observation replay mutated execution", replayed, record)
	}
	conflict := succeeded
	conflict.ResultJSON = `{"answer":43}`
	if _, err = results.Observe(ctx, conflict); !errors.Is(err, runapp.ErrProviderResultConflict) {
		t.Fatal("same observation id accepted conflicting provider facts", err)
	}
	late := succeeded
	late.ObservationID = "obs_success_late"
	late.ObservedAt = terminalAt.Add(time.Second)
	if _, err = results.Observe(ctx, late); !errors.Is(err, runapp.ErrProviderAlreadyTerminal) {
		t.Fatal("new observation appended after terminal result", err)
	}

	orderAt := orderSubmission.Attempt.SubmittedAt.Add(5 * time.Second)
	_, err = results.Observe(ctx, domain.ProviderObservation{WorkspaceID: "ws_result", RunID: "run_result_order", ObservationID: "obs_order_new", AttemptNo: 1, ProviderID: "provider_result", ProviderRequestID: "provider/result-order", ExternalTaskID: "task-run_result_order", State: domain.ProviderPending, ObservedAt: orderAt})
	if err != nil {
		t.Fatalf("newer pending provider observation failed: %v", err)
	}
	_, err = results.Observe(ctx, domain.ProviderObservation{WorkspaceID: "ws_result", RunID: "run_result_order", ObservationID: "obs_order_old", AttemptNo: 1, ProviderID: "provider_result", ProviderRequestID: "provider/result-order", ExternalTaskID: "task-run_result_order", State: domain.ProviderPending, ObservedAt: orderAt.Add(-time.Second)})
	if !errors.Is(err, runapp.ErrProviderResultConflict) {
		t.Fatal("out-of-order provider observation accepted", err)
	}

	t.Run("reconciler role is result-only and database bundles reject invented terminal state", func(t *testing.T) {
		for _, sql := range []string{
			`SELECT canonical_arguments FROM execution.run_admissions`,
			`SELECT * FROM identity.api_keys`,
			`SELECT * FROM commerce.budget_periods`,
			`SELECT * FROM connections.connections`,
			`SELECT * FROM supply.deployments`,
			`UPDATE execution.run_attempts SET state='unknown'`,
			`DELETE FROM execution.provider_observations`,
		} {
			if _, e := reconciler.Exec(ctx, sql); e == nil {
				t.Fatal("reconciler role accepted forbidden operation", sql)
			}
		}
		if _, e := reconciler.Exec(ctx, `SELECT set_config('mender.workspace_id','ws_result',false)`); e != nil {
			t.Fatal(e)
		}
		if _, e := reconciler.Exec(ctx, `UPDATE execution.runs SET state='failed',version=version+1,updated_at=updated_at+interval '1 second' WHERE workspace_id='ws_result' AND id='run_result_order'`); e == nil {
			t.Fatal("database accepted terminal Run without provider observation bundle")
		}
	})

	t.Log("real PostgreSQL provider result lifecycle verified: accepted submission releases Worker lease, observations are append-only/idempotent/monotonic, terminal result atomically finishes Run and Job, and reconciler role is isolated")
}
