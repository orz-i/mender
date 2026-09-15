//go:build integration

package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	"github.com/orz-i/mender/backend/migrations"
)

func pluginManifest(pluginID, version, displayName string) (string, string) {
	body := `{"apiVersion":"mender.io/plugin/v1alpha1","plugin_id":"` + pluginID + `","version":"` + version + `","publisher_id":"publisher_s4a","display_name":"` + displayName + `","description":"Reviewed Agent capability only.","capabilities":[{"kind":"agent","deployment_revision":"deploy_s4a_agent"}]}`
	sum := sha256.Sum256([]byte(body))
	return body, hex.EncodeToString(sum[:])
}

func pgErrorCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

func beginWorkspaceTx(t *testing.T, ctx context.Context, pool *pgxpool.Pool, workspace string) pgx.Tx {
	t.Helper()
	tx, err := pool.Begin(ctx)
	must(t, err)
	_, err = tx.Exec(ctx, `SELECT set_config('mender.workspace_id',$1,true)`, workspace)
	must(t, err)
	return tx
}

func exercisePluginPublication(t *testing.T, ctx context.Context, owner *pgxpool.Pool, runtimeDSN string) {
	t.Helper()
	publisher, _ := openTemporaryRole(t, ctx, owner, runtimeDSN, "mender_publisher_manager_", migrations.GrantPublisherManager)
	reviewer, _ := openTemporaryRole(t, ctx, owner, runtimeDSN, "mender_plugin_reviewer_", migrations.GrantGovernanceReviewer)
	must(t, database.PublisherManagerRole(ctx, publisher))
	must(t, database.GovernanceReviewerRole(ctx, reviewer))
	if database.PublisherManagerRole(ctx, owner) == nil || database.GovernanceReviewerRole(ctx, publisher) == nil || database.PublisherManagerRole(ctx, reviewer) == nil {
		t.Fatal("elevated or cross-purpose database role accepted for Plugin publication")
	}

	at := time.Date(2026, 9, 15, 15, 30, 0, 0, time.UTC)
	_, err := owner.Exec(ctx, `INSERT INTO supply.deployments(
	 revision,provider_id,transport_kind,endpoint_url,http_method,status_endpoint_url,status_http_method,cancel_endpoint_url,cancel_http_method,
	 auth_mode,auth_header_name,idempotency_header,request_timeout_ms,max_request_bytes,max_response_bytes,state,created_at)
	 VALUES('deploy_s4a_agent','provider_s4a_agent','agent_http','https://agent.example.test/submit','POST','https://agent.example.test/status','POST','https://agent.example.test/cancel','POST',
	 'bearer',NULL,'Idempotency-Key',1000,65536,1048576,'active',$1)`, at)
	must(t, err)

	manifest, digest := pluginManifest("example.search", "1.0.0", "Example search")
	tx := beginWorkspaceTx(t, ctx, publisher, "ws_plugin_a")
	_, err = tx.Exec(ctx, `INSERT INTO supply.publishers(workspace_id,id,owner_user_id,display_name)
	 VALUES('ws_plugin_a','publisher_s4a','user_publisher','S4-A Publisher')`)
	must(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO supply.plugins(workspace_id,id,publisher_id,created_by_user_id)
	 VALUES('ws_plugin_a','example.search','publisher_s4a','user_publisher')`)
	must(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO supply.plugin_versions(workspace_id,plugin_id,version,publisher_id,manifest_json,manifest_sha256,created_by_user_id)
	 VALUES('ws_plugin_a','example.search','1.0.0','publisher_s4a',$1::jsonb,$2,'user_publisher')`, manifest, digest)
	must(t, err)
	must(t, tx.Commit(ctx))

	// The restricted role cannot smuggle arbitrary executable metadata through
	// a direct SQL insert even if the application decoder is bypassed.
	badManifest := `{"apiVersion":"mender.io/plugin/v1alpha1","plugin_id":"example.search","version":"9.9.9","publisher_id":"publisher_s4a","display_name":"Bad","description":"","capabilities":[{"kind":"agent","deployment_revision":"deploy_s4a_agent"}],"script":"rm -rf /"}`
	tx = beginWorkspaceTx(t, ctx, publisher, "ws_plugin_a")
	_, err = tx.Exec(ctx, `INSERT INTO supply.plugin_versions(workspace_id,plugin_id,version,publisher_id,manifest_json,manifest_sha256,created_by_user_id)
	 VALUES('ws_plugin_a','example.search','9.9.9','publisher_s4a',$1::jsonb,$2,'user_publisher')`, badManifest, digest)
	if err == nil || pgErrorCode(err) != "23514" {
		t.Fatal("direct malformed Plugin manifest was not rejected by database contract", err)
	}
	_ = tx.Rollback(ctx)

	// FORCE RLS hides and rejects another Workspace even for the management role.
	tx = beginWorkspaceTx(t, ctx, publisher, "ws_plugin_b")
	var crossCount int
	must(t, tx.QueryRow(ctx, `SELECT count(*) FROM supply.publishers WHERE workspace_id='ws_plugin_a'`).Scan(&crossCount))
	if crossCount != 0 {
		t.Fatal("Publisher RLS exposed another Workspace", crossCount)
	}
	_, err = tx.Exec(ctx, `INSERT INTO supply.publishers(workspace_id,id,owner_user_id,display_name)
	 VALUES('ws_plugin_a','publisher_cross','user_cross','Cross Workspace')`)
	if err == nil {
		t.Fatal("Publisher RLS accepted a cross-Workspace insert")
	}
	_ = tx.Rollback(ctx)

	// The governance wrapper rechecks persisted Publisher ownership instead of
	// trusting a caller-supplied requester identity.
	tx = beginWorkspaceTx(t, ctx, publisher, "ws_plugin_a")
	_, err = tx.Exec(ctx, `SELECT governance.submit_plugin_publication($1,$2,$3,$4,$5,$6,$7)`,
		"ws_plugin_a", "plugin_approval_wrong_owner", "example.search", "1.0.0", "user_other", at.Add(time.Minute), at.Add(2*time.Hour))
	if err == nil || pgErrorCode(err) != "42501" {
		t.Fatal("non-owner submitted Plugin publication", err)
	}
	_ = tx.Rollback(ctx)

	submitAt := at.Add(2 * time.Minute)
	tx = beginWorkspaceTx(t, ctx, publisher, "ws_plugin_a")
	_, err = tx.Exec(ctx, `SELECT governance.submit_plugin_publication($1,$2,$3,$4,$5,$6,$7)`,
		"ws_plugin_a", "plugin_approval_1", "example.search", "1.0.0", "user_publisher", submitAt, submitAt.Add(24*time.Hour))
	must(t, err)
	must(t, tx.Commit(ctx))

	tx = beginWorkspaceTx(t, ctx, publisher, "ws_plugin_a")
	var state string
	var revision int64
	must(t, tx.QueryRow(ctx, `SELECT state,revision FROM supply.plugin_versions WHERE workspace_id='ws_plugin_a' AND plugin_id='example.search' AND version='1.0.0'`).Scan(&state, &revision))
	if state != "submitted" || revision != 1 {
		t.Fatal("submission did not bind exact draft revision", state, revision)
	}
	must(t, tx.Commit(ctx))

	// Maker/checker is enforced again in the database wrapper.
	tx = beginWorkspaceTx(t, ctx, reviewer, "ws_plugin_a")
	_, err = tx.Exec(ctx, `SELECT governance.approve_plugin_publication($1,$2,$3,$4,$5)`, "ws_plugin_a", "plugin_approval_1", "user_publisher", submitAt.Add(time.Minute), "self")
	if err == nil || pgErrorCode(err) != "42501" {
		t.Fatal("requester self-approved Plugin publication", err)
	}
	_ = tx.Rollback(ctx)

	// Rejection returns only this exact version to draft without rewriting the
	// approval record or historical audit.
	tx = beginWorkspaceTx(t, ctx, reviewer, "ws_plugin_a")
	_, err = tx.Exec(ctx, `SELECT governance.reject_plugin_publication($1,$2,$3,$4,$5)`, "ws_plugin_a", "plugin_approval_1", "user_reviewer", submitAt.Add(2*time.Minute), "needs clarification")
	must(t, err)
	must(t, tx.Commit(ctx))

	tx = beginWorkspaceTx(t, ctx, publisher, "ws_plugin_a")
	must(t, tx.QueryRow(ctx, `SELECT state,revision FROM supply.plugin_versions WHERE workspace_id='ws_plugin_a' AND plugin_id='example.search' AND version='1.0.0'`).Scan(&state, &revision))
	if state != "draft" || revision != 1 {
		t.Fatal("rejected PluginVersion did not return to the same draft revision", state, revision)
	}
	updatedManifest, updatedDigest := pluginManifest("example.search", "1.0.0", "Example search reviewed")
	_, err = tx.Exec(ctx, `UPDATE supply.plugin_versions SET manifest_json=$1::jsonb,manifest_sha256=$2
	 WHERE workspace_id='ws_plugin_a' AND plugin_id='example.search' AND version='1.0.0'`, updatedManifest, updatedDigest)
	must(t, err)
	must(t, tx.QueryRow(ctx, `SELECT revision FROM supply.plugin_versions WHERE workspace_id='ws_plugin_a' AND plugin_id='example.search' AND version='1.0.0'`).Scan(&revision))
	if revision != 2 {
		t.Fatal("draft manifest edit did not advance revision", revision)
	}
	must(t, tx.Commit(ctx))

	secondSubmitAt := submitAt.Add(3 * time.Minute)
	tx = beginWorkspaceTx(t, ctx, publisher, "ws_plugin_a")
	_, err = tx.Exec(ctx, `SELECT governance.submit_plugin_publication($1,$2,$3,$4,$5,$6,$7)`,
		"ws_plugin_a", "plugin_approval_2", "example.search", "1.0.0", "user_publisher", secondSubmitAt, secondSubmitAt.Add(24*time.Hour))
	must(t, err)
	must(t, tx.Commit(ctx))

	tx = beginWorkspaceTx(t, ctx, reviewer, "ws_plugin_a")
	var targetRevision int64
	must(t, tx.QueryRow(ctx, `SELECT target_revision FROM governance.plugin_publication_approvals WHERE workspace_id='ws_plugin_a' AND id='plugin_approval_2'`).Scan(&targetRevision))
	if targetRevision != 2 {
		t.Fatal("approval did not bind edited PluginVersion revision", targetRevision)
	}
	_, err = tx.Exec(ctx, `SELECT governance.approve_plugin_publication($1,$2,$3,$4,$5)`, "ws_plugin_a", "plugin_approval_2", "user_reviewer", secondSubmitAt.Add(time.Minute), "reviewed")
	must(t, err)
	must(t, tx.Commit(ctx))

	// Submitted/approved manifest material is immutable even though the
	// publisher-manager retains draft update permission.
	tx = beginWorkspaceTx(t, ctx, publisher, "ws_plugin_a")
	_, err = tx.Exec(ctx, `UPDATE supply.plugin_versions SET manifest_json=manifest_json || '{"display_name":"tampered"}'::jsonb
	 WHERE workspace_id='ws_plugin_a' AND plugin_id='example.search' AND version='1.0.0'`)
	if err == nil || pgErrorCode(err) != "23514" {
		t.Fatal("approved PluginVersion manifest remained mutable", err)
	}
	_ = tx.Rollback(ctx)

	// Final publication re-runs capability preflight; a reviewed Agent that is
	// disabled after approval cannot be published on stale approval evidence.
	_, err = owner.Exec(ctx, `UPDATE supply.deployments SET state='disabled' WHERE revision='deploy_s4a_agent'`)
	must(t, err)
	tx = beginWorkspaceTx(t, ctx, publisher, "ws_plugin_a")
	var approvalID *string
	err = tx.QueryRow(ctx, `SELECT governance.publish_approved_plugin($1,$2,$3,$4,$5)`, "ws_plugin_a", "example.search", "1.0.0", "user_publisher", secondSubmitAt.Add(2*time.Minute)).Scan(&approvalID)
	if err == nil || pgErrorCode(err) != "23514" {
		t.Fatal("stale capability approval bypassed final publication preflight", err)
	}
	_ = tx.Rollback(ctx)
	_, err = owner.Exec(ctx, `UPDATE supply.deployments SET state='active' WHERE revision='deploy_s4a_agent'`)
	must(t, err)

	publishAt := secondSubmitAt.Add(3 * time.Minute)
	tx = beginWorkspaceTx(t, ctx, publisher, "ws_plugin_a")
	approvalID = nil
	must(t, tx.QueryRow(ctx, `SELECT governance.publish_approved_plugin($1,$2,$3,$4,$5)`, "ws_plugin_a", "example.search", "1.0.0", "user_publisher", publishAt).Scan(&approvalID))
	if approvalID == nil || *approvalID != "plugin_approval_2" {
		t.Fatal("publication did not consume the exact approval", approvalID)
	}
	must(t, tx.QueryRow(ctx, `SELECT state,revision FROM supply.plugin_versions WHERE workspace_id='ws_plugin_a' AND plugin_id='example.search' AND version='1.0.0'`).Scan(&state, &revision))
	if state != "published" || revision != 2 {
		t.Fatal("PluginVersion publication rewrote revision or state", state, revision)
	}
	must(t, tx.Commit(ctx))

	// A published manifest cannot be updated or deleted; release history is not
	// rewritten by a later management operation.
	tx = beginWorkspaceTx(t, ctx, publisher, "ws_plugin_a")
	_, err = tx.Exec(ctx, `DELETE FROM supply.plugin_versions WHERE workspace_id='ws_plugin_a' AND plugin_id='example.search' AND version='1.0.0'`)
	if err == nil || pgErrorCode(err) != "23514" {
		t.Fatal("published PluginVersion was deletable", err)
	}
	_ = tx.Rollback(ctx)

	tx = beginWorkspaceTx(t, ctx, reviewer, "ws_plugin_a")
	var approvalState string
	must(t, tx.QueryRow(ctx, `SELECT state FROM governance.plugin_publication_approvals WHERE workspace_id='ws_plugin_a' AND id='plugin_approval_2'`).Scan(&approvalState))
	if approvalState != "consumed" {
		t.Fatal("approved publication was not consumed", approvalState)
	}
	var submittedEvents, approvedEvents, consumedEvents, publishedEvents int
	must(t, tx.QueryRow(ctx, `SELECT
	 count(*) FILTER (WHERE event_kind='approval_submitted'),
	 count(*) FILTER (WHERE event_kind='approval_approved'),
	 count(*) FILTER (WHERE event_kind='approval_consumed'),
	 count(*) FILTER (WHERE event_kind='publication_committed')
	 FROM governance.plugin_publication_audit_events
	 WHERE workspace_id='ws_plugin_a' AND approval_id='plugin_approval_2'`).Scan(&submittedEvents, &approvedEvents, &consumedEvents, &publishedEvents))
	if submittedEvents != 1 || approvedEvents != 1 || consumedEvents != 1 || publishedEvents != 1 {
		t.Fatal("Plugin publication audit sequence incomplete", submittedEvents, approvedEvents, consumedEvents, publishedEvents)
	}
	must(t, tx.Commit(ctx))

	// Reviewer RLS also hides approval history outside the selected Workspace.
	tx = beginWorkspaceTx(t, ctx, reviewer, "ws_plugin_b")
	must(t, tx.QueryRow(ctx, `SELECT count(*) FROM governance.plugin_publication_approvals WHERE workspace_id='ws_plugin_a'`).Scan(&crossCount))
	if crossCount != 0 {
		t.Fatal("reviewer RLS exposed another Workspace approval", crossCount)
	}
	must(t, tx.Commit(ctx))

	// Neither restricted role can bypass its intended half of maker/checker.
	tx = beginWorkspaceTx(t, ctx, publisher, "ws_plugin_a")
	_, err = tx.Exec(ctx, `SELECT supply.publish_plugin_version('ws_plugin_a','example.search','1.0.0',2,$1)`, publishAt)
	if err == nil || pgErrorCode(err) != "42501" {
		t.Fatal("publisher-manager obtained raw lifecycle function authority", err)
	}
	_ = tx.Rollback(ctx)
	tx = beginWorkspaceTx(t, ctx, reviewer, "ws_plugin_a")
	_, err = tx.Exec(ctx, `SELECT governance.publish_approved_plugin('ws_plugin_a','example.search','1.0.0','user_publisher',$1)`, publishAt)
	if err == nil || pgErrorCode(err) != "42501" {
		t.Fatal("governance reviewer obtained Publisher publication authority", err)
	}
	_ = tx.Rollback(ctx)
}
