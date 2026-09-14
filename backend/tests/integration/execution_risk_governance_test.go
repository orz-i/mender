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

func ensurePermissiveExecutionPolicy(t *testing.T, ctx context.Context, owner *pgxpool.Pool, workspace string, at time.Time) {
	t.Helper()
	tx, err := owner.Begin(ctx)
	must(t, err)
	defer func() { _ = tx.Rollback(context.Background()) }()
	_, err = tx.Exec(ctx, `SELECT set_config('mender.workspace_id',$1,true)`, workspace)
	must(t, err)
	var active int
	must(t, tx.QueryRow(ctx, `SELECT count(*) FROM governance.execution_policy_revisions WHERE workspace_id=$1 AND state='active'`, workspace).Scan(&active))
	if active == 0 {
		id := "integration_execution_policy_v1"
		_, err = tx.Exec(ctx, `SELECT governance.create_execution_policy($1,$2,'integration_admin','critical',false,$3)`, workspace, id, at)
		must(t, err)
		_, err = tx.Exec(ctx, `SELECT governance.activate_execution_policy($1,$2,'integration_admin',$3)`, workspace, id, at)
		must(t, err)
	}
	must(t, tx.Commit(ctx))
}

func exerciseExecutionRiskGovernance(t *testing.T, ctx context.Context, owner *pgxpool.Pool, runtimeDSN string) {
	t.Helper()
	policyManager, _ := openTemporaryRole(t, ctx, owner, runtimeDSN, "mender_exec_policy_", migrations.GrantGovernancePolicyManager)
	must(t, database.GovernancePolicyManagerRole(ctx, policyManager))

	at := time.Now().UTC().Truncate(time.Microsecond)
	_, err := owner.Exec(ctx, `INSERT INTO identity.workspaces(id,created_at) VALUES('ws_exec_policy',$1)`, at)
	must(t, err)
	for _, seed := range []struct {
		id, tool, effect, idem, set string
	}{
		{"tv_exec_safe", "tool_exec_safe", "read_only", "safe_read", "set_exec_safe"},
		{"tv_exec_unsafe", "tool_exec_unsafe", "write", "unsafe", "set_exec_unsafe"},
	} {
		_, err = owner.Exec(ctx, `INSERT INTO catalog.tool_versions(
		 id,tool_id,version,provider_id,price_version_id,deployment_revision,title,description,input_schema,output_schema,
		 side_effect,idempotency,mcp_publishable,state,published_at)
		 VALUES($1,$2,'1.0.0','provider_exec','price_exec','deploy_exec',$2,'','{"type":"object"}'::jsonb,'{"type":"object"}'::jsonb,$3,$4,false,'published',$5)`, seed.id, seed.tool, seed.effect, seed.idem, at)
		must(t, err)
		_, err = owner.Exec(ctx, `INSERT INTO distribution.toolset_bindings(
		 workspace_id,toolset_version_id,tool_id,tool_version_label,tool_version_id,budget_id,connection_id,mcp_name,mcp_exposed,state,published_at)
		 VALUES('ws_exec_policy',$1,$2,'1.0.0',$3,'budget_exec','conn_exec',NULL,false,'published',$4)`, seed.set, seed.tool, seed.id, at)
		must(t, err)
	}

	// New workspaces have no implicit policy: execution policy evaluation must fail closed.
	ownerTx, err := owner.Begin(ctx)
	must(t, err)
	_, err = ownerTx.Exec(ctx, `SELECT set_config('mender.workspace_id','ws_exec_policy',true)`)
	must(t, err)
	if _, err = ownerTx.Exec(ctx, `SELECT governance.evaluate_execution_policy(
	 'ws_exec_policy','human','user_exec','set_exec_safe','tv_exec_safe','conn_exec',
	 repeat('a',64),repeat('b',64),$1)`, at); err == nil {
		t.Fatal("new Workspace evaluated execution risk without an active policy")
	}
	_ = ownerTx.Rollback(ctx)

	policyTx, err := policyManager.Begin(ctx)
	must(t, err)
	_, err = policyTx.Exec(ctx, `SELECT set_config('mender.workspace_id','ws_exec_policy',true)`)
	must(t, err)
	var revision int64
	must(t, policyTx.QueryRow(ctx, `SELECT governance.create_execution_policy(
	 'ws_exec_policy','exec_allow_v1','policy_admin','critical',false,$1)`, at.Add(time.Second)).Scan(&revision))
	if revision != 1 {
		t.Fatal("unexpected first execution policy revision", revision)
	}
	_, err = policyTx.Exec(ctx, `SELECT governance.activate_execution_policy('ws_exec_policy','exec_allow_v1','policy_admin',$1)`, at.Add(2*time.Second))
	must(t, err)
	if _, err = policyTx.Exec(ctx, `UPDATE governance.execution_policy_revisions SET max_unconfirmed_risk_level='low' WHERE workspace_id='ws_exec_policy'`); err == nil {
		t.Fatal("policy manager obtained direct execution policy mutation")
	}
	_ = policyTx.Rollback(ctx)

	// The failed direct UPDATE aborts the transaction; create/activate again in a clean transaction.
	policyTx, err = policyManager.Begin(ctx)
	must(t, err)
	_, err = policyTx.Exec(ctx, `SELECT set_config('mender.workspace_id','ws_exec_policy',true)`)
	must(t, err)
	must(t, policyTx.QueryRow(ctx, `SELECT governance.create_execution_policy(
	 'ws_exec_policy','exec_allow_v1','policy_admin','critical',false,$1)`, at.Add(time.Second)).Scan(&revision))
	_, err = policyTx.Exec(ctx, `SELECT governance.activate_execution_policy('ws_exec_policy','exec_allow_v1','policy_admin',$1)`, at.Add(2*time.Second))
	must(t, err)
	must(t, policyTx.Commit(ctx))

	eval := func(subjectKind, subjectID, toolset, toolVersion, argsHash, idemHash string, when time.Time) (int64, string, string, []string) {
		t.Helper()
		tx, e := owner.Begin(ctx)
		must(t, e)
		defer func() { _ = tx.Rollback(context.Background()) }()
		_, e = tx.Exec(ctx, `SELECT set_config('mender.workspace_id','ws_exec_policy',true)`)
		must(t, e)
		var sequence int64
		must(t, tx.QueryRow(ctx, `SELECT governance.evaluate_execution_policy(
		 'ws_exec_policy',$1,$2,$3,$4,'conn_exec',$5,$6,$7)`, subjectKind, subjectID, toolset, toolVersion, argsHash, idemHash, when).Scan(&sequence))
		var risk, outcome string
		var reasons []string
		must(t, tx.QueryRow(ctx, `SELECT risk_level,outcome,reason_codes FROM governance.execution_policy_decisions WHERE sequence=$1`, sequence).Scan(&risk, &outcome, &reasons))
		must(t, tx.Commit(ctx))
		return sequence, risk, outcome, reasons
	}

	seq, risk, outcome, _ := eval("human", "user_exec", "set_exec_unsafe", "tv_exec_unsafe", repeatHex('a'), repeatHex('b'), at.Add(3*time.Second))
	if seq < 1 || risk != "critical" || outcome != "allow" {
		t.Fatal("compatibility execution policy did not allow unsafe write", seq, risk, outcome)
	}
	if _, err = owner.Exec(ctx, `UPDATE governance.execution_policy_decisions SET outcome='deny' WHERE sequence=$1`, seq); err == nil {
		t.Fatal("immutable execution policy decision accepted UPDATE")
	}

	policyTx, err = policyManager.Begin(ctx)
	must(t, err)
	_, err = policyTx.Exec(ctx, `SELECT set_config('mender.workspace_id','ws_exec_policy',true)`)
	must(t, err)
	must(t, policyTx.QueryRow(ctx, `SELECT governance.create_execution_policy(
	 'ws_exec_policy','exec_confirm_v2','policy_admin','low',false,$1)`, at.Add(4*time.Second)).Scan(&revision))
	if revision != 2 {
		t.Fatal("execution policy revision did not advance", revision)
	}
	_, err = policyTx.Exec(ctx, `SELECT governance.activate_execution_policy('ws_exec_policy','exec_confirm_v2','policy_admin',$1)`, at.Add(5*time.Second))
	must(t, err)
	must(t, policyTx.Commit(ctx))

	_, risk, outcome, reasons := eval("human", "user_exec", "set_exec_unsafe", "tv_exec_unsafe", repeatHex('c'), repeatHex('d'), at.Add(6*time.Second))
	if risk != "critical" || outcome != "confirmation_required" || !containsString(reasons, "human_confirmation_required") {
		t.Fatal("strict human execution policy did not require confirmation", risk, outcome, reasons)
	}
	_, _, outcome, reasons = eval("machine", "sa_exec", "set_exec_unsafe", "tv_exec_unsafe", repeatHex('c'), repeatHex('d'), at.Add(6*time.Second))
	if outcome != "deny" || !containsString(reasons, "machine_confirmation_unavailable") {
		t.Fatal("machine caller bypassed human confirmation requirement", outcome, reasons)
	}

	confirmTx, err := owner.Begin(ctx)
	must(t, err)
	_, err = confirmTx.Exec(ctx, `SELECT set_config('mender.workspace_id','ws_exec_policy',true)`)
	must(t, err)
	var confirmDecision int64
	var confirmationID *string
	must(t, confirmTx.QueryRow(ctx, `SELECT decision_sequence,confirmation_id_result FROM governance.create_execution_confirmation(
	 'ws_exec_policy','confirm_exec_1','user_exec','set_exec_unsafe','tv_exec_unsafe','conn_exec',$1,$2,$3,$4)`,
		repeatHex('c'), repeatHex('d'), at.Add(7*time.Second), at.Add(5*time.Minute)).Scan(&confirmDecision, &confirmationID))
	if confirmDecision < 1 || confirmationID == nil || *confirmationID != "confirm_exec_1" {
		t.Fatal("human confirmation was not created for exact dangerous action", confirmDecision, confirmationID)
	}
	var consumed *string
	must(t, confirmTx.QueryRow(ctx, `SELECT governance.consume_execution_confirmation(
	 'ws_exec_policy','user_exec','set_exec_unsafe','tv_exec_unsafe','conn_exec',$1,$2,$3)`,
		repeatHex('e'), repeatHex('d'), at.Add(8*time.Second)).Scan(&consumed))
	if consumed != nil {
		t.Fatal("arguments hash mismatch consumed confirmation", *consumed)
	}
	must(t, confirmTx.QueryRow(ctx, `SELECT governance.consume_execution_confirmation(
	 'ws_exec_policy','user_exec','set_exec_unsafe','tv_exec_unsafe','conn_exec',$1,$2,$3)`,
		repeatHex('c'), repeatHex('d'), at.Add(8*time.Second)).Scan(&consumed))
	if consumed == nil || *consumed != "confirm_exec_1" {
		t.Fatal("exact confirmation was not consumed", consumed)
	}
	var replay *string
	must(t, confirmTx.QueryRow(ctx, `SELECT governance.consume_execution_confirmation(
	 'ws_exec_policy','user_exec','set_exec_unsafe','tv_exec_unsafe','conn_exec',$1,$2,$3)`,
		repeatHex('c'), repeatHex('d'), at.Add(9*time.Second)).Scan(&replay))
	if replay != nil {
		t.Fatal("single-use confirmation was replayed", *replay)
	}
	must(t, confirmTx.Commit(ctx))

	// A policy revision change invalidates still-active confirmations.
	confirmTx, err = owner.Begin(ctx)
	must(t, err)
	_, err = confirmTx.Exec(ctx, `SELECT set_config('mender.workspace_id','ws_exec_policy',true)`)
	must(t, err)
	must(t, confirmTx.QueryRow(ctx, `SELECT decision_sequence,confirmation_id_result FROM governance.create_execution_confirmation(
	 'ws_exec_policy','confirm_exec_stale','user_exec','set_exec_unsafe','tv_exec_unsafe','conn_exec',$1,$2,$3,$4)`,
		repeatHex('f'), repeatHex('1'), at.Add(10*time.Second), at.Add(5*time.Minute)).Scan(&confirmDecision, &confirmationID))
	must(t, confirmTx.Commit(ctx))

	policyTx, err = policyManager.Begin(ctx)
	must(t, err)
	_, err = policyTx.Exec(ctx, `SELECT set_config('mender.workspace_id','ws_exec_policy',true)`)
	must(t, err)
	must(t, policyTx.QueryRow(ctx, `SELECT governance.create_execution_policy(
	 'ws_exec_policy','exec_confirm_v3','policy_admin','low',false,$1)`, at.Add(11*time.Second)).Scan(&revision))
	_, err = policyTx.Exec(ctx, `SELECT governance.activate_execution_policy('ws_exec_policy','exec_confirm_v3','policy_admin',$1)`, at.Add(12*time.Second))
	must(t, err)
	must(t, policyTx.Commit(ctx))
	var confirmationState string
	must(t, owner.QueryRow(ctx, `SELECT state FROM governance.execution_confirmations WHERE workspace_id='ws_exec_policy' AND id='confirm_exec_stale'`).Scan(&confirmationState))
	if confirmationState != "expired" {
		t.Fatal("policy activation did not expire outstanding execution confirmation", confirmationState)
	}

	policyTx, err = policyManager.Begin(ctx)
	must(t, err)
	_, err = policyTx.Exec(ctx, `SELECT set_config('mender.workspace_id','ws_exec_policy',true)`)
	must(t, err)
	var observedConfirmation string
	if err = policyTx.QueryRow(ctx, `SELECT id FROM governance.execution_confirmations WHERE workspace_id='ws_exec_policy' ORDER BY created_at LIMIT 1`).Scan(&observedConfirmation); err != nil || observedConfirmation == "" {
		t.Fatal("governance policy manager could not read execution confirmation projection", err)
	}
	if _, err = policyTx.Exec(ctx, `UPDATE governance.execution_confirmations SET state='expired',expired_at=$1 WHERE workspace_id='ws_exec_policy'`, at.Add(20*time.Second)); err == nil {
		t.Fatal("governance policy manager obtained execution confirmation mutation authority")
	}
	_ = policyTx.Rollback(ctx)
}

func repeatHex(ch byte) string {
	b := make([]byte, 64)
	for i := range b {
		b[i] = ch
	}
	return string(b)
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
