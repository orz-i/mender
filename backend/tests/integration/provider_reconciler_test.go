//go:build integration

package integration_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	runpg "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/postgres"
	supplystatus "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/supplystatus"
	runapp "github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
	supply "github.com/orz-i/mender/backend/internal/contexts/supply/public"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	"github.com/orz-i/mender/backend/migrations"
)

type integrationStatusReader struct {
	responses []supply.StatusObservation
	err       error
	calls     int
	queries   []supply.StatusQuery
}

func (r *integrationStatusReader) QueryStatus(_ context.Context, query supply.StatusQuery) (supply.StatusObservation, error) {
	r.calls++
	r.queries = append(r.queries, query)
	if r.err != nil {
		return supply.StatusObservation{}, r.err
	}
	if len(r.responses) == 0 {
		return supply.StatusObservation{}, supply.ErrProviderStatusUnavailable
	}
	value := r.responses[0]
	r.responses = r.responses[1:]
	return value, nil
}

func exerciseProviderReconciler(t *testing.T, ctx context.Context, owner, runtime *pgxpool.Pool, runtimeDSN string) {
	t.Helper()
	worker, _ := openTemporaryRole(t, ctx, owner, runtimeDSN, "mender_poll_worker_", migrations.GrantWorker)
	reconcilerPool, _ := openTemporaryRole(t, ctx, owner, runtimeDSN, "mender_poll_reconciler_", migrations.GrantReconciler)
	must(t, database.WorkerRole(ctx, worker))
	must(t, database.ReconcilerRole(ctx, reconcilerPool))
	if database.ReconcilerRole(ctx, runtime) == nil {
		t.Fatal("runtime role accepted as provider reconciler")
	}

	base := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	seed := func(run, deployment string) {
		_, err := owner.Exec(ctx, `INSERT INTO execution.runs(workspace_id,id,state,version,created_at,updated_at) VALUES('ws_poll',$1,'queued',1,$2,$2)`, run, base)
		must(t, err)
		_, err = owner.Exec(ctx, `INSERT INTO execution.run_admissions(workspace_id,run_id,subject_id,credential_id,idempotency_key,request_hash,reservation_id,tool_version_id,toolset_version_id,connection_id,price_version_id,deployment_revision,budget_id,period_id,currency,reserved_micro,canonical_arguments,created_at)
VALUES('ws_poll',$1,'sa_poll','key_poll',$1||'_idem',repeat('b',64),'res_'||$1,'tool_poll','set_poll','conn_poll','price_poll',$2,'budget_poll','period_poll','USD',0,'{}',$3)`, run, deployment, base)
		must(t, err)
		_, err = owner.Exec(ctx, `INSERT INTO execution.jobs(workspace_id,run_id,state,blocked_reason,available_at,created_at,updated_at) VALUES('ws_poll',$1,'blocked','executor_not_configured',$2,$2,$2)`, run, base)
		must(t, err)
	}
	clock := &integrationWorkerClock{at: base.Add(5 * time.Second)}
	control, err := runapp.NewWorkerControl(runpg.NewWorkers(worker), clock)
	must(t, err)
	accept := func(run, deployment, requestID string) runapp.SubmissionRecord {
		seed(run, deployment)
		count, e := control.Activate(ctx, "ws_poll", []string{deployment}, 1)
		must(t, e)
		if count != 1 {
			t.Fatal("poll fixture activation mismatch", run, count)
		}
		lease, e := control.LeaseOne(ctx, "ws_poll", "worker_poll", 30*time.Second)
		must(t, e)
		clock.at = clock.at.Add(time.Second)
		intent, e := control.BeginSubmission(ctx, lease, "submit."+run+".1")
		must(t, e)
		clock.at = clock.at.Add(time.Second)
		record, e := control.RecordSubmitted(ctx, intent, "provider_poll", requestID, "task-"+run)
		must(t, e)
		return record
	}

	submitted := accept("run_poll_a", "deploy_poll_a", "provider/request-poll-a")
	pendingAt := submitted.Attempt.SubmittedAt.Add(time.Second)
	succeededAt := pendingAt.Add(time.Second)
	reader := &integrationStatusReader{responses: []supply.StatusObservation{
		{ObservationID: "obs.poll.a.pending", State: supply.StatusPending, ObservedAt: pendingAt},
		{ObservationID: "obs.poll.a.success", State: supply.StatusSucceeded, ResultJSON: `{"value":"done"}`, ObservedAt: succeededAt},
	}}
	statusSource, err := supplystatus.New(map[string]supply.ProviderStatusReader{"provider_poll": reader})
	must(t, err)
	results, err := runapp.NewProviderResults(runpg.NewProviderResults(reconcilerPool))
	must(t, err)
	reconciler, err := runapp.NewProviderReconciler(runpg.NewProviderReconciliation(reconcilerPool), statusSource, results)
	must(t, err)

	record, err := reconciler.ReconcileOne(ctx, "ws_poll")
	must(t, err)
	if record.Observation.State != domain.ProviderPending || record.Run.State != domain.Running || record.Job.State != domain.JobProviderWaiting {
		t.Fatal("pending provider poll changed terminal state", record)
	}
	record, err = reconciler.ReconcileOne(ctx, "ws_poll")
	must(t, err)
	if record.Observation.State != domain.ProviderSucceeded || record.Run.State != domain.Succeeded || record.Job.State != domain.JobFinished {
		t.Fatal("provider success did not converge", record)
	}
	if reader.calls != 2 || len(reader.queries) != 2 || reader.queries[0].ProviderID != "provider_poll" || reader.queries[0].ProviderRequestID != "provider/request-poll-a" || reader.queries[0].ExternalTaskID != "task-run_poll_a" {
		t.Fatal("provider status was not routed by durable identity", reader.calls, reader.queries)
	}
	if _, err = reconciler.ReconcileOne(ctx, "ws_poll"); !errors.Is(err, runapp.ErrNoProviderReconciliation) {
		t.Fatal("terminal run remained pollable", err)
	}
	var attempts, observations int
	must(t, owner.QueryRow(ctx, `SELECT count(*) FROM execution.run_attempts WHERE workspace_id='ws_poll' AND run_id='run_poll_a'`).Scan(&attempts))
	must(t, owner.QueryRow(ctx, `SELECT count(*) FROM execution.provider_observations WHERE workspace_id='ws_poll' AND run_id='run_poll_a'`).Scan(&observations))
	if attempts != 1 || observations != 2 {
		t.Fatal("reconciliation retried submission or lost observations", attempts, observations)
	}

	unavailable := accept("run_poll_b", "deploy_poll_b", "provider/request-poll-b")
	reader.err = errors.New("raw-provider-status-detail")
	beforeVersion := unavailable.Run.Version
	if _, err = reconciler.ReconcileOne(ctx, "ws_poll"); !errors.Is(err, runapp.ErrProviderStatusUnavailable) {
		t.Fatal("provider status failure did not fail closed", err)
	}
	var runState, jobState string
	var runVersion int64
	must(t, owner.QueryRow(ctx, `SELECT r.state,r.version,j.state FROM execution.runs r JOIN execution.jobs j ON (j.workspace_id,j.run_id)=(r.workspace_id,r.id) WHERE r.workspace_id='ws_poll' AND r.id='run_poll_b'`).Scan(&runState, &runVersion, &jobState))
	must(t, owner.QueryRow(ctx, `SELECT count(*) FROM execution.provider_observations WHERE workspace_id='ws_poll' AND run_id='run_poll_b'`).Scan(&observations))
	must(t, owner.QueryRow(ctx, `SELECT count(*) FROM execution.run_attempts WHERE workspace_id='ws_poll' AND run_id='run_poll_b'`).Scan(&attempts))
	if runState != "running" || jobState != "provider_waiting" || runVersion != int64(beforeVersion) || observations != 0 || attempts != 1 {
		t.Fatal("provider status failure mutated execution or retried submission", runState, runVersion, jobState, observations, attempts)
	}

	t.Log("real PostgreSQL reconciliation verified: provider identity routing, pending-to-success convergence, terminal candidate removal, no resubmission, and status-unavailable fail-closed behavior")
}
