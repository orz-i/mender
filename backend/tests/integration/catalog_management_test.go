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

func exerciseCatalogManagementFoundation(t *testing.T, ctx context.Context, owner, runtime *pgxpool.Pool, runtimeDSN string) {
	t.Helper()
	manager, _ := openTemporaryRole(t, ctx, owner, runtimeDSN, "mender_catalog_manager_", migrations.GrantCatalogManager)
	must(t, database.CatalogManagerRole(ctx, manager))
	if database.CatalogManagerRole(ctx, owner) == nil || database.CatalogManagerRole(ctx, runtime) == nil {
		t.Fatal("owner/runtime role accepted as catalog manager")
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
	must(t, err)
	_, err = ownerTx.Exec(ctx, `UPDATE distribution.toolsets SET state='published',published_at=$1,updated_at=$1 WHERE workspace_id='ws_catalog_a' AND id='set_catalog_draft'`, at)
	must(t, err)
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
}
