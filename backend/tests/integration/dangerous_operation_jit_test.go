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

func execScoped(t *testing.T, ctx context.Context, pool *pgxpool.Pool, workspace, statement string, args ...any) {
	t.Helper()
	tx := beginWorkspaceTx(t, ctx, pool, workspace)
	_, err := tx.Exec(ctx, statement, args...)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func expectScopedPGCode(t *testing.T, ctx context.Context, pool *pgxpool.Pool, workspace, code, statement string, args ...any) {
	t.Helper()
	tx := beginWorkspaceTx(t, ctx, pool, workspace)
	_, err := tx.Exec(ctx, statement, args...)
	got := pgErrorCode(err)
	_ = tx.Rollback(ctx)
	if err == nil || got != code {
		t.Fatalf("expected PostgreSQL code %s, got code=%s err=%v", code, got, err)
	}
}

func createT24Canary(t *testing.T, ctx context.Context, manager *pgxpool.Pool, id string, at time.Time) {
	t.Helper()
	execScoped(t, ctx, manager, "ws_release_t23", `SELECT supply.create_release_plan($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		"ws_release_t23", id, "example.release", "1.0.0", "set_release", "tv_release", "provider_release", "deploy_release_promote", "deploy_release_candidate", "admin_release", "T24 exact approval drill", at)
	execScoped(t, ctx, manager, "ws_release_t23", `SELECT supply.start_release_canary($1,$2,$3,$4,$5,$6)`,
		"ws_release_t23", id, "admin_release", "T24 canary", at.Add(time.Minute), at.Add(3*time.Minute))
}

func requestReleaseApproval(t *testing.T, ctx context.Context, manager *pgxpool.Pool, approval, plan string, at, expires time.Time) {
	t.Helper()
	execScoped(t, ctx, manager, "ws_release_t23", `SELECT governance.request_release_emergency_approval($1,$2,$3,$4,$5,$6,$7)`,
		"ws_release_t23", approval, "admin_release", plan, "T24 incident", at, expires)
}

func approveDanger(t *testing.T, ctx context.Context, manager *pgxpool.Pool, workspace, approval, reviewer string, at time.Time) {
	t.Helper()
	execScoped(t, ctx, manager, workspace, `SELECT governance.approve_dangerous_operation($1,$2,$3,$4,$5)`, workspace, approval, reviewer, at, "independent review")
}

func exerciseDangerousOperationJIT(t *testing.T, ctx context.Context, owner *pgxpool.Pool, runtimeDSN string) {
	t.Helper()
	dangerous, _ := openTemporaryRole(t, ctx, owner, runtimeDSN, "mender_dangerous_manager_", migrations.GrantDangerousOperationManager)
	release, _ := openTemporaryRole(t, ctx, owner, runtimeDSN, "mender_t24_release_manager_", migrations.GrantReleaseManager)
	support, _ := openTemporaryRole(t, ctx, owner, runtimeDSN, "mender_support_reader_", migrations.GrantSupportReader)
	must(t, database.DangerousOperationManagerRole(ctx, dangerous))
	must(t, database.ReleaseManagerRole(ctx, release))
	must(t, database.SupportReaderRole(ctx, support))
	if database.DangerousOperationManagerRole(ctx, release) == nil || database.ReleaseManagerRole(ctx, dangerous) == nil || database.SupportReaderRole(ctx, dangerous) == nil {
		t.Fatal("cross-purpose restricted role was accepted")
	}

	// The two halves of emergency governance cannot invoke each other's power.
	expectScopedPGCode(t, ctx, release, "ws_release_t23", "42501", `SELECT governance.request_release_emergency_approval($1,$2,$3,$4,$5,$6,$7)`,
		"ws_release_t23", "danger_cross_release", "admin_release", "release_t23_3", "cross-purpose", time.Date(2026, 9, 15, 21, 0, 0, 0, time.UTC), time.Date(2026, 9, 15, 21, 10, 0, 0, time.UTC))
	expectScopedPGCode(t, ctx, dangerous, "ws_release_t23", "42501", `SELECT supply.emergency_disable_release($1,$2,$3,$4,$5,$6)`,
		"ws_release_t23", "release_t23_3", "missing", "admin_release", "cross-purpose", time.Date(2026, 9, 15, 21, 0, 0, 0, time.UTC))
	expectScopedPGCode(t, ctx, dangerous, "ws_release_t23", "42501", `SELECT governance.submit_dangerous_operation($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,$12,$13,$14,$15,$16)`,
		"ws_release_t23", "danger_forge", "admin_release", "workspace_member", "admin_release", "release.emergency_disable", "release_plan", "release_t23_3", "2",
		`{"mode":"different"}`, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", nil, nil, "forged", time.Date(2026, 9, 15, 21, 0, 0, 0, time.UTC), time.Date(2026, 9, 15, 21, 10, 0, 0, time.UTC))

	// Exact revision binding: an approval for canary revision 2 cannot be used
	// after drain advances the plan to revision 3.
	base := time.Date(2026, 9, 15, 21, 0, 0, 0, time.UTC)
	createT24Canary(t, ctx, release, "release_t24_stale", base)
	requestReleaseApproval(t, ctx, dangerous, "danger_t24_stale", "release_t24_stale", base.Add(70*time.Second), base.Add(12*time.Minute))
	expectScopedPGCode(t, ctx, dangerous, "ws_release_t23", "42501", `SELECT governance.approve_dangerous_operation($1,$2,$3,$4,$5)`,
		"ws_release_t23", "danger_t24_stale", "admin_release", base.Add(80*time.Second), "self review")
	approveDanger(t, ctx, dangerous, "ws_release_t23", "danger_t24_stale", "review_release", base.Add(90*time.Second))
	execScoped(t, ctx, release, "ws_release_t23", `SELECT supply.drain_release($1,$2,$3,$4,$5)`, "ws_release_t23", "release_t24_stale", "admin_release", "advance revision", base.Add(2*time.Minute))
	expectScopedPGCode(t, ctx, release, "ws_release_t23", "42501", `SELECT supply.emergency_disable_release($1,$2,$3,$4,$5,$6)`,
		"ws_release_t23", "release_t24_stale", "danger_t24_stale", "admin_release", "stale approval", base.Add(150*time.Second))
	var staleState string
	tx := beginWorkspaceTx(t, ctx, dangerous, "ws_release_t23")
	must(t, tx.QueryRow(ctx, `SELECT state FROM governance.dangerous_operation_approvals WHERE workspace_id=$1 AND id='danger_t24_stale'`, "ws_release_t23").Scan(&staleState))
	must(t, tx.Commit(ctx))
	if staleState != "approved" {
		t.Fatal("failed stale consumption altered approval", staleState)
	}
	execScoped(t, ctx, release, "ws_release_t23", `SELECT supply.rollback_release($1,$2,$3,$4,$5)`, "ws_release_t23", "release_t24_stale", "admin_release", "cleanup stale drill", base.Add(3*time.Minute))

	// Expiry is checked at consumption time even after independent review.
	createT24Canary(t, ctx, release, "release_t24_expired", base.Add(4*time.Minute))
	requestReleaseApproval(t, ctx, dangerous, "danger_t24_expired", "release_t24_expired", base.Add(5*time.Minute+10*time.Second), base.Add(6*time.Minute+10*time.Second))
	approveDanger(t, ctx, dangerous, "ws_release_t23", "danger_t24_expired", "review_release", base.Add(5*time.Minute+20*time.Second))
	expectScopedPGCode(t, ctx, release, "ws_release_t23", "42501", `SELECT supply.emergency_disable_release($1,$2,$3,$4,$5,$6)`,
		"ws_release_t23", "release_t24_expired", "danger_t24_expired", "admin_release", "expired approval", base.Add(6*time.Minute+10*time.Second))
	execScoped(t, ctx, release, "ws_release_t23", `SELECT supply.rollback_release($1,$2,$3,$4,$5)`, "ws_release_t23", "release_t24_expired", "admin_release", "cleanup expiry drill", base.Add(7*time.Minute))

	// Exact requester and one-time consumption are both enforced.
	createT24Canary(t, ctx, release, "release_t24_consume", base.Add(8*time.Minute))
	requestReleaseApproval(t, ctx, dangerous, "danger_t24_consume", "release_t24_consume", base.Add(9*time.Minute+10*time.Second), base.Add(20*time.Minute))
	approveDanger(t, ctx, dangerous, "ws_release_t23", "danger_t24_consume", "review_release", base.Add(9*time.Minute+20*time.Second))
	expectScopedPGCode(t, ctx, release, "ws_release_t23", "42501", `SELECT supply.emergency_disable_release($1,$2,$3,$4,$5,$6)`,
		"ws_release_t23", "release_t24_consume", "danger_t24_consume", "review_release", "wrong requester", base.Add(9*time.Minute+30*time.Second))
	execScoped(t, ctx, release, "ws_release_t23", `SELECT supply.emergency_disable_release($1,$2,$3,$4,$5,$6)`,
		"ws_release_t23", "release_t24_consume", "danger_t24_consume", "admin_release", "confirmed emergency", base.Add(9*time.Minute+40*time.Second))
	tx = beginWorkspaceTx(t, ctx, dangerous, "ws_release_t23")
	var consumedState string
	var consumedAt *time.Time
	must(t, tx.QueryRow(ctx, `SELECT state,consumed_at FROM governance.dangerous_operation_approvals WHERE workspace_id=$1 AND id='danger_t24_consume'`, "ws_release_t23").Scan(&consumedState, &consumedAt))
	must(t, tx.Commit(ctx))
	if consumedState != "consumed" || consumedAt == nil {
		t.Fatal("emergency approval was not consumed", consumedState, consumedAt)
	}
	tx = beginWorkspaceTx(t, ctx, owner, "ws_release_t23")
	_, err := tx.Exec(ctx, `SELECT governance.consume_release_emergency_approval($1,$2,$3,$4,$5,$6)`, "ws_release_t23", "danger_t24_consume", "admin_release", "release_t24_consume", int64(2), base.Add(10*time.Minute))
	if err == nil || pgErrorCode(err) != "42501" {
		t.Fatal("consumed emergency approval was reusable", err)
	}
	_ = tx.Rollback(ctx)
	execScoped(t, ctx, release, "ws_release_t23", `SELECT supply.rollback_release($1,$2,$3,$4,$5)`, "ws_release_t23", "release_t24_consume", "admin_release", "recover after exact consume", base.Add(11*time.Minute))

	// T26: Platform Staff exists globally but has no tenant membership. JIT is
	// maker/checker, one-time activation, scoped, expiring and revocable.
	supportWorkspace := "ws_support_t26"
	supportAt := time.Date(2026, 9, 15, 22, 0, 0, 0, time.UTC)
	_, err = owner.Exec(ctx, `INSERT INTO identity.workspaces(id,created_at) VALUES($1,$2)`, supportWorkspace, supportAt)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO identity.users(id,display_name,created_at) VALUES
	 ('staff_support','Support Staff',$1),('staff_reviewer','Support Reviewer',$1)`, supportAt)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO identity.platform_staff(user_id,role,created_at) VALUES
	 ('staff_support','support',$1),('staff_reviewer','reviewer',$1)`, supportAt)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO execution.runs(workspace_id,id,state,version,created_at,updated_at) VALUES($1,'run_support_visible','queued',1,$2,$2)`, supportWorkspace, supportAt)
	must(t, err)
	var membershipCount int
	must(t, owner.QueryRow(ctx, `SELECT count(*) FROM identity.workspace_memberships WHERE workspace_id=$1 AND user_id IN ('staff_support','staff_reviewer')`, supportWorkspace).Scan(&membershipCount))
	if membershipCount != 0 {
		t.Fatal("Platform Staff unexpectedly became tenant members", membershipCount)
	}

	expectScopedPGCode(t, ctx, dangerous, supportWorkspace, "22023", `SELECT governance.request_support_jit_approval($1,$2,$3,$4,$5,$6,$7,$8)`,
		supportWorkspace, "danger_support_bad_scope", "staff_support", []string{"run:cancel"}, 600, "unsafe scope", supportAt.Add(time.Minute), supportAt.Add(10*time.Minute))
	execScoped(t, ctx, dangerous, supportWorkspace, `SELECT governance.request_support_jit_approval($1,$2,$3,$4,$5,$6,$7,$8)`,
		supportWorkspace, "danger_support_active", "staff_support", []string{"run:read"}, 600, "customer incident", supportAt.Add(time.Minute), supportAt.Add(15*time.Minute))
	expectScopedPGCode(t, ctx, dangerous, supportWorkspace, "42501", `SELECT governance.approve_dangerous_operation($1,$2,$3,$4,$5)`,
		supportWorkspace, "danger_support_active", "staff_support", supportAt.Add(70*time.Second), "self review")
	approveDanger(t, ctx, dangerous, supportWorkspace, "danger_support_active", "staff_reviewer", supportAt.Add(80*time.Second))
	expectScopedPGCode(t, ctx, support, supportWorkspace, "42501", `SELECT * FROM governance.list_jit_support_runs($1,$2,$3)`, supportWorkspace, "staff_support", supportAt.Add(90*time.Second))

	tx = beginWorkspaceTx(t, ctx, dangerous, supportWorkspace)
	var grantExpiry time.Time
	must(t, tx.QueryRow(ctx, `SELECT governance.activate_jit_support($1,$2,$3,$4,$5)`, supportWorkspace, "danger_support_active", "jit_support_active", "staff_support", supportAt.Add(2*time.Minute)).Scan(&grantExpiry))
	must(t, tx.Commit(ctx))
	if !grantExpiry.Equal(supportAt.Add(12 * time.Minute)) {
		t.Fatal("JIT grant expiry drifted from approved TTL", grantExpiry)
	}
	expectScopedPGCode(t, ctx, dangerous, supportWorkspace, "42501", `SELECT governance.activate_jit_support($1,$2,$3,$4,$5)`, supportWorkspace, "danger_support_active", "jit_support_replay", "staff_support", supportAt.Add(3*time.Minute))

	tx = beginWorkspaceTx(t, ctx, support, supportWorkspace)
	var runID, runState string
	var runVersion int64
	must(t, tx.QueryRow(ctx, `SELECT id,state,version FROM governance.list_jit_support_runs($1,$2,$3)`, supportWorkspace, "staff_support", supportAt.Add(3*time.Minute)).Scan(&runID, &runState, &runVersion))
	must(t, tx.Commit(ctx))
	if runID != "run_support_visible" || runState != "queued" || runVersion != 1 {
		t.Fatal("JIT support projection drifted", runID, runState, runVersion)
	}
	expectScopedPGCode(t, ctx, support, supportWorkspace, "42501", `SELECT count(*) FROM execution.runs`)
	expectScopedPGCode(t, ctx, support, supportWorkspace, "42501", `SELECT count(*) FROM execution.run_admissions`)
	expectScopedPGCode(t, ctx, support, supportWorkspace, "42501", `SELECT count(*) FROM execution.run_events`)
	expectScopedPGCode(t, ctx, support, supportWorkspace, "42501", `SELECT count(*) FROM execution.artifacts`)
	expectScopedPGCode(t, ctx, support, supportWorkspace, "42501", `SELECT count(*) FROM governance.jit_support_grants`)
	expectScopedPGCode(t, ctx, support, supportWorkspace, "42501", `SELECT count(*) FROM identity.platform_staff`)
	expectScopedPGCode(t, ctx, support, "ws_support_other", "42501", `SELECT * FROM governance.list_jit_support_runs($1,$2,$3)`, supportWorkspace, "staff_support", supportAt.Add(3*time.Minute))
	expectScopedPGCode(t, ctx, support, supportWorkspace, "42501", `SELECT * FROM governance.list_jit_support_runs($1,$2,$3)`, supportWorkspace, "staff_support", grantExpiry)

	// A separately approved grant is revoked before expiry; every later read is
	// denied by the DB projection itself, not merely by application convention.
	execScoped(t, ctx, dangerous, supportWorkspace, `SELECT governance.request_support_jit_approval($1,$2,$3,$4,$5,$6,$7,$8)`,
		supportWorkspace, "danger_support_revoke", "staff_support", []string{"run:read"}, 1200, "second incident", supportAt.Add(4*time.Minute), supportAt.Add(18*time.Minute))
	approveDanger(t, ctx, dangerous, supportWorkspace, "danger_support_revoke", "staff_reviewer", supportAt.Add(5*time.Minute))
	execScoped(t, ctx, dangerous, supportWorkspace, `SELECT governance.activate_jit_support($1,$2,$3,$4,$5)`, supportWorkspace, "danger_support_revoke", "jit_support_revoke", "staff_support", supportAt.Add(6*time.Minute))
	tx = beginWorkspaceTx(t, ctx, support, supportWorkspace)
	must(t, tx.QueryRow(ctx, `SELECT id FROM governance.list_jit_support_runs($1,$2,$3)`, supportWorkspace, "staff_support", supportAt.Add(12*time.Minute+30*time.Second)).Scan(&runID))
	must(t, tx.Commit(ctx))
	execScoped(t, ctx, dangerous, supportWorkspace, `SELECT governance.revoke_jit_support($1,$2,$3,$4,$5)`, supportWorkspace, "jit_support_revoke", "staff_reviewer", supportAt.Add(13*time.Minute), "incident resolved")
	expectScopedPGCode(t, ctx, support, supportWorkspace, "42501", `SELECT * FROM governance.list_jit_support_runs($1,$2,$3)`, supportWorkspace, "staff_support", supportAt.Add(14*time.Minute))
	must(t, owner.QueryRow(ctx, `SELECT count(*) FROM identity.workspace_memberships WHERE workspace_id=$1 AND user_id IN ('staff_support','staff_reviewer')`, supportWorkspace).Scan(&membershipCount))
	if membershipCount != 0 {
		t.Fatal("JIT lifecycle mutated tenant membership", membershipCount)
	}

	var requested, approved, consumed, granted, revoked int
	must(t, owner.QueryRow(ctx, `SELECT
	 count(*) FILTER (WHERE event_kind='requested'),count(*) FILTER (WHERE event_kind='approved'),
	 count(*) FILTER (WHERE event_kind='consumed'),count(*) FILTER (WHERE event_kind='jit_granted'),
	 count(*) FILTER (WHERE event_kind='jit_revoked')
	 FROM governance.dangerous_operation_audit_events WHERE workspace_id=$1`, supportWorkspace).Scan(&requested, &approved, &consumed, &granted, &revoked))
	if requested != 2 || approved != 2 || consumed != 2 || granted != 2 || revoked != 1 {
		t.Fatal("JIT audit sequence incomplete", requested, approved, consumed, granted, revoked)
	}
}
