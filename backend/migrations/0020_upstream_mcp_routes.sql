-- Owner: supply. A reviewed Catalog ToolVersion may bind to exactly one
-- discovered upstream Tool snapshot. Runtime call results are temporary
-- provider evidence keyed by the admitted Run and protected by Workspace RLS.

-- Upstream MCP descriptions commonly contain line breaks. 0019 deliberately
-- bounded their length but its generic control-character check was too strict
-- for ordinary human-readable Markdown. PostgreSQL text itself rejects NUL;
-- keep only the explicit length bound here rather than mutating 0019.
ALTER TABLE supply.mcp_tool_snapshots
    DROP CONSTRAINT mcp_tool_snapshots_title_check,
    DROP CONSTRAINT mcp_tool_snapshots_description_check,
    ADD CONSTRAINT mcp_tool_snapshot_title_length CHECK (char_length(title) BETWEEN 0 AND 200),
    ADD CONSTRAINT mcp_tool_snapshot_description_length CHECK (char_length(description) <= 4000);

CREATE TABLE supply.mcp_tool_routes (
    tool_version_id text PRIMARY KEY CHECK (tool_version_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    deployment_revision text NOT NULL REFERENCES supply.deployments(revision),
    upstream_tool_name text NOT NULL CHECK (upstream_tool_name ~ '^[A-Za-z0-9_.:-]{1,128}$'),
    snapshot_sha256 text NOT NULL CHECK (snapshot_sha256 ~ '^[a-f0-9]{64}$'),
    state text NOT NULL CHECK (state IN ('active','disabled')),
    created_at timestamptz NOT NULL,
    FOREIGN KEY (deployment_revision,upstream_tool_name,snapshot_sha256)
        REFERENCES supply.mcp_tool_snapshots(deployment_revision,tool_name,content_sha256)
);

CREATE TABLE supply.mcp_call_results (
    workspace_id text NOT NULL CHECK (workspace_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    run_id text NOT NULL CHECK (run_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    deployment_revision text NOT NULL REFERENCES supply.deployments(revision),
    submission_key text NOT NULL CHECK (char_length(submission_key) BETWEEN 8 AND 200),
    provider_id text NOT NULL CHECK (provider_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    provider_request_id text NOT NULL CHECK (char_length(provider_request_id) BETWEEN 1 AND 512),
    state text NOT NULL CHECK (state IN ('succeeded','failed')),
    result_json jsonb,
    error_code text,
    observed_at timestamptz NOT NULL,
    PRIMARY KEY (workspace_id,run_id,submission_key),
    UNIQUE (workspace_id,run_id,provider_request_id),
    CHECK (
        (state='succeeded' AND result_json IS NOT NULL AND error_code IS NULL)
        OR (state='failed' AND result_json IS NULL AND error_code ~ '^[A-Za-z0-9._:-]{1,128}$')
    )
);

ALTER TABLE supply.mcp_call_results ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply.mcp_call_results FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON supply.mcp_call_results
    USING (workspace_id=nullif(current_setting('mender.workspace_id',true),''))
    WITH CHECK (workspace_id=nullif(current_setting('mender.workspace_id',true),''));

REVOKE ALL ON supply.mcp_tool_routes,supply.mcp_call_results FROM PUBLIC;

COMMENT ON TABLE supply.mcp_tool_routes IS
    'Reviewed immutable ToolVersion -> exact upstream MCP discovery snapshot binding.';
COMMENT ON TABLE supply.mcp_call_results IS
    'Workspace-scoped durable result evidence for synchronous upstream MCP tools/call. It is reconciled into Execution Provider Result/Artifact; it is not a user-facing result store.';
