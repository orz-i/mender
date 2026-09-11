-- Owner: supply. Reviewed remote MCP server descriptors and immutable discovery
-- snapshots. This migration does not enable network access or publish Catalog
-- ToolVersions; it only records the server-owned contract needed by a later
-- outbound MCP adapter.

ALTER TABLE supply.deployments
    DROP CONSTRAINT deployments_transport_kind_check,
    DROP CONSTRAINT deployments_idempotency_header_check,
    ALTER COLUMN idempotency_header DROP NOT NULL,
    ADD COLUMN mcp_protocol_version text,
    ADD COLUMN mcp_stateless boolean;

ALTER TABLE supply.deployments
    ADD CONSTRAINT supply_transport_kind CHECK (transport_kind IN ('http','mcp_streamable_http')),
    ADD CONSTRAINT supply_transport_contract CHECK (
        (
            transport_kind='http'
            AND idempotency_header ~ '^[A-Za-z0-9-]{1,128}$'
            AND mcp_protocol_version IS NULL
            AND mcp_stateless IS NULL
        )
        OR (
            transport_kind='mcp_streamable_http'
            AND idempotency_header IS NULL
            AND mcp_protocol_version='2026-07-28'
            AND mcp_stateless IS TRUE
            AND status_endpoint_url IS NULL
            AND status_http_method IS NULL
            AND cancel_endpoint_url IS NULL
            AND cancel_http_method IS NULL
        )
    );

CREATE TABLE supply.mcp_tool_snapshots (
    deployment_revision text NOT NULL REFERENCES supply.deployments(revision),
    tool_name text NOT NULL CHECK (tool_name ~ '^[A-Za-z0-9_.:-]{1,128}$'),
    title text NOT NULL CHECK (char_length(title) BETWEEN 0 AND 200 AND title !~ '[[:cntrl:]]'),
    description text NOT NULL CHECK (char_length(description) <= 4000 AND description !~ '[[:cntrl:]]'),
    input_schema jsonb NOT NULL CHECK (jsonb_typeof(input_schema)='object'),
    output_schema jsonb CHECK (output_schema IS NULL OR jsonb_typeof(output_schema)='object'),
    annotations jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(annotations)='object'),
    content_sha256 text NOT NULL CHECK (content_sha256 ~ '^[a-f0-9]{64}$'),
    discovered_at timestamptz NOT NULL,
    PRIMARY KEY (deployment_revision, tool_name, content_sha256)
);

CREATE INDEX supply_mcp_tool_snapshots_latest
    ON supply.mcp_tool_snapshots(deployment_revision,tool_name,discovered_at DESC,content_sha256);

REVOKE ALL ON supply.mcp_tool_snapshots FROM PUBLIC;

COMMENT ON COLUMN supply.deployments.mcp_protocol_version IS
    'Reviewed upstream MCP protocol version. v1 only accepts 2026-07-28.';
COMMENT ON TABLE supply.mcp_tool_snapshots IS
    'Append-only untrusted upstream Tool discovery snapshots. Publication into Catalog is a separate reviewed action.';
