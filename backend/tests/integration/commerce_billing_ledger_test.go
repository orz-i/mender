//go:build integration

package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	"github.com/orz-i/mender/backend/migrations"
)

func seedSettledBillingRun(t *testing.T, ctx context.Context, owner *pgxpool.Pool, workspace, run, suffix string, reserved, charged int64, base time.Time) {
	t.Helper()
	price := "price_" + suffix
	budget := "budget_" + suffix
	period := "period_" + suffix
	reservation := "reservation_" + suffix
	_, err := owner.Exec(ctx, `INSERT INTO commerce.price_versions(id,tool_version_id,currency,reserve_micro,starts_at,ends_at,active,charge_micro,billing_policy)
	 VALUES($1,$2,'USD',$3,$4,$5,true,$3,'fixed_success_only')`, price, "tool_"+suffix, reserved, base.Add(-time.Hour), base.Add(24*time.Hour))
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO commerce.budget_periods(workspace_id,budget_id,period_id,currency,starts_at,ends_at,limit_micro,consumed_micro,reserved_micro)
	 VALUES($1,$2,$3,'USD',$4,$5,1000000,$6,0)`, workspace, budget, period, base.Add(-time.Hour), base.Add(24*time.Hour), charged)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO execution.runs(workspace_id,id,state,version,created_at,updated_at) VALUES($1,$2,'succeeded',2,$3,$4)`, workspace, run, base, base.Add(time.Minute))
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO execution.run_admissions(
	 workspace_id,run_id,subject_id,credential_id,idempotency_key,request_hash,reservation_id,tool_version_id,toolset_version_id,connection_id,price_version_id,deployment_revision,budget_id,period_id,currency,reserved_micro,canonical_arguments,created_at)
	 VALUES($1,$2,'sa_billing','key_billing',$2||'_idem',repeat('a',64),$3,$4,'set_billing','conn_billing',$5,'deploy_billing',$6,$7,'USD',$8,'{}',$9)`,
		workspace, run, reservation, "tool_"+suffix, price, budget, period, reserved, base)
	must(t, err)

	tx, err := owner.Begin(ctx)
	must(t, err)
	defer func() { _ = tx.Rollback(context.Background()) }()
	settledAt := base.Add(2 * time.Minute)
	_, err = tx.Exec(ctx, `INSERT INTO commerce.reservations(workspace_id,id,run_id,budget_id,period_id,currency,amount_micro,state,created_at,released_at,charged_micro,settled_at)
	 VALUES($1,$2,$3,$4,$5,'USD',$6,'settled',$7,NULL,$8,$9)`, workspace, reservation, run, budget, period, reserved, base, charged, settledAt)
	must(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO commerce.usage_settlements(workspace_id,run_id,reservation_id,price_version_id,budget_id,period_id,currency,reserved_micro,charged_micro,outcome,observed_at,settled_at)
	 VALUES($1,$2,$3,$4,$5,$6,'USD',$7,$8,'succeeded',$9,$10)`, workspace, run, reservation, price, budget, period, reserved, charged, base.Add(time.Minute), settledAt)
	must(t, err)
	must(t, tx.Commit(ctx))
}

