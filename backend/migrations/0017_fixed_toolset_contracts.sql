-- Owners: catalog owns immutable ToolVersion contracts; distribution owns
-- Workspace-scoped Toolset publication aliases and fixed Connection selection.
-- Existing bindings remain MCP-hidden until explicitly republished with
-- mcp_exposed=true, a stable mcp_name and a fixed connection_id.

ALTER TABLE catalog.tool_versions
    ADD COLUMN title text NOT NULL DEFAULT '',
    ADD COLUMN description text NOT NULL DEFAULT '',
    ADD COLUMN input_schema jsonb NOT NULL DEFAULT '{"type":"object","additionalProperties":true}'::jsonb,
    ADD COLUMN output_schema jsonb NOT NULL DEFAULT '{"type":"object"}'::jsonb,
    ADD COLUMN side_effect text NOT NULL DEFAULT 'read_only',
    ADD COLUMN idempotency text NOT NULL DEFAULT 'safe_read',
    ADD COLUMN mcp_publishable boolean NOT NULL DEFAULT false;

UPDATE catalog.tool_versions SET title=tool_id WHERE title='';

ALTER TABLE catalog.tool_versions
    ADD CONSTRAINT tool_versions_title_check CHECK (length(title) BETWEEN 1 AND 200),
    ADD CONSTRAINT tool_versions_description_check CHECK (length(description) <= 4000),
    ADD CONSTRAINT tool_versions_input_schema_check CHECK (jsonb_typeof(input_schema)='object' AND input_schema->>'type'='object'),
    ADD CONSTRAINT tool_versions_output_schema_check CHECK (jsonb_typeof(output_schema)='object'),
    ADD CONSTRAINT tool_versions_side_effect_check CHECK (side_effect IN ('read_only','write')),
    ADD CONSTRAINT tool_versions_idempotency_check CHECK (idempotency IN ('safe_read','idempotent','unsafe'));

ALTER TABLE distribution.toolset_bindings
    ADD COLUMN connection_id text,
    ADD COLUMN mcp_name text,
    ADD COLUMN mcp_exposed boolean NOT NULL DEFAULT false,
    ADD CONSTRAINT toolset_bindings_connection_id_check CHECK (connection_id IS NULL OR connection_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    ADD CONSTRAINT toolset_bindings_mcp_name_check CHECK (mcp_name IS NULL OR mcp_name ~ '^[a-z][a-z0-9_]{0,63}$'),
    ADD CONSTRAINT toolset_bindings_mcp_exposure_check CHECK (NOT mcp_exposed OR (connection_id IS NOT NULL AND mcp_name IS NOT NULL));

CREATE UNIQUE INDEX toolset_mcp_name_unique
    ON distribution.toolset_bindings(workspace_id,toolset_version_id,mcp_name)
    WHERE mcp_exposed;

