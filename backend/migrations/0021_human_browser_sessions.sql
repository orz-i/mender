-- Owner: identity. Human OIDC links and opaque browser sessions are separate
-- from machine API keys. Session tokens and CSRF values are stored as SHA-256
-- digests only; the browser receives the raw values once as cookies.
CREATE TABLE identity.users (
    id text PRIMARY KEY CHECK (id ~ '^[A-Za-z0-9_-]{1,128}$'),
    display_name text NOT NULL DEFAULT '' CHECK (length(display_name) <= 200),
    disabled boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL
);

CREATE TABLE identity.workspace_memberships (
    workspace_id text NOT NULL REFERENCES identity.workspaces(id),
    user_id text NOT NULL REFERENCES identity.users(id),
    role text NOT NULL CHECK (role IN ('owner','admin','developer','viewer')),
    disabled boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (workspace_id,user_id)
);
CREATE INDEX workspace_memberships_user ON identity.workspace_memberships(user_id,workspace_id);

CREATE TABLE identity.oidc_identities (
    issuer text NOT NULL CHECK (length(issuer) BETWEEN 8 AND 2048),
    subject text NOT NULL CHECK (length(subject) BETWEEN 1 AND 512),
    user_id text NOT NULL REFERENCES identity.users(id),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (issuer,subject)
);
CREATE INDEX oidc_identities_user ON identity.oidc_identities(user_id);

CREATE TABLE identity.browser_sessions (
    digest text PRIMARY KEY CHECK (digest ~ '^[a-f0-9]{64}$'),
    user_id text NOT NULL REFERENCES identity.users(id),
    csrf_digest text NOT NULL CHECK (csrf_digest ~ '^[a-f0-9]{64}$'),
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL CHECK (expires_at > created_at),
    revoked_at timestamptz NULL CHECK (revoked_at IS NULL OR revoked_at >= created_at)
);
CREATE INDEX browser_sessions_user_expires ON identity.browser_sessions(user_id,expires_at) WHERE revoked_at IS NULL;

REVOKE ALL ON identity.users,identity.workspace_memberships,identity.oidc_identities,identity.browser_sessions FROM PUBLIC;