func seedPendingBillingRun(t *testing.T, ctx context.Context, owner *pgxpool.Pool, workspace, run, suffix string, amount int64, base time.Time) {
	t.Helper()
	price := "price_" + suffix
	budget := "budget_" + suffix
	period := "period_" + suffix
	reservation := "reservation_" + suffix
	unknownAt := base.Add(time.Minute)
	providerRequestID := "provider/" + suffix
	externalTaskID := "task-" + suffix
	_, err := owner.Exec(ctx, `INSERT INTO commerce.price_versions(id,tool_version_id,currency,reserve_micro,starts_at,ends_at,active,charge_micro,billing_policy)
	 VALUES($1,$2,'USD',$3,$4,$5,true,$3,'fixed_success_only')`, price, "tool_"+suffix, amount, base.Add(-time.Hour), base.Add(24*time.Hour))
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO execution.runs(workspace_id,id,state,version,created_at,updated_at) VALUES($1,$2,'reconciling',2,$3,$4)`, workspace, run, base, unknownAt)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO execution.run_admissions(
	 workspace_id,run_id,subject_id,credential_id,idempotency_key,request_hash,reservation_id,tool_version_id,toolset_version_id,connection_id,price_version_id,deployment_revision,budget_id,period_id,currency,reserved_micro,canonical_arguments,created_at)
	 VALUES($1,$2,'sa_billing','key_billing',$2||'_idem',repeat('b',64),$3,$4,'set_billing','conn_billing',$5,'deploy_billing',$6,$7,'USD',$8,'{}',$9)`,
		workspace, run, reservation, "tool_"+suffix, price, budget, period, amount, base)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO execution.jobs(
	 workspace_id,run_id,state,blocked_reason,available_at,created_at,stopped_at,priority,lease_owner,lease_until,lease_generation,attempt_count,max_attempts,updated_at)
	 VALUES($1,$2,'reconciling','submission_outcome_unknown',$3,$3,NULL,0,NULL,NULL,1,1,3,$4)`, workspace, run, base, unknownAt)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO execution.run_attempts(
	 workspace_id,run_id,attempt_no,lease_generation,lease_owner,state,leased_at,lease_until,finished_at,
	 submission_key,submission_intent_at,provider_id,provider_request_id,external_task_id,submitted_at,unknown_at,unknown_reason)
	 VALUES($1,$2,1,1,'worker_billing','unknown',$3,$4,$5,$6,$7,'provider_billing',$8,$9,$10,$5,'submission outcome unknown')`,
		workspace, run, base, base.Add(30*time.Minute), unknownAt, "submit."+suffix+".1", base.Add(20*time.Second), providerRequestID, externalTaskID, base.Add(30*time.Second))
	must(t, err)
	tx, err := owner.Begin(ctx)
	must(t, err)
	defer func() { _ = tx.Rollback(context.Background()) }()
	_, err = tx.Exec(ctx, `INSERT INTO commerce.budget_periods(workspace_id,budget_id,period_id,currency,starts_at,ends_at,limit_micro,consumed_micro,reserved_micro)
	 VALUES($1,$2,$3,'USD',$4,$5,1000000,0,$6)`, workspace, budget, period, base.Add(-time.Hour), base.Add(24*time.Hour), amount)
	must(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO commerce.reservations(workspace_id,id,run_id,budget_id,period_id,currency,amount_micro,state,created_at)
	 VALUES($1,$2,$3,$4,$5,'USD',$6,'held',$7)`, workspace, reservation, run, budget, period, amount, base)
	must(t, err)
	must(t, tx.Commit(ctx))
}

func settleLateBillingRun(t *testing.T, ctx context.Context, owner *pgxpool.Pool, workspace, run, suffix string, amount int64, base time.Time) {
	t.Helper()
	price := "price_" + suffix
	budget := "budget_" + suffix
	period := "period_" + suffix
	reservation := "reservation_" + suffix
	providerRequestID := "provider/" + suffix
	externalTaskID := "task-" + suffix
	terminalAt := base.Add(7 * time.Minute)
	settledAt := base.Add(8 * time.Minute)
	// Late terminal provider evidence converges the previously unknown submission
	// first. The existing deferred Execution constraints require observation, Run
	// and Job terminal facts to agree atomically before Commerce may settle it.
	tx, err := owner.Begin(ctx)
	must(t, err)
	defer func() { _ = tx.Rollback(context.Background()) }()
	_, err = tx.Exec(ctx, `INSERT INTO execution.provider_observations(
	 workspace_id,run_id,observation_id,attempt_no,provider_id,provider_request_id,external_task_id,state,result_json,error_code,observed_at)
	 VALUES($1,$2,$3,1,'provider_billing',$4,$5,'succeeded','{"late":true}'::jsonb,NULL,$6)`, workspace, run, "obs_"+suffix+"_terminal", providerRequestID, externalTaskID, terminalAt)
	must(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO execution.artifacts(
	 workspace_id,run_id,id,kind,media_type,source_observation_id,content_json,created_at)
	 VALUES($1,$2,'art_'||$2,'provider_result','application/json',$3,'{"late":true}'::jsonb,$4)`, workspace, run, "obs_"+suffix+"_terminal", terminalAt)
	must(t, err)
	_, err = tx.Exec(ctx, `UPDATE execution.runs SET state='succeeded',version=3,updated_at=$3 WHERE workspace_id=$1 AND id=$2`, workspace, run, terminalAt)
	must(t, err)
	_, err = tx.Exec(ctx, `UPDATE execution.jobs SET state='finished',blocked_reason=NULL,stopped_at=$3,updated_at=$3 WHERE workspace_id=$1 AND run_id=$2`, workspace, run, terminalAt)
	must(t, err)
	must(t, tx.Commit(ctx))

	tx, err = owner.Begin(ctx)
	must(t, err)
	_, err = tx.Exec(ctx, `UPDATE commerce.budget_periods SET consumed_micro=$4,reserved_micro=0,revision=revision+1 WHERE workspace_id=$1 AND budget_id=$2 AND period_id=$3`, workspace, budget, period, amount)
	must(t, err)
	_, err = tx.Exec(ctx, `UPDATE commerce.reservations SET state='settled',charged_micro=$4,settled_at=$5 WHERE workspace_id=$1 AND id=$2 AND run_id=$3`, workspace, reservation, run, amount, settledAt)
	must(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO commerce.usage_settlements(workspace_id,run_id,reservation_id,price_version_id,budget_id,period_id,currency,reserved_micro,charged_micro,outcome,observed_at,settled_at)
	 VALUES($1,$2,$3,$4,$5,$6,'USD',$7,$7,'succeeded',$8,$9)`, workspace, run, reservation, price, budget, period, amount, terminalAt, settledAt)
	must(t, err)
	must(t, tx.Commit(ctx))
}

func requestCommerceApproval(t *testing.T, ctx context.Context, dangerous *pgxpool.Pool, workspace, approval, actor, action, businessKey, basisKind, basisID, direction string, amount int64, reason string, at time.Time) {
	t.Helper()
	execScoped(t, ctx, dangerous, workspace, `SELECT governance.request_commerce_approval($1,$2,$3,$4,$5,$6,$7,$8,$9,'USD',$10,$11,$12)`,
		workspace, approval, actor, action, businessKey, basisKind, basisID, direction, amount, reason, at, at.Add(15*time.Minute))
}

func billingSummary(t *testing.T, ctx context.Context, billing *pgxpool.Pool, workspace, actor string) (charged, refunded, debit, credit, net, count int64) {
	t.Helper()
	tx := beginWorkspaceTx(t, ctx, billing, workspace)
	var gotWorkspace, currency string
	var latest *time.Time
	must(t, tx.QueryRow(ctx, `SELECT * FROM commerce.billing_summary($1,$2,'USD')`, workspace, actor).Scan(&gotWorkspace, &currency, &charged, &refunded, &debit, &credit, &net, &count, &latest))
	must(t, tx.Commit(ctx))
	if gotWorkspace != workspace || currency != "USD" || count > 0 && latest == nil {
		t.Fatal("billing summary projection drifted", gotWorkspace, currency, count, latest)
	}
	return
}

func billingReconciliation(t *testing.T, ctx context.Context, billing *pgxpool.Pool, workspace, actor string) (usage, ledger, missing, pending, difference int64) {
	t.Helper()
	tx := beginWorkspaceTx(t, ctx, billing, workspace)
	var gotWorkspace, currency string
	must(t, tx.QueryRow(ctx, `SELECT * FROM commerce.billing_reconciliation($1,$2,'USD')`, workspace, actor).Scan(&gotWorkspace, &currency, &usage, &ledger, &missing, &pending, &difference))
	must(t, tx.Commit(ctx))
	if gotWorkspace != workspace || currency != "USD" {
		t.Fatal("billing reconciliation projection drifted", gotWorkspace, currency)
	}
	return
}

func exerciseCommerceBillingLedger(t *testing.T, ctx context.Context, owner *pgxpool.Pool, runtimeDSN string) {
	t.Helper()
	billing, _ := openTemporaryRole(t, ctx, owner, runtimeDSN, "mender_billing_manager_", migrations.GrantBillingManager)
	dangerous, _ := openTemporaryRole(t, ctx, owner, runtimeDSN, "mender_billing_danger_", migrations.GrantDangerousOperationManager)
	must(t, database.BillingManagerRole(ctx, billing))
	must(t, database.DangerousOperationManagerRole(ctx, dangerous))
	if database.BillingManagerRole(ctx, owner) == nil || database.BillingManagerRole(ctx, dangerous) == nil || database.DangerousOperationManagerRole(ctx, billing) == nil {
		t.Fatal("billing or dangerous restricted role accepted a cross-purpose principal")
	}

	workspace := "ws_billing_t20"
	base := time.Date(2026, 9, 16, 5, 0, 0, 0, time.UTC)
	_, err := owner.Exec(ctx, `INSERT INTO identity.workspaces(id,created_at) VALUES($1,$2)`, workspace, base)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO identity.users(id,display_name,created_at) VALUES
	 ('finance_operator','Finance Operator',$1),('finance_reviewer','Finance Reviewer',$1),('finance_auditor','Finance Auditor',$1),('finance_support','Finance Support',$1)`, base)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO identity.platform_staff(user_id,role,created_at) VALUES
	 ('finance_operator','operator',$1),('finance_reviewer','reviewer',$1),('finance_auditor','auditor',$1),('finance_support','support',$1)`, base)
	must(t, err)
	var membershipCount int
	must(t, owner.QueryRow(ctx, `SELECT count(*) FROM identity.workspace_memberships WHERE workspace_id=$1 AND user_id LIKE 'finance_%'`, workspace).Scan(&membershipCount))
	if membershipCount != 0 {
		t.Fatal("Platform finance staff unexpectedly became tenant members", membershipCount)
	}

	for _, statement := range []string{
		`SELECT count(*) FROM commerce.billing_journals`, `SELECT count(*) FROM commerce.billing_entries`, `SELECT count(*) FROM commerce.usage_settlements`,
		`SELECT count(*) FROM commerce.reservations`, `SELECT count(*) FROM commerce.budget_periods`, `SELECT count(*) FROM governance.dangerous_operation_approvals`,
		`SELECT count(*) FROM identity.platform_staff`, `SELECT count(*) FROM execution.runs`, `SELECT count(*) FROM execution.run_admissions`,
	} {
		if _, err = billing.Exec(ctx, statement); err == nil {
			t.Fatal("billing manager obtained direct table authority", statement)
		}
	}
	expectScopedPGCode(t, ctx, billing, workspace, "42501", `SELECT governance.consume_commerce_approval($1,'missing','finance_operator','commerce.refund','refund:x','usage_settlement','run_x','credit',1,'USD',$2)`, workspace, base)
	if _, err = dangerous.Exec(ctx, `SELECT count(*) FROM commerce.billing_journals`); err == nil {
		t.Fatal("dangerous-operation manager obtained direct Commerce table authority")
	}

	seedSettledBillingRun(t, ctx, owner, workspace, "run_billing_settled", "billing_settled", 100, 80, base.Add(time.Minute))
	var chargeJournals, chargeLines int
	var chargeBalance int64
	must(t, owner.QueryRow(ctx, `SELECT count(*) FROM commerce.billing_journals WHERE workspace_id=$1 AND journal_kind='charge' AND basis_id='run_billing_settled'`, workspace).Scan(&chargeJournals))
	must(t, owner.QueryRow(ctx, `SELECT count(*),coalesce(sum(e.delta_micro),0) FROM commerce.billing_entries e JOIN commerce.billing_journals j ON (j.workspace_id,j.id)=(e.workspace_id,e.journal_id) WHERE j.workspace_id=$1 AND j.basis_id='run_billing_settled' AND j.journal_kind='charge'`, workspace).Scan(&chargeLines, &chargeBalance))
	if chargeJournals != 1 || chargeLines != 2 || chargeBalance != 0 {
		t.Fatal("usage settlement did not mirror to exactly one balanced charge journal", chargeJournals, chargeLines, chargeBalance)
	}
	var chargeID string
	must(t, owner.QueryRow(ctx, `SELECT commerce.append_usage_charge_journal($1,'run_billing_settled','USD',80,$2)`, workspace, base.Add(3*time.Minute)).Scan(&chargeID))
	must(t, owner.QueryRow(ctx, `SELECT count(*) FROM commerce.billing_journals WHERE workspace_id=$1 AND journal_kind='charge' AND basis_id='run_billing_settled'`, workspace).Scan(&chargeJournals))
	if chargeJournals != 1 {
		t.Fatal("charge replay duplicated a journal", chargeJournals)
	}
	expectScopedPGCode(t, ctx, owner, workspace, "42501", `UPDATE commerce.billing_journals SET reason='rewritten' WHERE workspace_id=$1 AND id=$2`, workspace, chargeID)
	expectScopedPGCode(t, ctx, owner, workspace, "42501", `UPDATE commerce.usage_settlements SET charged_micro=79 WHERE workspace_id=$1 AND run_id='run_billing_settled'`, workspace)

	// T20: a direct malformed journal cannot commit because the deferred balance
	// constraint requires exactly two same-currency, zero-sum entries.
	tx, err := owner.Begin(ctx)
	must(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO commerce.billing_journals(workspace_id,id,business_key,journal_kind,direction,currency,amount_micro,basis_kind,basis_id,reason,occurred_at)
	 VALUES($1,'journal_unbalanced','usage:unbalanced','charge','','USD',3,'usage_settlement','run_unbalanced','usage settlement',$2)`, workspace, base.Add(4*time.Minute))
	must(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO commerce.billing_entries(workspace_id,journal_id,line_no,account_kind,currency,delta_micro) VALUES($1,'journal_unbalanced',1,'workspace_receivable','USD',3)`, workspace)
	must(t, err)
	err = tx.Commit(ctx)
	if err == nil || pgErrorCode(err) != "23514" {
		t.Fatal("unbalanced journal committed", err)
	}
	_ = tx.Rollback(ctx)

	requestCommerceApproval(t, ctx, dangerous, workspace, "approval_refund_25", "finance_operator", "commerce.refund", "refund:case_25", "usage_settlement", "run_billing_settled", "credit", 25, "partial customer refund", base.Add(5*time.Minute))
	expectScopedPGCode(t, ctx, dangerous, workspace, "42501", `SELECT governance.approve_dangerous_operation($1,'approval_refund_25','finance_operator',$2,'self review')`, workspace, base.Add(6*time.Minute))
	approveDanger(t, ctx, dangerous, workspace, "approval_refund_25", "finance_reviewer", base.Add(6*time.Minute))
	expectScopedPGCode(t, ctx, billing, workspace, "42501", `SELECT * FROM commerce.post_billing_refund($1,'refund:case_25','run_billing_settled',26,'USD','approval_refund_25','finance_operator','partial customer refund',$2)`, workspace, base.Add(7*time.Minute))
	var approvalState string
	must(t, owner.QueryRow(ctx, `SELECT state FROM governance.dangerous_operation_approvals WHERE workspace_id=$1 AND id='approval_refund_25'`, workspace).Scan(&approvalState))
	if approvalState != "approved" {
		t.Fatal("failed exact refund binding consumed approval", approvalState)
	}

	tx = beginWorkspaceTx(t, ctx, billing, workspace)
	var refundID string
	var replay bool
	must(t, tx.QueryRow(ctx, `SELECT * FROM commerce.post_billing_refund($1,'refund:case_25','run_billing_settled',25,'USD','approval_refund_25','finance_operator','partial customer refund',$2)`, workspace, base.Add(7*time.Minute)).Scan(&refundID, &replay))
	must(t, tx.Commit(ctx))
	if refundID == "" || replay {
		t.Fatal("first refund was not appended", refundID, replay)
	}
	must(t, owner.QueryRow(ctx, `SELECT state FROM governance.dangerous_operation_approvals WHERE workspace_id=$1 AND id='approval_refund_25'`, workspace).Scan(&approvalState))
	if approvalState != "consumed" {
		t.Fatal("exact refund did not consume approval", approvalState)
	}
	tx = beginWorkspaceTx(t, ctx, billing, workspace)
	must(t, tx.QueryRow(ctx, `SELECT * FROM commerce.post_billing_refund($1,'refund:case_25','run_billing_settled',25,'USD','approval_refund_25','finance_operator','partial customer refund',$2)`, workspace, base.Add(8*time.Minute)).Scan(&refundID, &replay))
	must(t, tx.Commit(ctx))
	if !replay {
		t.Fatal("exact refund replay was not idempotent")
	}
	expectScopedPGCode(t, ctx, billing, workspace, "23505", `SELECT * FROM commerce.post_billing_refund($1,'refund:case_25','run_billing_settled',24,'USD','approval_refund_25','finance_operator','partial customer refund',$2)`, workspace, base.Add(8*time.Minute))

	requestCommerceApproval(t, ctx, dangerous, workspace, "approval_refund_over", "finance_operator", "commerce.refund", "refund:case_over", "usage_settlement", "run_billing_settled", "credit", 60, "over refund drill", base.Add(9*time.Minute))
	approveDanger(t, ctx, dangerous, workspace, "approval_refund_over", "finance_reviewer", base.Add(10*time.Minute))
	expectScopedPGCode(t, ctx, billing, workspace, "23514", `SELECT * FROM commerce.post_billing_refund($1,'refund:case_over','run_billing_settled',60,'USD','approval_refund_over','finance_operator','over refund drill',$2)`, workspace, base.Add(11*time.Minute))
	must(t, owner.QueryRow(ctx, `SELECT state FROM governance.dangerous_operation_approvals WHERE workspace_id=$1 AND id='approval_refund_over'`, workspace).Scan(&approvalState))
	if approvalState != "approved" {
		t.Fatal("over-refund rollback consumed approval", approvalState)
	}

	requestCommerceApproval(t, ctx, dangerous, workspace, "approval_adjust_7", "finance_operator", "commerce.adjustment", "adjust:case_7", "run", "run_billing_settled", "debit", 7, "late provider cost correction", base.Add(12*time.Minute))
	approveDanger(t, ctx, dangerous, workspace, "approval_adjust_7", "finance_reviewer", base.Add(13*time.Minute))
	execScoped(t, ctx, billing, workspace, `SELECT * FROM commerce.post_billing_adjustment($1,'adjust:case_7','run','run_billing_settled','debit',7,'USD','approval_adjust_7','finance_operator','late provider cost correction',$2)`, workspace, base.Add(14*time.Minute))

	charged, refunded, debit, credit, net, journals := billingSummary(t, ctx, billing, workspace, "finance_auditor")
	if charged != 80 || refunded != 25 || debit != 7 || credit != 0 || net != 62 || journals != 3 {
		t.Fatal("billing summary did not rebuild from immutable entries", charged, refunded, debit, credit, net, journals)
	}
	expectScopedPGCode(t, ctx, billing, workspace, "42501", `SELECT * FROM commerce.billing_summary($1,'finance_support','USD')`, workspace)
	var brokenGroups int
	must(t, owner.QueryRow(ctx, `SELECT count(*) FROM (
	 SELECT j.id FROM commerce.billing_journals j JOIN commerce.billing_entries e ON (e.workspace_id,e.journal_id)=(j.workspace_id,j.id)
	 WHERE j.workspace_id=$1 GROUP BY j.id HAVING count(*)<>2 OR sum(e.delta_micro)<>0 OR count(DISTINCT e.currency)<>1
	) broken`, workspace).Scan(&brokenGroups))
	if brokenGroups != 0 {
		t.Fatal("T20 found a non-zero-sum journal", brokenGroups)
	}

	// T21: unknown Provider outcome keeps the reservation held, creates no usage
	// settlement or charge, and appears only as pending reconciliation.
	seedPendingBillingRun(t, ctx, owner, workspace, "run_billing_pending", "billing_pending", 40, base.Add(15*time.Minute))
	usage, ledger, missing, pending, difference := billingReconciliation(t, ctx, billing, workspace, "finance_reviewer")
	if usage != 80 || ledger != 80 || missing != 0 || pending != 1 || difference != 0 {
		t.Fatal("pending reconciliation projection drifted", usage, ledger, missing, pending, difference)
	}
	var reservationState string
	var releasedAt *time.Time
	must(t, owner.QueryRow(ctx, `SELECT state,released_at FROM commerce.reservations WHERE workspace_id=$1 AND run_id='run_billing_pending'`, workspace).Scan(&reservationState, &releasedAt))
	if reservationState != "held" || releasedAt != nil {
		t.Fatal("unknown Provider outcome auto-released quota", reservationState, releasedAt)
	}
	must(t, owner.QueryRow(ctx, `SELECT count(*) FROM commerce.billing_journals WHERE workspace_id=$1 AND basis_id='run_billing_pending'`, workspace).Scan(&chargeJournals))
	if chargeJournals != 0 {
		t.Fatal("unknown Provider outcome produced billing charge", chargeJournals)
	}

	settleLateBillingRun(t, ctx, owner, workspace, "run_billing_pending", "billing_pending", 40, base.Add(15*time.Minute))
	usage, ledger, missing, pending, difference = billingReconciliation(t, ctx, billing, workspace, "finance_reviewer")
	if usage != 120 || ledger != 120 || missing != 0 || pending != 0 || difference != 0 {
		t.Fatal("late settlement did not converge exactly once", usage, ledger, missing, pending, difference)
	}
	must(t, owner.QueryRow(ctx, `SELECT count(*) FROM commerce.billing_journals WHERE workspace_id=$1 AND journal_kind='charge' AND basis_id='run_billing_pending'`, workspace).Scan(&chargeJournals))
	if chargeJournals != 1 {
		t.Fatal("late settlement charge did not converge exactly once", chargeJournals)
	}
	charged, refunded, debit, credit, net, journals = billingSummary(t, ctx, billing, workspace, "finance_auditor")
	if charged != 120 || refunded != 25 || debit != 7 || credit != 0 || net != 102 || journals != 4 {
		t.Fatal("late settlement changed supplemental history instead of appending charge", charged, refunded, debit, credit, net, journals)
	}
	must(t, owner.QueryRow(ctx, `SELECT count(*) FROM identity.workspace_memberships WHERE workspace_id=$1 AND user_id LIKE 'finance_%'`, workspace).Scan(&membershipCount))
	if membershipCount != 0 {
		t.Fatal("billing lifecycle mutated tenant membership", membershipCount)
	}

	var requested, approved, consumed int
	must(t, owner.QueryRow(ctx, `SELECT count(*) FILTER (WHERE event_kind='requested'),count(*) FILTER (WHERE event_kind='approved'),count(*) FILTER (WHERE event_kind='consumed')
	 FROM governance.dangerous_operation_audit_events WHERE workspace_id=$1 AND approval_id IN ('approval_refund_25','approval_refund_over','approval_adjust_7')`, workspace).Scan(&requested, &approved, &consumed))
	if requested != 3 || approved != 3 || consumed != 2 {
		t.Fatal("commerce approval audit sequence incomplete", requested, approved, consumed)
	}

	t.Log("T20/T21 billing ledger verified: zero-sum immutable journals, exact maker-checker refunds/adjustments, over-refund rollback, pending reconciliation and late-fact append-only convergence")
}
