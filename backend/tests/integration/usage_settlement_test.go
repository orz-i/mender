//go:build integration

package integration_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/bootstrap"
	runpg "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/postgres"
	runapp "github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application/ports"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	settlementapp "github.com/orz-i/mender/backend/internal/processes/settlement/application"
	"github.com/orz-i/mender/backend/migrations"
)

func exerciseUsageSettlement(t *testing.T, ctx context.Context, owner, runtime *pgxpool.Pool, runtimeDSN string) {
	t.Helper()
	worker, _ := openTemporaryRole(t, ctx, owner, runtimeDSN, "mender_settle_worker_", migrations.GrantWorker)
	reconciler, _ := openTemporaryRole(t, ctx, owner, runtimeDSN, "mender_settle_reconciler_", migrations.GrantReconciler)
	cancellation, _ := openTemporaryRole(t, ctx, owner, runtimeDSN, "mender_settle_cancel_", migrations.GrantCancellation)
	settlementPool, settlementDSN := openTemporaryRoleWithDSN(t, ctx, owner, runtimeDSN, "mender_settlement_", migrations.GrantSettlement)
	must(t, database.WorkerRole(ctx, worker))
	must(t, database.ReconcilerRole(ctx, reconciler))
	must(t, database.CancellationRole(ctx, cancellation))
	must(t, database.SettlementRole(ctx, settlementPool))
	if database.SettlementRole(ctx, runtime) == nil || database.SettlementRole(ctx, owner) == nil {
		t.Fatal("runtime/owner role accepted as usage settlement principal")
	}
	if unsafeRuntime, closeUnsafe, err := bootstrap.BuildUsageSettlementRuntime(ctx, bootstrap.UsageSettlementRuntimeConfig{DatabaseURL: runtimeDSN}); err == nil || unsafeRuntime != nil || closeUnsafe != nil {
		t.Fatal("non-settlement database role built settlement runtime")
	}

	base := time.Now().UTC().Truncate(time.Microsecond).Add(-2 * time.Minute)
	commerceTx, err := owner.Begin(ctx)
	must(t, err)
	defer func() { _ = commerceTx.Rollback(context.Background()) }()
	_, err = commerceTx.Exec(ctx, `INSERT INTO commerce.price_versions(id,tool_version_id,currency,reserve_micro,charge_micro,billing_policy,starts_at,ends_at,active)
VALUES('price_settle','tool_settle','USD',100,70,'fixed_success_only',$1,$2,true)`, base.Add(-time.Hour), base.Add(time.Hour))
	must(t, err)
	_, err = commerceTx.Exec(ctx, `INSERT INTO commerce.budget_periods(workspace_id,budget_id,period_id,currency,starts_at,ends_at,limit_micro,consumed_micro,reserved_micro,revision)
VALUES('ws_settle','budget_settle','period_settle','USD',$1,$2,1000,50,500,1)`, base.Add(-time.Hour), base.Add(time.Hour))
	must(t, err)
	for _, runID := range []string{"run_settle_success", "run_settle_failed", "run_settle_canceled", "run_settle_fault", "run_settle_concurrent"} {
		_, err = commerceTx.Exec(ctx, `INSERT INTO commerce.reservations(workspace_id,id,run_id,budget_id,period_id,currency,amount_micro,state,created_at)
VALUES('ws_settle',$1,$2,'budget_settle','period_settle','USD',100,'held',$3)`, "res_"+runID, runID, base)
		must(t, err)
	}
	must(t, commerceTx.Commit(ctx))

	clock := &integrationWorkerClock{at: base.Add(10 * time.Second)}
	workerControl, err := runapp.NewWorkerControl(runpg.NewWorkers(worker), clock)
	must(t, err)
	results, err := runapp.NewProviderResults(runpg.NewProviderResults(reconciler))
	must(t, err)
	seedSubmitted := func(runID string) runapp.SubmissionRecord {
		_, err := owner.Exec(ctx, `INSERT INTO execution.runs(workspace_id,id,state,version,created_at,updated_at) VALUES('ws_settle',$1,'queued',1,$2,$2)`, runID, base)
		must(t, err)
		_, err = owner.Exec(ctx, `INSERT INTO execution.run_admissions(workspace_id,run_id,subject_id,credential_id,idempotency_key,request_hash,reservation_id,tool_version_id,toolset_version_id,connection_id,price_version_id,deployment_revision,budget_id,period_id,currency,reserved_micro,canonical_arguments,created_at)
VALUES('ws_settle',$1,'sa_settle','key_settle',$1||'_idem',repeat('e',64),$2,'tool_settle','set_settle','conn_settle','price_settle','deploy_settle','budget_settle','period_settle','USD',100,'{"private":"never visible to settlement"}',$3)`, runID, "res_"+runID, base)
		must(t, err)
		_, err = owner.Exec(ctx, `INSERT INTO execution.jobs(workspace_id,run_id,state,blocked_reason,available_at,created_at,updated_at) VALUES('ws_settle',$1,'blocked','executor_not_configured',$2,$2,$2)`, runID, base)
		must(t, err)
		count, err := workerControl.Activate(ctx, "ws_settle", []string{"deploy_settle"}, 1)
		must(t, err)
		if count != 1 {
			t.Fatal("settlement fixture activation mismatch", runID, count)
		}
		lease, err := workerControl.LeaseOne(ctx, "ws_settle", "worker_settle", 30*time.Second)
		must(t, err)
		clock.at = clock.at.Add(time.Second)
		intent, err := workerControl.BeginSubmission(ctx, lease, "submit."+runID+".1")
		must(t, err)
		clock.at = clock.at.Add(time.Second)
		record, err := workerControl.RecordSubmitted(ctx, intent, "provider_settle", "provider/request-"+runID, "task-"+runID)
		must(t, err)
		return record
	}
	observe := func(runID string, state domain.ProviderResultState, resultJSON, errorCode string) time.Time {
		submitted := seedSubmitted(runID)
		at := submitted.Attempt.SubmittedAt.Add(time.Second)
		if state == domain.ProviderCanceled {
			requestClock := &integrationWorkerClock{at: submitted.Attempt.SubmittedAt.Add(500 * time.Millisecond)}
			requests, err := runapp.NewProviderCancelRequests(runpg.NewProviderCancelRequests(cancellation), requestClock)
			must(t, err)
			_, err = requests.Request(ctx, ports.Caller{WorkspaceID: "ws_settle", SubjectID: "sa_settle", CredentialID: "key_settle"}, domain.RunID(runID), "settlement cancellation fixture")
			must(t, err)
		}
		observation := domain.ProviderObservation{WorkspaceID: "ws_settle", RunID: domain.RunID(runID), ObservationID: "obs.settle." + runID, AttemptNo: 1, ProviderID: "provider_settle", ProviderRequestID: "provider/request-" + runID, ExternalTaskID: "task-" + runID, State: state, ResultJSON: resultJSON, ErrorCode: errorCode, ObservedAt: at}
		record, err := results.Observe(ctx, observation)
		must(t, err)
		if !record.Run.State.IsTerminal() || record.Job.State != domain.JobFinished {
			t.Fatal("provider terminal fact did not converge before settlement", record)
		}
		var jobState, observationID string
		must(t, owner.QueryRow(ctx, `SELECT state,observation_id FROM execution.settlement_jobs WHERE workspace_id='ws_settle' AND run_id=$1`, runID).Scan(&jobState, &observationID))
		if jobState != "pending" || observationID != observation.ObservationID {
			t.Fatal("terminal result did not atomically persist settlement job", runID, jobState, observationID)
		}
		return at
	}

	observe("run_settle_success", domain.ProviderSucceeded, `{"ok":true}`, "")
	observe("run_settle_failed", domain.ProviderFailed, "", "provider_failed")
	observe("run_settle_canceled", domain.ProviderCanceled, "", "")
	observe("run_settle_fault", domain.ProviderSucceeded, `{"ok":true}`, "")
	observe("run_settle_concurrent", domain.ProviderSucceeded, `{"ok":true}`, "")

	settlementRuntime, closeSettlement, err := bootstrap.BuildUsageSettlementRuntime(ctx, bootstrap.UsageSettlementRuntimeConfig{DatabaseURL: settlementDSN})
	must(t, err)
	defer closeSettlement()
	assertBudget := func(consumed, reserved int64) {
		var gotConsumed, gotReserved int64
		must(t, owner.QueryRow(ctx, `SELECT consumed_micro,reserved_micro FROM commerce.budget_periods WHERE workspace_id='ws_settle' AND budget_id='budget_settle' AND period_id='period_settle'`).Scan(&gotConsumed, &gotReserved))
		if gotConsumed != consumed || gotReserved != reserved {
			t.Fatal("unexpected settlement quota", gotConsumed, gotReserved, "want", consumed, reserved)
		}
	}
	assertSettlement := func(runID string, charge int64, outcome string) {
		var reservationState string
		var charged int64
		must(t, owner.QueryRow(ctx, `SELECT state,charged_micro FROM commerce.reservations WHERE workspace_id='ws_settle' AND run_id=$1`, runID).Scan(&reservationState, &charged))
		if reservationState != "settled" || charged != charge {
			t.Fatal("reservation not settled", runID, reservationState, charged)
		}
		var receiptCharge int64
		var receiptOutcome, jobState string
		must(t, owner.QueryRow(ctx, `SELECT charged_micro,outcome FROM commerce.usage_settlements WHERE workspace_id='ws_settle' AND run_id=$1`, runID).Scan(&receiptCharge, &receiptOutcome))
		must(t, owner.QueryRow(ctx, `SELECT state FROM execution.settlement_jobs WHERE workspace_id='ws_settle' AND run_id=$1`, runID).Scan(&jobState))
		if receiptCharge != charge || receiptOutcome != outcome || jobState != "finished" {
			t.Fatal("settlement receipt/job mismatch", runID, receiptCharge, receiptOutcome, jobState)
		}
	}

	for _, tc := range []struct {
		runID                      string
		charge, consumed, reserved int64
		outcome                    string
	}{
		{"run_settle_success", 70, 120, 400, "succeeded"},
		{"run_settle_failed", 0, 120, 300, "failed"},
		{"run_settle_canceled", 0, 120, 200, "canceled"},
	} {
		receipt, err := settlementRuntime.SettleOne(ctx, "ws_settle")
		must(t, err)
		if receipt.ChargedMicro != tc.charge {
			t.Fatal("unexpected charged quota", tc.runID, receipt.ChargedMicro)
		}
		assertSettlement(tc.runID, tc.charge, tc.outcome)
		assertBudget(tc.consumed, tc.reserved)
	}

	// Force the receipt insert to fail after the budget/reservation updates. The
	// UoW must rollback every write and leave the durable settlement job pending.
	_, err = owner.Exec(ctx, `ALTER TABLE commerce.usage_settlements ADD CONSTRAINT test_reject_fault_settlement CHECK (run_id <> 'run_settle_fault')`)
	must(t, err)
	if _, err = settlementRuntime.SettleOne(ctx, "ws_settle"); !errors.Is(err, settlementapp.ErrUnavailable) {
		t.Fatal("injected settlement failure did not fail closed", err)
	}
	assertBudget(120, 200)
	var state string
	must(t, owner.QueryRow(ctx, `SELECT state FROM commerce.reservations WHERE workspace_id='ws_settle' AND run_id='run_settle_fault'`).Scan(&state))
	if state != "held" {
		t.Fatal("failed settlement mutated reservation", state)
	}
	must(t, owner.QueryRow(ctx, `SELECT state FROM execution.settlement_jobs WHERE workspace_id='ws_settle' AND run_id='run_settle_fault'`).Scan(&state))
	if state != "pending" {
		t.Fatal("failed settlement marked job finished", state)
	}
	_, err = owner.Exec(ctx, `ALTER TABLE commerce.usage_settlements DROP CONSTRAINT test_reject_fault_settlement`)
	must(t, err)
	receipt, err := settlementRuntime.SettleOne(ctx, "ws_settle")
	must(t, err)
	if receipt.ChargedMicro != 70 {
		t.Fatal(receipt)
	}
	assertSettlement("run_settle_fault", 70, "succeeded")
	assertBudget(190, 100)

	// Two concurrent settlement cycles race for the same remaining job. SKIP
	// LOCKED guarantees one settles and the other observes no work.
	var group sync.WaitGroup
	resultsErr := make(chan error, 2)
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			_, e := settlementRuntime.SettleOne(ctx, "ws_settle")
			resultsErr <- e
		}()
	}
	group.Wait()
	close(resultsErr)
	var settled, noWork int
	for e := range resultsErr {
		switch {
		case e == nil:
			settled++
		case errors.Is(e, settlementapp.ErrNoWork):
			noWork++
		default:
			t.Fatal("unexpected concurrent settlement result", e)
		}
	}
	if settled != 1 || noWork != 1 {
		t.Fatal("settlement claim was not single-owner", settled, noWork)
	}
	assertSettlement("run_settle_concurrent", 70, "succeeded")
	assertBudget(260, 0)

	// Simulate a recovered delivery marker after the immutable Commerce receipt
	// already exists. Reprocessing must only finish the job, never charge twice.
	_, err = owner.Exec(ctx, `UPDATE execution.settlement_jobs SET state='pending',finished_at=NULL WHERE workspace_id='ws_settle' AND run_id='run_settle_success'`)
	must(t, err)
	receipt, err = settlementRuntime.SettleOne(ctx, "ws_settle")
	must(t, err)
	if receipt.ChargedMicro != 70 {
		t.Fatal(receipt)
	}
	assertBudget(260, 0)
	var settlementCount int
	must(t, owner.QueryRow(ctx, `SELECT count(*) FROM commerce.usage_settlements WHERE workspace_id='ws_settle'`).Scan(&settlementCount))
	if settlementCount != 5 {
		t.Fatal("settlement replay duplicated immutable receipt", settlementCount)
	}

	t.Run("settlement role is RLS-scoped and cannot inspect execution payloads", func(t *testing.T) {
		var count int
		must(t, settlementPool.QueryRow(ctx, `SELECT count(*) FROM execution.settlement_jobs`).Scan(&count))
		if count != 0 {
			t.Fatal("settlement workspace context leaked across pooled connection", count)
		}
		for _, sql := range []string{
			`SELECT canonical_arguments FROM execution.run_admissions`,
			`SELECT * FROM connections.connections`,
			`SELECT * FROM supply.deployments`,
			`SELECT * FROM identity.api_keys`,
			`UPDATE execution.provider_observations SET state='failed'`,
			`INSERT INTO execution.settlement_jobs(workspace_id,run_id,observation_id,created_at) VALUES('ws_settle','forged','forged',now())`,
			`UPDATE commerce.reservations SET amount_micro=0`,
		} {
			if _, e := settlementPool.Exec(ctx, sql); e == nil {
				t.Fatal("settlement role accepted forbidden operation", sql)
			}
		}
	})

	t.Log("real PostgreSQL usage settlement verified: terminal job durability, success-only charge, zero-charge fail/cancel, rollback/recovery, replay idempotency, concurrent claim, RLS and least-privilege role")
}
