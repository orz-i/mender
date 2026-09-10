-- Owner: identity. Operator-only migration; the API role receives SELECT only.
CREATE SCHEMA identity;
CREATE TABLE identity.workspaces (
    id text PRIMARY KEY CHECK (id ~ '^[A-Za-z0-9_-]{1,128}$'),
    disabled boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE identity.service_accounts (
    workspace_id text NOT NULL REFERENCES identity.workspaces(id),
    id text NOT NULL CHECK (id ~ '^[A-Za-z0-9_-]{1,128}$'),
    disabled boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, id)
);
CREATE TABLE identity.api_keys (
    id text PRIMARY KEY CHECK (id ~ '^[a-f0-9]{32}$'),
    workspace_id text NOT NULL,
    subject_id text NOT NULL,
    digest text NOT NULL CHECK (digest ~ '^[a-f0-9]{64}$'),
    scopes text[] NOT NULL CHECK (cardinality(scopes) > 0 AND scopes <@ ARRAY['run:read','run:cancel']::text[] AND array_position(scopes,NULL) IS NULL),
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL CHECK (expires_at > created_at),
    revoked boolean NOT NULL DEFAULT false,
    FOREIGN KEY (workspace_id, subject_id) REFERENCES identity.service_accounts(workspace_id,id)
);
CREATE INDEX api_keys_workspace_subject ON identity.api_keys(workspace_id,subject_id);
REVOKE ALL ON SCHEMA identity FROM PUBLIC;
REVOKE ALL ON ALL TABLES IN SCHEMA identity FROM PUBLIC;
