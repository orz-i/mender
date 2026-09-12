-- Owner: identity. A Human StartRun credential is a short-lived, exact
-- capability bound to one Workspace/Toolset/ToolVersion/Connection/currency
-- and an upper charge cap. The Browser Session cookie is never accepted as
-- run:create by Admission. Raw tokens are returned once; only SHA-256 digests
-- are persisted.
CREATE TABLE identity.run_start_delegations (
    id text PRIMARY KEY CHECK (id ~ '^[A-Za-z0-9_-]{1,128}$'),
    digest text NOT NULL UNIQUE CHECK (digest ~ '^[a-f0-9]{64}$'),
    workspace_id text NOT NULL REFERENCES identity.workspaces(id),
    user_id text NOT NULL REFERENCES identity.users(id),
    toolset_version_id text NOT NULL CHECK (toolset_version_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    tool_id text NOT NULL CHECK (tool_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    tool_version text NOT NULL CHECK (tool_version ~ '^[A-Za-z0-9][A-Za-z0-9._:+-]{0,127}$'),
    tool_version_id text NOT NULL CHECK (tool_version_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    connection_id text NOT NULL CHECK (connection_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    currency text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    max_charge_micro bigint NOT NULL CHECK (max_charge_micro >= 0),
    idempotency_key text NOT NULL CHECK (idempotency_key ~ '^[!-~]{8,128}$'),
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL CHECK (expires_at > created_at),
    revoked_at timestamptz NULL CHECK (revoked_at IS NULL OR revoked_at >= created_at)
);
CREATE INDEX run_start_delegations_user_workspace_expires
    ON identity.run_start_delegations(user_id,workspace_id,expires_at)
    WHERE revoked_at IS NULL;

REVOKE ALL ON identity.run_start_delegations FROM PUBLIC;
