-- Owners: catalog owns ToolVersion facts; distribution owns Toolset bindings;
-- connections owns connection/grant state; commerce owns PriceVersion and budgets.
-- This migration only creates local admission-plan data. It does not create an executor.

ALTER TABLE identity.api_keys DROP CONSTRAINT api_keys_scopes_check;
ALTER TABLE identity.api_keys ADD CONSTRAINT api_keys_scopes_check
    CHECK (cardinality(scopes) > 0
       AND scopes <@ ARRAY['run:read','run:cancel','run:create']::text[]
       AND array_position(scopes,NULL) IS NULL);

CREATE SCHEMA catalog;
CREATE TABLE catalog.tool_versions (
    id text PRIMARY KEY CHECK (id ~ '^[A-Za-z0-9_-]{1,128}$'),
    tool_id text NOT NULL CHECK (tool_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    version text NOT NULL CHECK (version ~ '^[A-Za-z0-9][A-Za-z0-9._:+-]{0,127}$'),
    provider_id text NOT NULL CHECK (provider_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    price_version_id text NOT NULL CHECK (price_version_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    deployment_revision text NOT NULL CHECK (deployment_revision ~ '^[A-Za-z0-9_-]{1,128}$'),
    state text NOT NULL CHECK (state IN ('published','disabled')),
    published_at timestamptz NOT NULL,
    UNIQUE(tool_id,version)
);

CREATE SCHEMA distribution;
CREATE TABLE distribution.toolset_bindings (
    workspace_id text NOT NULL CHECK (workspace_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    toolset_version_id text NOT NULL CHECK (toolset_version_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    tool_id text NOT NULL CHECK (tool_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    tool_version_label text NOT NULL CHECK (tool_version_label ~ '^[A-Za-z0-9][A-Za-z0-9._:+-]{0,127}$'),
    tool_version_id text NOT NULL CHECK (tool_version_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    budget_id text NOT NULL CHECK (budget_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    state text NOT NULL CHECK (state IN ('published','disabled')),
    published_at timestamptz NOT NULL,
    PRIMARY KEY(workspace_id,toolset_version_id,tool_version_id),
    UNIQUE(workspace_id,toolset_version_id,tool_id,tool_version_label)
);
ALTER TABLE distribution.toolset_bindings ENABLE ROW LEVEL SECURITY;
ALTER TABLE distribution.toolset_bindings FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON distribution.toolset_bindings
    USING(workspace_id=nullif(current_setting('mender.workspace_id',true),''))
    WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));

CREATE SCHEMA connections;
CREATE TABLE connections.connections (
    workspace_id text NOT NULL CHECK (workspace_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    id text NOT NULL CHECK (id ~ '^[A-Za-z0-9_-]{1,128}$'),
    provider_id text NOT NULL CHECK (provider_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    credential_version_ref text NOT NULL CHECK (credential_version_ref ~ '^[A-Za-z0-9_-]{1,128}$'),
    state text NOT NULL CHECK (state IN ('active','expired','revoked','error')),
    revision bigint NOT NULL CHECK (revision > 0),
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL CHECK (expires_at > created_at),
    PRIMARY KEY(workspace_id,id)
);
CREATE TABLE connections.connection_grants (
    workspace_id text NOT NULL,
    connection_id text NOT NULL,
    subject_id text NOT NULL CHECK (subject_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL CHECK (expires_at > created_at),
    PRIMARY KEY(workspace_id,connection_id,subject_id),
    FOREIGN KEY(workspace_id,connection_id) REFERENCES connections.connections(workspace_id,id)
);
ALTER TABLE connections.connections ENABLE ROW LEVEL SECURITY;
ALTER TABLE connections.connections FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON connections.connections
    USING(workspace_id=nullif(current_setting('mender.workspace_id',true),''))
    WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));
ALTER TABLE connections.connection_grants ENABLE ROW LEVEL SECURITY;
ALTER TABLE connections.connection_grants FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON connections.connection_grants
    USING(workspace_id=nullif(current_setting('mender.workspace_id',true),''))
    WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));

CREATE TABLE commerce.price_versions (
    id text PRIMARY KEY CHECK (id ~ '^[A-Za-z0-9_-]{1,128}$'),
    tool_version_id text NOT NULL CHECK (tool_version_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    currency text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    reserve_micro bigint NOT NULL CHECK (reserve_micro >= 0),
    starts_at timestamptz NOT NULL,
    ends_at timestamptz NOT NULL CHECK (ends_at > starts_at),
    active boolean NOT NULL DEFAULT true
);
CREATE INDEX price_version_tool ON commerce.price_versions(tool_version_id,id);

REVOKE ALL ON SCHEMA catalog,distribution,connections FROM PUBLIC;
REVOKE ALL ON ALL TABLES IN SCHEMA catalog FROM PUBLIC;
REVOKE ALL ON ALL TABLES IN SCHEMA distribution FROM PUBLIC;
REVOKE ALL ON ALL TABLES IN SCHEMA connections FROM PUBLIC;
REVOKE ALL ON commerce.price_versions FROM PUBLIC;
