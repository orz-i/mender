//go:build integration

package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	governancepg "github.com/orz-i/mender/backend/internal/contexts/governance/adapters/outbound/postgres"
	governanceapp "github.com/orz-i/mender/backend/internal/contexts/governance/application"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	catalogmanagementpg "github.com/orz-i/mender/backend/internal/processes/catalogmanagement/adapters/outbound/postgres"
	"github.com/orz-i/mender/backend/migrations"
)

func exerciseCatalogManagementFoundation(t *testing.T, ctx context.Context, owner, runtime *pgxpool.Pool, runtimeDSN string) {
	t.Helper()
	manager, _ := openTemporaryRole(t, ctx, owner, runtimeDSN, "mender_catalog_manager_", migrations.GrantCatalogManager)
	reviewer, _ := openTemporaryRole(t, ctx, owner, runtimeDSN, "mender_governance_reviewer_", migrations.GrantGovernanceReviewer)
	policyManager, _ := openTemporaryRole(t, ctx, owner, runtimeDSN, "mender_gov_policy_", migrations.GrantGovernancePolicyManager)
	must(t, database.CatalogManagerRole(ctx, manager))
	must(t, database.GovernanceReviewerRole(ctx, reviewer))
	must(t, database.GovernancePolicyManagerRole(ctx, policyManager))
	if database.CatalogManagerRole(ctx, owner) == nil || database.CatalogManagerRole(ctx, runtime) == nil {
		t.Fatal("owner/runtime role accepted as catalog manager")
	}
	if database.GovernanceReviewerRole(ctx, owner) == nil || database.GovernanceReviewerRole(ctx, manager) == nil {
		t.Fatal("owner/catalog-manager role accepted as governance reviewer")
	}
	if database.GovernancePolicyManagerRole(ctx, owner) == nil || database.GovernancePolicyManagerRole(ctx, reviewer) == nil || database.GovernancePolicyManagerRole(ctx, manager) == nil {
		t.Fatal("owner/reviewer/catalog-manager role accepted as governance policy manager")
	}

	tx, err := manager.Begin(ctx)
	must(t, err)
	defer func() { _ = tx.Rollback(context.Background()) }()
	_, err = tx.Exec(ctx, "SELECT set_config('mender.workspace_id','ws_catalog_a',true)")
	must(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO catalog.tool_version_management(
	 workspace_id,tool_version_id,tool_id,version,provider_id,price_version_id,deployment_revision,title,description,input_schema,output_schema,side_effect,idempotency,mcp_publishable)
	 VALUES('ws_catalog_a','tv_catalog_a','tool_catalog','1.0.0','provider_catalog','price_catalog','deploy_catalog','Catalog Tool','draft','{"type":"object"}'::jsonb,'{"type":"object"}'::jsonb,'read_only','safe_read',true)`)
	must(t, err)
	_, err = tx.Exec(ctx, `UPDATE catalog.tool_version_management SET title='Catalog Tool Updated' WHERE workspace_id='ws_catalog_a' AND tool_version_id='tv_catalog_a'`)
	must(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO distribution.toolsets(workspace_id,id) VALUES('ws_catalog_a','set_catalog_draft')`)
	must(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO distribution.toolset_bindings(workspace_id,toolset_version_id,tool_id,tool_version_label,tool_version_id,budget_id,connection_id,mcp_name,mcp_exposed)
	 VALUES('ws_catalog_a','set_catalog_draft','tool_catalog','1.0.0','tv_catalog_a','budget_catalog','conn_catalog','catalog_tool',true)`)
	must(t, err)
	_, err = tx.Exec(ctx, `UPDATE distribution.toolset_bindings SET budget_id='budget_catalog_2' WHERE workspace_id='ws_catalog_a' AND toolset_version_id='set_catalog_draft' AND tool_version_id='tv_catalog_a'`)
	must(t, err)
	must(t, tx.Commit(ctx))

	if _, err = manager.Exec(ctx, `UPDATE catalog.tool_version_management SET state='published' WHERE workspace_id='ws_catalog_a' AND tool_version_id='tv_catalog_a'`); err == nil {
		t.Fatal("catalog manager obtained lifecycle-state mutation authority")
	}
	crossTx, err := manager.Begin(ctx)
	must(t, err)
	_, err = crossTx.Exec(ctx, "SELECT set_config('mender.workspace_id','ws_catalog_a',true)")
	must(t, err)
	if _, err = crossTx.Exec(ctx, `INSERT INTO catalog.tool_version_management(
	 workspace_id,tool_version_id,tool_id,version,provider_id,price_version_id,deployment_revision,title,input_schema,output_schema,side_effect,idempotency)
	 VALUES('ws_catalog_b','tv_catalog_b','tool_catalog','1.0.0','provider_catalog','price_catalog','deploy_catalog','Other Workspace','{"type":"object"}'::jsonb,'{"type":"object"}'::jsonb,'read_only','safe_read')`); err == nil {
		t.Fatal("catalog manager RLS accepted another workspace")
	}
	_ = crossTx.Rollback(ctx)

	at := time.Now().UTC().Truncate(time.Microsecond)
	_, err = owner.Exec(ctx, `INSERT INTO catalog.tool_versions(id,tool_id,version,provider_id,price_version_id,deployment_revision,title,description,input_schema,output_schema,side_effect,idempotency,mcp_publishable,state,published_at)
	 VALUES('tv_immutable','tool_immutable','1.0.0','provider_catalog','price_catalog','deploy_catalog','Immutable','','{"type":"object"}'::jsonb,'{"type":"object"}'::jsonb,'read_only','safe_read',false,'published',$1)`, at)
	must(t, err)
	if _, err = owner.Exec(ctx, `UPDATE catalog.tool_versions SET title='Changed' WHERE id='tv_immutable'`); err == nil {
		t.Fatal("published ToolVersion facts were mutable")
	}
	_, err = owner.Exec(ctx, `UPDATE catalog.tool_versions SET state='retired' WHERE id='tv_immutable'`)
	must(t, err)

	ownerTx, err := owner.Begin(ctx)
	must(t, err)
	defer func() { _ = ownerTx.Rollback(context.Background()) }()
	_, err = ownerTx.Exec(ctx, "SELECT set_config('mender.workspace_id','ws_catalog_a',true)")
	must(t, err)
	_, err = ownerTx.Exec(ctx, `UPDATE distribution.toolset_bindings SET state='published',published_at=$1 WHERE workspace_id='ws_catalog_a' AND toolset_version_id='set_catalog_draft'`, at)
	if err != nil {
		t.Fatal("legacy binding publication fixture failed", err)
	}
	_, err = ownerTx.Exec(ctx, `UPDATE distribution.toolsets SET state='published',published_at=$1,updated_at=$1 WHERE workspace_id='ws_catalog_a' AND id='set_catalog_draft'`, at)
	if err != nil {
		t.Fatal("legacy parent Toolset publication fixture failed", err)
	}
	must(t, ownerTx.Commit(ctx))

	tx, err = manager.Begin(ctx)
	must(t, err)
	defer func() { _ = tx.Rollback(context.Background()) }()
	_, err = tx.Exec(ctx, "SELECT set_config('mender.workspace_id','ws_catalog_a',true)")
	must(t, err)
	if _, err = tx.Exec(ctx, `UPDATE distribution.toolset_bindings SET budget_id='budget_mutated' WHERE workspace_id='ws_catalog_a' AND toolset_version_id='set_catalog_draft'`); err == nil {
		t.Fatal("published Toolset binding facts were mutable")
	}
	_ = tx.Rollback(ctx)

	if _, err = runtime.Exec(ctx, `SELECT * FROM catalog.tool_version_management`); err == nil {
		t.Fatal("runtime role gained Human catalog-management reads")
	}

	workflowAt := at.Add(time.Minute)
	_, err = owner.Exec(ctx, `INSERT INTO commerce.price_versions(id,tool_version_id,currency,reserve_micro,starts_at,ends_at,active,charge_micro,billing_policy)
	 VALUES('price_catalog_publish','tv_catalog_publish','USD',125000,$1,$2,true,125000,'fixed_success_only')`, workflowAt.Add(-time.Hour), workflowAt.Add(time.Hour))
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO connections.connections(workspace_id,id,provider_id,credential_version_ref,state,revision,created_at,expires_at)
	 VALUES('ws_catalog_publish','conn_catalog_publish','provider_catalog_publish','secret_catalog_publish','active',1,$1,$2)`, workflowAt.Add(-time.Hour), workflowAt.Add(time.Hour))
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO commerce.budget_periods(workspace_id,budget_id,period_id,currency,starts_at,ends_at,limit_micro)
	 VALUES('ws_catalog_publish','budget_catalog_publish','period_catalog_publish','USD',$1,$2,1000000)`, workflowAt.Add(-time.Hour), workflowAt.Add(time.Hour))
	must(t, err)

	tx, err = manager.Begin(ctx)
	must(t, err)
	_, err = tx.Exec(ctx, "SELECT set_config('mender.workspace_id','ws_catalog_publish',true)")
	must(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO catalog.tool_version_management(
	 workspace_id,tool_version_id,tool_id,version,provider_id,price_version_id,deployment_revision,title,description,input_schema,output_schema,side_effect,idempotency,mcp_publishable)
	 VALUES('ws_catalog_publish','tv_catalog_publish','tool_catalog_publish','1.0.0','provider_catalog_publish','price_catalog_publish','deploy_catalog_publish','Published through workflow','',
	 '{"type":"object","additionalProperties":false}'::jsonb,'{"type":"object"}'::jsonb,'read_only','safe_read',true)`)
	must(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO catalog.tool_version_management(
	 workspace_id,tool_version_id,tool_id,version,provider_id,price_version_id,deployment_revision,title,description,input_schema,output_schema,side_effect,idempotency,mcp_publishable)
	 VALUES('ws_catalog_publish','tv_catalog_unsafe','tool_catalog_unsafe','1.0.0','provider_catalog_publish','price_catalog_publish','deploy_catalog_unsafe','Unsafe write draft','',
	 '{"type":"object"}'::jsonb,'{"type":"object"}'::jsonb,'write','unsafe',true)`)
	must(t, err)
	var issueCount int
	must(t, tx.QueryRow(ctx, `SELECT count(*) FROM catalog.tool_version_publish_issues('ws_catalog_publish','tv_catalog_publish',$1)`, workflowAt).Scan(&issueCount))
	if issueCount != 0 {
		t.Fatal("valid ToolVersion preflight unexpectedly failed", issueCount)
	}
	must(t, tx.Commit(ctx))

	policyTx, err := policyManager.Begin(ctx)
	must(t, err)
	_, err = policyTx.Exec(ctx, "SELECT set_config('mender.workspace_id','ws_catalog_publish',true)")
	must(t, err)
	if _, err = policyTx.Exec(ctx, `SELECT * FROM catalog.tool_version_management LIMIT 1`); err == nil {
		t.Fatal("governance policy manager gained Catalog read authority")
	}
	_ = policyTx.Rollback(ctx)
	policyTx, err = policyManager.Begin(ctx)
	must(t, err)
	_, err = policyTx.Exec(ctx, "SELECT set_config('mender.workspace_id','ws_catalog_publish',true)")
	must(t, err)
	var policyRevision int64
	must(t, policyTx.QueryRow(ctx, `SELECT governance.create_catalog_publication_policy('ws_catalog_publish','policy_allow_v1','policy_admin','critical',false,false,$1)`, workflowAt).Scan(&policyRevision))
	if policyRevision != 1 {
		t.Fatal("unexpected first policy revision", policyRevision)
	}
	_, err = policyTx.Exec(ctx, `SELECT governance.activate_catalog_publication_policy('ws_catalog_publish','policy_allow_v1','policy_admin',$1)`, workflowAt)
	must(t, err)
	if _, err = policyTx.Exec(ctx, `UPDATE governance.catalog_publication_policy_revisions SET max_risk_level='low' WHERE workspace_id='ws_catalog_publish' AND id='policy_allow_v1'`); err == nil {
		t.Fatal("policy manager obtained direct policy mutation authority")
	}
	_ = policyTx.Rollback(ctx)
	policyTx, err = policyManager.Begin(ctx)
	must(t, err)
	_, err = policyTx.Exec(ctx, "SELECT set_config('mender.workspace_id','ws_catalog_publish',true)")
	must(t, err)
	must(t, policyTx.QueryRow(ctx, `SELECT governance.create_catalog_publication_policy('ws_catalog_publish','policy_allow_v1','policy_admin','critical',false,false,$1)`, workflowAt).Scan(&policyRevision))
	_, err = policyTx.Exec(ctx, `SELECT governance.activate_catalog_publication_policy('ws_catalog_publish','policy_allow_v1','policy_admin',$1)`, workflowAt)
	must(t, err)
	must(t, policyTx.Commit(ctx))

	policyEvalTx, err := owner.Begin(ctx)
	must(t, err)
	_, err = policyEvalTx.Exec(ctx, "SELECT set_config('mender.workspace_id','ws_catalog_publish',true)")
	must(t, err)
	var decisionSeq int64
	must(t, policyEvalTx.QueryRow(ctx, `SELECT governance.evaluate_catalog_publication_policy('ws_catalog_publish','tool_version','tv_catalog_publish',$1)`, workflowAt).Scan(&decisionSeq))
	must(t, policyEvalTx.Commit(ctx))
	var riskLevel, outcome string
	var reasonCodes []string
	must(t, owner.QueryRow(ctx, `SELECT risk_level,outcome,reason_codes FROM governance.catalog_publication_policy_decisions WHERE sequence=$1`, decisionSeq).Scan(&riskLevel, &outcome, &reasonCodes))
	if riskLevel != "low" || outcome != "allow" || len(reasonCodes) != 2 || reasonCodes[1] != "within_risk_ceiling" {
		t.Fatal("safe read policy decision drift", riskLevel, outcome, reasonCodes)
	}
	tx, err = manager.Begin(ctx)
	must(t, err)
	_, err = tx.Exec(ctx, "SELECT set_config('mender.workspace_id','ws_catalog_publish',true)")
	must(t, err)
	if _, err = tx.Exec(ctx, `UPDATE governance.catalog_publication_policy_decisions SET outcome='deny' WHERE sequence=$1`, decisionSeq); err == nil {
		t.Fatal("catalog manager obtained policy decision mutation authority")
	}
	_ = tx.Rollback(ctx)
	if _, err = owner.Exec(ctx, `UPDATE governance.catalog_publication_policy_decisions SET outcome='deny' WHERE sequence=$1`, decisionSeq); err == nil {
		t.Fatal("immutable policy decision accepted update")
	}

	policyTx, err = policyManager.Begin(ctx)
	must(t, err)
	_, err = policyTx.Exec(ctx, "SELECT set_config('mender.workspace_id','ws_catalog_publish',true)")
	must(t, err)
	must(t, policyTx.QueryRow(ctx, `SELECT governance.create_catalog_publication_policy('ws_catalog_publish','policy_strict_v2','policy_admin','high',true,true,$1)`, workflowAt.Add(time.Second)).Scan(&policyRevision))
	if policyRevision != 2 {
		t.Fatal("policy revision did not advance", policyRevision)
	}
	must(t, policyTx.Commit(ctx))

	tx, err = manager.Begin(ctx)
	must(t, err)
	_, err = tx.Exec(ctx, "SELECT set_config('mender.workspace_id','ws_catalog_publish',true)")
	must(t, err)
	_, err = tx.Exec(ctx, `SELECT governance.submit_catalog_publication('ws_catalog_publish','approval_policy_old','tool_version','tv_catalog_publish','maker_catalog',$1,$2)`, workflowAt.Add(time.Second), workflowAt.Add(time.Hour))
	must(t, err)
	must(t, tx.Commit(ctx))

	policyTx, err = policyManager.Begin(ctx)
	must(t, err)
	_, err = policyTx.Exec(ctx, "SELECT set_config('mender.workspace_id','ws_catalog_publish',true)")
	must(t, err)
	_, err = policyTx.Exec(ctx, `SELECT governance.activate_catalog_publication_policy('ws_catalog_publish','policy_strict_v2','policy_admin',$1)`, workflowAt.Add(2*time.Second))
	must(t, err)
	must(t, policyTx.Commit(ctx))
	var approvalState, expirationReason string
	must(t, owner.QueryRow(ctx, `SELECT state FROM governance.catalog_publication_approvals WHERE workspace_id='ws_catalog_publish' AND id='approval_policy_old'`).Scan(&approvalState))
	if approvalState != "expired" {
		t.Fatal("policy activation did not invalidate active approval", approvalState)
	}
	must(t, owner.QueryRow(ctx, `SELECT reason_code FROM governance.catalog_publication_audit_events WHERE workspace_id='ws_catalog_publish' AND approval_id='approval_policy_old' AND event_kind='approval_expired' ORDER BY sequence DESC LIMIT 1`).Scan(&expirationReason))
	if expirationReason != "policy_revision_changed" {
		t.Fatal("policy activation audit reason drift", expirationReason)
	}

	policyEvalTx, err = owner.Begin(ctx)
	must(t, err)
	_, err = policyEvalTx.Exec(ctx, "SELECT set_config('mender.workspace_id','ws_catalog_publish',true)")
	must(t, err)
	must(t, policyEvalTx.QueryRow(ctx, `SELECT governance.evaluate_catalog_publication_policy('ws_catalog_publish','tool_version','tv_catalog_unsafe',$1)`, workflowAt.Add(3*time.Second)).Scan(&decisionSeq))
	must(t, policyEvalTx.Commit(ctx))
	must(t, owner.QueryRow(ctx, `SELECT risk_level,outcome,reason_codes FROM governance.catalog_publication_policy_decisions WHERE sequence=$1`, decisionSeq).Scan(&riskLevel, &outcome, &reasonCodes))
	if riskLevel != "critical" || outcome != "deny" || len(reasonCodes) < 2 {
		t.Fatal("unsafe write policy decision did not deny", riskLevel, outcome, reasonCodes)
	}

	tx, err = manager.Begin(ctx)
	must(t, err)
	_, err = tx.Exec(ctx, "SELECT set_config('mender.workspace_id','ws_catalog_publish',true)")
	must(t, err)
	_, err = tx.Exec(ctx, `SELECT governance.submit_catalog_publication('ws_catalog_publish','approval_tv_1','tool_version','tv_catalog_publish','maker_catalog',$1,$2)`, workflowAt, workflowAt.Add(time.Hour))
	must(t, err)
	must(t, tx.Commit(ctx))

	// Reviewer authority is function-only and maker/checker is enforced even if
	// a caller tries to pass the requester's identity as the reviewer.
	reviewTx, err := reviewer.Begin(ctx)
	must(t, err)
	_, err = reviewTx.Exec(ctx, "SELECT set_config('mender.workspace_id','ws_catalog_publish',true)")
	must(t, err)
	if _, err = reviewTx.Exec(ctx, `UPDATE governance.catalog_publication_approvals SET state='approved' WHERE id='approval_tv_1'`); err == nil {
		t.Fatal("governance reviewer obtained direct approval-table mutation")
	}
	_ = reviewTx.Rollback(ctx)
	reviewTx, err = reviewer.Begin(ctx)
	must(t, err)
	_, err = reviewTx.Exec(ctx, "SELECT set_config('mender.workspace_id','ws_catalog_publish',true)")
	must(t, err)
	if _, err = reviewTx.Exec(ctx, `SELECT governance.approve_catalog_publication('ws_catalog_publish','approval_tv_1','maker_catalog',$1,'self')`, workflowAt.Add(time.Second)); err == nil {
		t.Fatal("maker approved own publication")
	}
	_ = reviewTx.Rollback(ctx)
	reviewTx, err = reviewer.Begin(ctx)
	must(t, err)
	_, err = reviewTx.Exec(ctx, "SELECT set_config('mender.workspace_id','ws_catalog_publish',true)")
	must(t, err)
	_, err = reviewTx.Exec(ctx, `SELECT governance.approve_catalog_publication('ws_catalog_publish','approval_tv_1','reviewer_catalog',$1,'reviewed')`, workflowAt.Add(time.Second))
	must(t, err)
	must(t, reviewTx.Commit(ctx))

	// Exact revision binding prevents reusing an approval after the draft changes.
	tx, err = manager.Begin(ctx)
	must(t, err)
	_, err = tx.Exec(ctx, "SELECT set_config('mender.workspace_id','ws_catalog_publish',true)")
	must(t, err)
	_, err = tx.Exec(ctx, `UPDATE catalog.tool_version_management SET title='Changed after approval' WHERE workspace_id='ws_catalog_publish' AND tool_version_id='tv_catalog_publish'`)
	must(t, err)
	if _, err = tx.Exec(ctx, `SELECT catalog.publish_tool_version('ws_catalog_publish','tv_catalog_publish',$1)`, workflowAt.Add(2*time.Second)); err == nil {
		t.Fatal("stale approval published changed ToolVersion revision")
	}
	_ = tx.Rollback(ctx)

	tx, err = manager.Begin(ctx)
	must(t, err)
	_, err = tx.Exec(ctx, "SELECT set_config('mender.workspace_id','ws_catalog_publish',true)")
	must(t, err)
	_, err = tx.Exec(ctx, `UPDATE catalog.tool_version_management SET title='Changed after approval' WHERE workspace_id='ws_catalog_publish' AND tool_version_id='tv_catalog_publish'`)
	must(t, err)
	_, err = tx.Exec(ctx, `SELECT governance.submit_catalog_publication('ws_catalog_publish','approval_tv_2','tool_version','tv_catalog_publish','maker_catalog',$1,$2)`, workflowAt.Add(2*time.Second), workflowAt.Add(time.Hour))
	must(t, err)
	must(t, tx.Commit(ctx))
	reviewTx, err = reviewer.Begin(ctx)
	must(t, err)
	_, err = reviewTx.Exec(ctx, "SELECT set_config('mender.workspace_id','ws_catalog_publish',true)")
	must(t, err)
	_, err = reviewTx.Exec(ctx, `SELECT governance.approve_catalog_publication('ws_catalog_publish','approval_tv_2','reviewer_catalog',$1,'reviewed revision 2')`, workflowAt.Add(3*time.Second))
	must(t, err)
	must(t, reviewTx.Commit(ctx))
	tx, err = manager.Begin(ctx)
	must(t, err)
	_, err = tx.Exec(ctx, "SELECT set_config('mender.workspace_id','ws_catalog_publish',true)")
	must(t, err)
	_, err = tx.Exec(ctx, `SELECT catalog.publish_tool_version('ws_catalog_publish','tv_catalog_publish',$1)`, workflowAt.Add(4*time.Second))
	must(t, err)
	var state string
	must(t, tx.QueryRow(ctx, `SELECT state FROM catalog.tool_version_management WHERE workspace_id='ws_catalog_publish' AND tool_version_id='tv_catalog_publish'`).Scan(&state))
	if state != "published" {
		t.Fatal("ToolVersion workflow did not publish management fact", state)
	}
	_, err = tx.Exec(ctx, `INSERT INTO distribution.toolsets(workspace_id,id) VALUES('ws_catalog_publish','set_catalog_publish')`)
	if err != nil {
		t.Fatal("governed Toolset draft creation failed", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO distribution.toolset_bindings(workspace_id,toolset_version_id,tool_id,tool_version_label,tool_version_id,budget_id,connection_id,mcp_name,mcp_exposed)
	 VALUES('ws_catalog_publish','set_catalog_publish','tool_catalog_publish','1.0.0','tv_catalog_publish','budget_catalog_publish','conn_catalog_publish','catalog_publish',true)`)
	must(t, err)
	must(t, tx.QueryRow(ctx, `SELECT count(*) FROM distribution.toolset_publish_issues('ws_catalog_publish','set_catalog_publish',$1)`, workflowAt).Scan(&issueCount))
	if issueCount != 0 {
		t.Fatal("valid Toolset preflight unexpectedly failed", issueCount)
	}
	must(t, tx.Commit(ctx))
	catalogRepo := catalogmanagementpg.New(manager)
	approval, err := catalogRepo.SubmitPublication(ctx, "ws_catalog_publish", "approval_set_1", "toolset", "set_catalog_publish", "maker_catalog", workflowAt.Add(5*time.Second), workflowAt.Add(time.Hour))
	must(t, err)
	if approval.TargetRevision < 1 || approval.State != "pending" || approval.RequesterUserID != "maker_catalog" {
		t.Fatal("catalog management repository did not return exact approval request", approval)
	}
	reviewRepo := governancepg.NewPublicationReview(reviewer)
	reviewed, err := reviewRepo.ApprovePublication(ctx, "ws_catalog_publish", "approval_set_1", "reviewer_catalog", "toolset reviewed", workflowAt.Add(6*time.Second))
	must(t, err)
	if reviewed.State != "approved" || reviewed.ReviewerUserID != "reviewer_catalog" {
		t.Fatal("governance repository did not return reviewed approval", reviewed)
	}

	_, err = owner.Exec(ctx, `UPDATE connections.connections SET state='revoked',revision=revision+1 WHERE workspace_id='ws_catalog_publish' AND id='conn_catalog_publish'`)
	must(t, err)
	tx, err = manager.Begin(ctx)
	must(t, err)
	_, err = tx.Exec(ctx, "SELECT set_config('mender.workspace_id','ws_catalog_publish',true)")
	must(t, err)
	var issueCode string
	must(t, tx.QueryRow(ctx, `SELECT code FROM distribution.toolset_publish_issues('ws_catalog_publish','set_catalog_publish',$1) WHERE code='connection_unavailable' LIMIT 1`, workflowAt).Scan(&issueCode))
	if issueCode != "connection_unavailable" {
		t.Fatal("revoked Connection did not invalidate Toolset preflight", issueCode)
	}
	if _, err = tx.Exec(ctx, `SELECT distribution.publish_toolset('ws_catalog_publish','set_catalog_publish',$1)`, workflowAt); err == nil {
		t.Fatal("publish bypassed authoritative preflight after Connection revocation")
	}
	_ = tx.Rollback(ctx)

	_, err = owner.Exec(ctx, `UPDATE connections.connections SET state='active',revision=revision+1 WHERE workspace_id='ws_catalog_publish' AND id='conn_catalog_publish'`)
	must(t, err)
	tx, err = manager.Begin(ctx)
	must(t, err)
	_, err = tx.Exec(ctx, "SELECT set_config('mender.workspace_id','ws_catalog_publish',true)")
	must(t, err)
	_, err = tx.Exec(ctx, `SELECT distribution.publish_toolset('ws_catalog_publish','set_catalog_publish',$1)`, workflowAt.Add(7*time.Second))
	if err != nil {
		t.Fatal("approved governed Toolset publication failed", err)
	}
	must(t, tx.Commit(ctx))
	must(t, owner.QueryRow(ctx, `SELECT state FROM governance.catalog_publication_approvals WHERE workspace_id='ws_catalog_publish' AND id='approval_set_1'`).Scan(&approvalState))
	if approvalState != "consumed" {
		t.Fatal("successful Toolset publish did not consume approval", approvalState)
	}
	snapshot, err := catalogRepo.Snapshot(ctx, "ws_catalog_publish", workflowAt.Add(8*time.Second))
	must(t, err)
	if len(snapshot.Approvals) < 2 || snapshot.ToolVersions[0].Revision < 1 || snapshot.Toolsets[0].Revision < 1 {
		t.Fatal("Catalog snapshot omitted revision-bound publication governance facts", len(snapshot.Approvals))
	}
	reviewList, err := reviewRepo.ListPublicationApprovals(ctx, "ws_catalog_publish", workflowAt.Add(8*time.Second))
	must(t, err)
	if len(reviewList) < 2 {
		t.Fatal("Governance reviewer repository omitted workspace approvals", reviewList)
	}

	tx, err = manager.Begin(ctx)
	must(t, err)
	_, err = tx.Exec(ctx, "SELECT set_config('mender.workspace_id','ws_catalog_publish',true)")
	must(t, err)
	if _, err = tx.Exec(ctx, `SELECT catalog.retire_tool_version('ws_catalog_publish','tv_catalog_publish',$1)`, workflowAt.Add(8*time.Second)); err == nil {
		t.Fatal("ToolVersion retired while still bound by a published Toolset")
	}
	_ = tx.Rollback(ctx)

	tx, err = manager.Begin(ctx)
	must(t, err)
	_, err = tx.Exec(ctx, "SELECT set_config('mender.workspace_id','ws_catalog_publish',true)")
	must(t, err)
	_, err = tx.Exec(ctx, `SELECT distribution.retire_toolset('ws_catalog_publish','set_catalog_publish',$1)`, workflowAt.Add(9*time.Second))
	if err != nil {
		t.Fatal("governed Toolset retirement failed", err)
	}
	_, err = tx.Exec(ctx, `SELECT catalog.retire_tool_version('ws_catalog_publish','tv_catalog_publish',$1)`, workflowAt.Add(10*time.Second))
	must(t, err)
	must(t, tx.Commit(ctx))
	must(t, owner.QueryRow(ctx, `SELECT state FROM catalog.tool_versions WHERE id='tv_catalog_publish'`).Scan(&state))
	if state != "retired" {
		t.Fatal("retired ToolVersion remained runtime-callable", state)
	}

	// Expiry is persisted as a governance fact when a reviewer observes an
	// elapsed approval. The reviewed function returns without granting approval.
	_, err = owner.Exec(ctx, `INSERT INTO commerce.price_versions(id,tool_version_id,currency,reserve_micro,starts_at,ends_at,active,charge_micro,billing_policy)
	 VALUES('price_catalog_expire','tv_catalog_expire','USD',1,$1,$2,true,1,'fixed_success_only')`, workflowAt.Add(-time.Hour), workflowAt.Add(time.Hour))
	must(t, err)
	tx, err = manager.Begin(ctx)
	must(t, err)
	_, err = tx.Exec(ctx, "SELECT set_config('mender.workspace_id','ws_catalog_publish',true)")
	must(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO catalog.tool_version_management(
	 workspace_id,tool_version_id,tool_id,version,provider_id,price_version_id,deployment_revision,title,description,input_schema,output_schema,side_effect,idempotency,mcp_publishable)
	 VALUES('ws_catalog_publish','tv_catalog_expire','tool_catalog_expire','1.0.0','provider_catalog_publish','price_catalog_expire','deploy_catalog_expire','Expiry audit','',
	 '{"type":"object"}'::jsonb,'{"type":"object"}'::jsonb,'read_only','safe_read',false)`)
	must(t, err)
	_, err = tx.Exec(ctx, `SELECT governance.submit_catalog_publication('ws_catalog_publish','approval_expire_1','tool_version','tv_catalog_expire','maker_catalog',$1,$2)`, workflowAt.Add(11*time.Second), workflowAt.Add(12*time.Second))
	must(t, err)
	must(t, tx.Commit(ctx))
	reviewTx, err = reviewer.Begin(ctx)
	must(t, err)
	_, err = reviewTx.Exec(ctx, "SELECT set_config('mender.workspace_id','ws_catalog_publish',true)")
	must(t, err)
	_, err = reviewTx.Exec(ctx, `SELECT governance.approve_catalog_publication('ws_catalog_publish','approval_expire_1','reviewer_catalog',$1,'too late')`, workflowAt.Add(13*time.Second))
	must(t, err)
	must(t, reviewTx.Commit(ctx))
	must(t, owner.QueryRow(ctx, `SELECT state FROM governance.catalog_publication_approvals WHERE workspace_id='ws_catalog_publish' AND id='approval_expire_1'`).Scan(&approvalState))
	if approvalState != "expired" {
		t.Fatal("elapsed publication approval was not durably expired", approvalState)
	}

	// Catalog management can cause audited transitions but cannot read or write
	// the append-only history directly.
	tx, err = manager.Begin(ctx)
	must(t, err)
	_, err = tx.Exec(ctx, "SELECT set_config('mender.workspace_id','ws_catalog_publish',true)")
	must(t, err)
	if _, err = tx.Exec(ctx, `SELECT sequence FROM governance.catalog_publication_audit_events LIMIT 1`); err == nil {
		t.Fatal("catalog manager obtained governance audit read authority")
	}
	_ = tx.Rollback(ctx)

	reviewTx, err = reviewer.Begin(ctx)
	must(t, err)
	_, err = reviewTx.Exec(ctx, "SELECT set_config('mender.workspace_id','ws_catalog_publish',true)")
	must(t, err)
	if _, err = reviewTx.Exec(ctx, `INSERT INTO governance.catalog_publication_audit_events(workspace_id,approval_id,target_kind,target_id,target_revision,event_kind,occurred_at) VALUES('ws_catalog_publish','forged','tool_version','tv_catalog_publish',1,'approval_consumed',$1)`, workflowAt); err == nil {
		t.Fatal("governance reviewer obtained direct audit append authority")
	}
	_ = reviewTx.Rollback(ctx)

	ownerAuditTx, err := owner.Begin(ctx)
	must(t, err)
	_, err = ownerAuditTx.Exec(ctx, "SELECT set_config('mender.workspace_id','ws_catalog_publish',true)")
	must(t, err)
	if _, err = ownerAuditTx.Exec(ctx, `UPDATE governance.catalog_publication_audit_events SET note='tampered' WHERE workspace_id='ws_catalog_publish'`); err == nil {
		t.Fatal("append-only publication audit accepted UPDATE")
	}
	_ = ownerAuditTx.Rollback(ctx)

	reviewTx, err = reviewer.Begin(ctx)
	must(t, err)
	_, err = reviewTx.Exec(ctx, "SELECT set_config('mender.workspace_id','ws_catalog_publish',true)")
	must(t, err)
	var tv1History, tv2History, setHistory, expiryHistory string
	must(t, reviewTx.QueryRow(ctx, `SELECT string_agg(event_kind||':'||reason_code,',' ORDER BY sequence) FROM governance.catalog_publication_audit_events WHERE workspace_id='ws_catalog_publish' AND approval_id='approval_tv_1'`).Scan(&tv1History))
	must(t, reviewTx.QueryRow(ctx, `SELECT string_agg(event_kind||':'||reason_code,',' ORDER BY sequence) FROM governance.catalog_publication_audit_events WHERE workspace_id='ws_catalog_publish' AND approval_id='approval_tv_2'`).Scan(&tv2History))
	must(t, reviewTx.QueryRow(ctx, `SELECT string_agg(event_kind||':'||reason_code,',' ORDER BY sequence) FROM governance.catalog_publication_audit_events WHERE workspace_id='ws_catalog_publish' AND approval_id='approval_set_1'`).Scan(&setHistory))
	must(t, reviewTx.QueryRow(ctx, `SELECT string_agg(event_kind||':'||reason_code,',' ORDER BY sequence) FROM governance.catalog_publication_audit_events WHERE workspace_id='ws_catalog_publish' AND approval_id='approval_expire_1'`).Scan(&expiryHistory))
	if tv1History != "approval_submitted:,approval_approved:,approval_expired:revision_drift" {
		t.Fatal("ToolVersion stale approval audit history mismatch", tv1History)
	}
	if tv2History != "approval_submitted:,approval_approved:,approval_consumed:,publication_committed:" {
		t.Fatal("ToolVersion publication audit history mismatch", tv2History)
	}
	if setHistory != "approval_submitted:,approval_approved:,approval_consumed:,publication_committed:" {
		t.Fatal("Toolset publication audit history mismatch", setHistory)
	}
	if expiryHistory != "approval_submitted:,approval_expired:ttl_elapsed" {
		t.Fatal("elapsed approval audit history mismatch", expiryHistory)
	}
	var staleRevision, observedRevision int64
	must(t, reviewTx.QueryRow(ctx, `SELECT target_revision,observed_revision FROM governance.catalog_publication_audit_events WHERE workspace_id='ws_catalog_publish' AND approval_id='approval_tv_1' AND event_kind='approval_expired'`).Scan(&staleRevision, &observedRevision))
	if staleRevision != 1 || observedRevision != 2 {
		t.Fatal("revision drift audit lost exact revisions", staleRevision, observedRevision)
	}
	var retiredCount int
	must(t, reviewTx.QueryRow(ctx, `SELECT count(*) FROM governance.catalog_publication_audit_events WHERE workspace_id='ws_catalog_publish' AND event_kind='publication_retired' AND target_id IN ('set_catalog_publish','tv_catalog_publish')`).Scan(&retiredCount))
	if retiredCount != 2 {
		t.Fatal("publication retirement audit facts missing", retiredCount)
	}
	must(t, reviewTx.Commit(ctx))

	historyPage, err := reviewRepo.ListPublicationHistory(ctx, "ws_catalog_publish", governanceapp.PublicationHistoryFilter{TargetKind: "tool_version", TargetID: "tv_catalog_publish", Limit: 2})
	must(t, err)
	if len(historyPage.Events) != 2 || historyPage.NextBeforeSequence < 1 || historyPage.Events[0].Sequence <= historyPage.Events[1].Sequence {
		t.Fatal("publication history first page did not preserve bounded descending cursor", historyPage)
	}
	historyNext, err := reviewRepo.ListPublicationHistory(ctx, "ws_catalog_publish", governanceapp.PublicationHistoryFilter{TargetKind: "tool_version", TargetID: "tv_catalog_publish", BeforeSequence: historyPage.NextBeforeSequence, Limit: 2})
	must(t, err)
	if len(historyNext.Events) == 0 || historyNext.Events[0].Sequence >= historyPage.NextBeforeSequence {
		t.Fatal("publication history cursor repeated or reordered events", historyNext)
	}
	expiryPage, err := reviewRepo.ListPublicationHistory(ctx, "ws_catalog_publish", governanceapp.PublicationHistoryFilter{ApprovalID: "approval_expire_1", EventKind: "approval_expired", Limit: 10})
	must(t, err)
	if len(expiryPage.Events) != 1 || expiryPage.Events[0].ReasonCode != "ttl_elapsed" || expiryPage.Events[0].ApprovalID != "approval_expire_1" {
		t.Fatal("publication history filters returned incorrect audit facts", expiryPage)
	}
	crossHistory, err := reviewRepo.ListPublicationHistory(ctx, "ws_catalog_other", governanceapp.PublicationHistoryFilter{Limit: 10})
	must(t, err)
	if len(crossHistory.Events) != 0 {
		t.Fatal("publication history crossed Workspace RLS", crossHistory.Events)
	}
}
