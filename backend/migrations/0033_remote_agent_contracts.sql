-- Owner: supply. Remote Agent remains a reviewed supplier transport mapped
-- into the existing Execution Run/Job/Artifact lifecycle. This migration adds
-- no Agent-owned task table and enables no network access by itself.

ALTER TABLE supply.deployments
    DROP CONSTRAINT supply_transport_kind,
    DROP CONSTRAINT supply_transport_contract;

ALTER TABLE supply.deployments
    ADD CONSTRAINT supply_transport_kind CHECK (transport_kind IN ('http','mcp_streamable_http','agent_http')),
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
        OR (
            transport_kind='agent_http'
            AND idempotency_header ~ '^[A-Za-z0-9-]{1,128}$'
            AND mcp_protocol_version IS NULL
            AND mcp_stateless IS NULL
            AND status_endpoint_url IS NOT NULL
            AND status_http_method='POST'
            AND cancel_endpoint_url IS NOT NULL
            AND cancel_http_method='POST'
        )
    );

COMMENT ON COLUMN supply.deployments.transport_kind IS
    'Reviewed supplier transport: HTTP tool, stateless upstream MCP, or asynchronous remote Agent HTTP.';
