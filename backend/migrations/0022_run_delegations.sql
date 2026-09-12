-- Owner: identity. Human browser sessions can mint short-lived Run API
-- delegations, but the browser session cookie itself is never a Run
-- credential. Raw delegation tokens are returned once and only SHA-256
-- digests are persisted.
CREATE TABLE identity.run_delegations (
    id text PRIMARY KEY CHECK (id ~ '^[A-Za-z0-9_-]{1,128}$'),
    digest text NOT NULL UNIQUE CHECK (digest ~ '^[a-f0-9]{64}$'),
    workspace_id text NOT NULL REFERENCES identity.workspaces(id),
    user_id text NOT NULL REFERENCES identity.users(id),
    scopes text[] NOT NULL CHECK (cardinality(scopes) BETWEEN 1 AND 2),
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL CHECK (expires_at > created_at),
    revoked_at timestamptz NULL CHECK (revoked_at IS NULL OR revoked_at >= created_at),
    CHECK (scopes <@ ARRAY['run:read','run:cancel']::text[])
);
CREATE INDEX run_delegations_user_workspace_expires
    ON identity.run_delegations(user_id,workspace_id,expires_at)
    WHERE revoked_at IS NULL;

REVOKE ALL ON identity.run_delegations FROM PUBLIC;
