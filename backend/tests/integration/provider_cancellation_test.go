//go:build integration

package integration_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	runpg "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/postgres"
	supplycancel "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/supplycancel"
	supplystatus "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/supplystatus"
	runapp "github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application/ports"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
	supply "github.com/orz-i/mender/backend/internal/contexts/supply/public"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	"github.com/orz-i/mender/backend/migrations"
)

type cancelAssertProvider struct {
	owner *pgxpool.Pool
	runID string
	result supply.CancelResult
	err error
	calls int
	stateSeen string
}

func (c *cancelAssertProvider) Cancel(ctx context.Context, _ supply.CancelQuery) (supply.CancelResult, error) {
	c.calls++
	if c.owner != nil {
		if e := c.owner.QueryRow(ctx, `SELECT state FROM execution.provider_cancel_intents WHERE workspace_id='ws_provider_cancel' AND run_id=$1`, c.runID).Scan(&c.stateSeen); e != nil {
			return supply.CancelResult{}, e
		}
	}
	if c.err != nil {
		return supply.CancelResult{}, c.err
	}
	return c.result, nil
}

func exerciseProviderCancellation(t *testing.T, ctx context.Context, owner, runtime *pgxpool.Pool, runtimeDSN string) {
	t.Helper()
	worker, _ := openTemporaryRole(t, ctx, owner, runtimeDSN, "mender_cancel_worker_", migrations.GrantWorker)
	cancelPool, _ := openTemporaryRole(t, ctx, owner, runtimeDSN, "mender_provider_cancel_", migrations.GrantCancellation)
	reconcilerPool, _ := openTemporaryRole(t, ctx, owner, runtimeDSN, "mender_cancel_reconciler_", migrations.GrantReconciler)
	must(t, database.WorkerRole(ctx, worker))
	must(t, database.CancellationRole(ctx, cancelPool))
	must(t, database.ReconcilerRole(ctx, reconcilerPool))
	if database.CancellationRole(ctx, runtime) == nil || database.ReconcilerRole(ctx, runtime) == nil {
		t.Fatal("runtime role accepted as provider cancellation principal")
	}

	base := time.Now().UTC().Truncate(time.Microsecond).Add(-2 * time.Minute)
	clock := &integrationWorkerClock{at: base.Add(5 * time.Second)}
	workerControl, err := runapp.NewWorkerControl(runpg.NewWorkers(worker), clock)
	must(t, err)
	seedAccepted := func(runID, deployment, requestID string) runapp.SubmissionRecord {
		_, err := owner.Exec(ctx, `INSERT INTO execution.runs(workspace_id,id,state,version,created_at,updated_at) VALUES('ws_provider_cancel',$1,'queued',1,$2,$2)`, runID, base)
		must(t, err)
		_, err = owner.Exec(ctx, `INSERT INTO execution.run_admissions(workspace_id,run_id,subject_id,credential_id,idempotency_key,request_hash,reservation_id,tool_version_id,toolset_version_id,connection_id,price_version_id,deployment_revision,budget_id,period_id,currency,reserved_micro,canonical_arguments,created_at)
VALUES('ws_provider_cancel',$1,'sa_provider_cancel','key_provider_cancel',$1||'_idem',repeat('c',64),'res_'||$1,'tool_cancel','set_cancel','conn_cancel','price_cancel',$2,'budget_cancel','period_cancel','USD',0,'{}',$3)`, runID, deployment, base)
		must(t, err)
		_, err = owner.Exec(ctx, `INSERT INTO execution.jobs(workspace_id,run_id,state,blocked_reason,available_at,created_at,updated_at) VALUES('ws_provider_cancel',$1,'blocked','executor_not_configured',$2,$2,$2)`, runID, base)
		must(t, err)
		count, err := workerControl.Activate(ctx, "ws_provider_cancel", []string{deployment}, 1)
		must(t, err)
		if count != 1 {
			t.Fatal("provider cancel fixture activation mismatch", runID, count)
		}
		lease, err := workerControl.LeaseOne(ctx, "ws_provider_cancel", "worker_provider_cancel", 30*time.Second)
		must(t, err)
		clock.at = clock.at.Add(time.Second)
		intent, err := workerControl.BeginSubmission(ctx, lease, "submit."+runID+".1")
		must(t, err)
		clock.at = clock.at.Add(time.Second)
		record, err := workerControl.RecordSubmitted(ctx, intent, "provider_cancel", requestID, "task-"+runID)
		must(t, err)
		return record
	}

	requestClock := &integrationWorkerClock{at: base.Add(30 * time.Second)}
	requests, err := runapp.NewProviderCancelRequests(runpg.NewProviderCancelRequests(cancelPool), requestClock)
	must(t, err)
	caller := ports.Caller{WorkspaceID: "ws_provider_cancel", SubjectID: "sa_provider_cancel", CredentialID: "key_provider_cancel"}
	control := runpg.NewProviderCancellations(reconcilerPool)
	results, err := runapp.NewProviderResults(runpg.NewProviderResults(reconcilerPool))
	must(t, err)

	t.Run("cancel acknowledgement wins and late success is rejected", func(t *testing.T) {
		submitted := seedAccepted("run_cancel_ack", "deploy_cancel_ack", "provider/request-cancel-ack")
		requestClock.at = submitted.Attempt.SubmittedAt.Add(2 * time.Second)
		requested, err := requests.Request(ctx, caller, "run_cancel_ack", "user requested stop")
		must(t, err)
		if requested.Run.State != domain.CancelRequested || requested.Target.ProviderID != "provider_cancel" || requested.Replay {
			t.Fatal(requested)
		}
		replay, err := requests.Request(ctx, caller, "run_cancel_ack", "user requested stop")
		must(t, err)
		if !replay.Replay || replay.Run.Version != requested.Run.Version || replay.Target.CancelKey != requested.Target.CancelKey {
			t.Fatal("provider cancel request replay mutated state", replay, requested)
		}

		ackAt := requestClock.at.Add(2 * time.Second)
		provider := &cancelAssertProvider{owner: owner, runID: "run_cancel_ack", result: supply.CancelResult{Disposition: supply.CancelAcknowledged, ObservationID: "obs.cancel.ack", ObservedAt: ackAt}}
		source, err := supplycancel.New(map[string]supply.ProviderCanceler{"provider_cancel": provider})
		must(t, err)
		dispatchClock := &integrationWorkerClock{at: requestClock.at.Add(time.Second)}
		dispatcher, err := runapp.NewProviderCancelDispatcher(control, source, dispatchClock)
		must(t, err)
		record, err := dispatcher.CancelOne(ctx, "ws_provider_cancel")
		must(t, err)
		if provider.calls != 1 || provider.stateSeen != "sending" || record.Run.State != domain.Canceled || record.Job.State != domain.JobFinished || record.Observation.State != domain.ProviderCanceled {
			t.Fatal("provider cancel acknowledgement did not atomically converge", provider.calls, provider.stateSeen, record)
		}
		var cancelState, outcomeID string
		must(t, owner.QueryRow(ctx, `SELECT state,outcome_observation_id FROM execution.provider_cancel_intents WHERE workspace_id='ws_provider_cancel' AND run_id='run_cancel_ack'`).Scan(&cancelState, &outcomeID))
		if cancelState != "fulfilled" || outcomeID != "obs.cancel.ack" {
			t.Fatal(cancelState, outcomeID)
		}
		late := domain.ProviderObservation{WorkspaceID: "ws_provider_cancel", RunID: "run_cancel_ack", ObservationID: "obs.cancel.late-success", AttemptNo: 1, ProviderID: "provider_cancel", ProviderRequestID: "provider/request-cancel-ack", ExternalTaskID: "task-run_cancel_ack", State: domain.ProviderSucceeded, ResultJSON: `{"late":true}`, ObservedAt: ackAt.Add(time.Second)}
		if _, err = results.Observe(ctx, late); !errors.Is(err, runapp.ErrProviderAlreadyTerminal) {
			t.Fatal("late success overwrote acknowledged cancellation", err)
		}
	})

	t.Run("provider success wins before cancellation acknowledgement", func(t *testing.T) {
		submitted := seedAccepted("run_cancel_success", "deploy_cancel_success", "provider/request-cancel-success")
		requestClock.at = submitted.Attempt.SubmittedAt.Add(2 * time.Second)
		_, err := requests.Request(ctx, caller, "run_cancel_success", "stop if still running")
		must(t, err)
		dispatchClock := requestClock.at.Add(time.Second)
		target, found, err := control.ClaimProviderCancel(ctx, "ws_provider_cancel", dispatchClock)
		must(t, err)
		if !found || target.SendingAt.IsZero() {
			t.Fatal(target, found)
		}
		successAt := requestClock.at.Add(500 * time.Millisecond)
		success := domain.ProviderObservation{WorkspaceID: "ws_provider_cancel", RunID: "run_cancel_success", ObservationID: "obs.cancel.success-wins", AttemptNo: 1, ProviderID: "provider_cancel", ProviderRequestID: "provider/request-cancel-success", ExternalTaskID: "task-run_cancel_success", State: domain.ProviderSucceeded, ResultJSON: `{"done":true}`, ObservedAt: successAt}
		record, err := results.Observe(ctx, success)
		must(t, err)
		if record.Run.State != domain.Succeeded || record.Job.State != domain.JobFinished {
			t.Fatal(record)
		}
		var cancelState string
		must(t, owner.QueryRow(ctx, `SELECT state FROM execution.provider_cancel_intents WHERE workspace_id='ws_provider_cancel' AND run_id='run_cancel_success'`).Scan(&cancelState))
		if cancelState != "superseded" {
			t.Fatal("provider success did not supersede cancel intent", cancelState)
		}
		if _, err = control.RecordProviderCancelAcknowledged(ctx, target, "obs.cancel.too-late", dispatchClock.Add(time.Second)); !errors.Is(err, runapp.ErrProviderAlreadyTerminal) {
			t.Fatal("late cancellation acknowledgement overwrote success", err)
		}
	})

	t.Run("unknown cancellation is not reissued and status polling resolves it", func(t *testing.T) {
		submitted := seedAccepted("run_cancel_unknown", "deploy_cancel_unknown", "provider/request-cancel-unknown")
		requestClock.at = submitted.Attempt.SubmittedAt.Add(2 * time.Second)
		_, err := requests.Request(ctx, caller, "run_cancel_unknown", "stop")
		must(t, err)
		provider := &cancelAssertProvider{owner: owner, runID: "run_cancel_unknown", err: errors.New("raw provider cancel failure")}
		source, err := supplycancel.New(map[string]supply.ProviderCanceler{"provider_cancel": provider})
		must(t, err)
		dispatchClock := &integrationWorkerClock{at: requestClock.at.Add(time.Second)}
		dispatcher, err := runapp.NewProviderCancelDispatcher(control, source, dispatchClock)
		must(t, err)
		if _, err = dispatcher.CancelOne(ctx, "ws_provider_cancel"); !errors.Is(err, runapp.ErrProviderCancelOutcomeUnknown) {
			t.Fatal("unknown provider cancel was not persisted", err)
		}
		if provider.calls != 1 || provider.stateSeen != "sending" {
			t.Fatal(provider.calls, provider.stateSeen)
		}
		if _, err = dispatcher.CancelOne(ctx, "ws_provider_cancel"); !errors.Is(err, runapp.ErrNoProviderCancellation) || provider.calls != 1 {
			t.Fatal("unknown provider cancel was blindly reissued", err, provider.calls)
		}
		var cancelState, unknownReason string
		must(t, owner.QueryRow(ctx, `SELECT state,unknown_reason FROM execution.provider_cancel_intents WHERE workspace_id='ws_provider_cancel' AND run_id='run_cancel_unknown'`).Scan(&cancelState, &unknownReason))
		if cancelState != "unknown" || unknownReason != "provider cancellation outcome unknown" {
			t.Fatal(cancelState, unknownReason)
		}

		statusAt := dispatchClock.at.Add(time.Second)
		statusReader := &integrationStatusReader{responses: []supply.StatusObservation{{ObservationID: "obs.cancel.status-confirmed", State: supply.StatusCanceled, ObservedAt: statusAt}}}
		statusSource, err := supplystatus.New(map[string]supply.ProviderStatusReader{"provider_cancel": statusReader})
		must(t, err)
		reconciler, err := runapp.NewProviderReconciler(runpg.NewProviderReconciliation(reconcilerPool), statusSource, results)
		must(t, err)
		record, err := reconciler.ReconcileOne(ctx, "ws_provider_cancel")
		must(t, err)
		if record.Run.State != domain.Canceled || record.Job.State != domain.JobFinished || record.Observation.State != domain.ProviderCanceled {
			t.Fatal(record)
		}
		must(t, owner.QueryRow(ctx, `SELECT state FROM execution.provider_cancel_intents WHERE workspace_id='ws_provider_cancel' AND run_id='run_cancel_unknown'`).Scan(&cancelState))
		if cancelState != "fulfilled" {
			t.Fatal("status polling did not fulfill unknown cancel intent", cancelState)
		}
		var attempts int
		must(t, owner.QueryRow(ctx, `SELECT count(*) FROM execution.run_attempts WHERE workspace_id='ws_provider_cancel' AND run_id='run_cancel_unknown'`).Scan(&attempts))
		if attempts != 1 {
			t.Fatal("provider cancellation created/retried execution attempt", attempts)
		}
	})

	t.Run("role boundaries reject forged provider cancellation outcomes", func(t *testing.T) {
		for _, sql := range []string{
			`UPDATE execution.provider_cancel_intents SET state='fulfilled',resolved_at=now(),outcome_observation_id='forged'`,
			`DELETE FROM execution.provider_cancel_intents`,
		} {
			if _, err := cancelPool.Exec(ctx, sql); err == nil {
				t.Fatal("cancellation requester forged provider outcome", sql)
			}
		}
		if _, err := reconcilerPool.Exec(ctx, `INSERT INTO execution.provider_cancel_intents(workspace_id,run_id,attempt_no,cancel_key,provider_id,provider_request_id,requested_by_subject,requested_by_credential,state,requested_at) VALUES('ws_provider_cancel','run_cancel_ack',1,'forged.cancel','provider_cancel','forged','x','x','requested',now())`); err == nil {
			t.Fatal("reconciler forged user cancellation intent")
		}
		for _, pool := range []*pgxpool.Pool{runtime, worker} {
			if _, err := pool.Exec(ctx, `SELECT * FROM execution.provider_cancel_intents`); err == nil {
				t.Fatal("unrelated runtime can access provider cancel intents")
			}
		}
	})

	t.Log("real PostgreSQL provider cancellation verified: durable intent, sending-before-call, no blind reissue, ack-vs-success convergence, status reconciliation, and role separation")
}
