-- Owners: catalog owns workspace-scoped ToolVersion management facts;
-- distribution owns workspace-scoped Toolset lifecycle and bindings.
-- Runtime/publication facts remain fail-closed: drafts are never callable.

ALTER TABLE catalog.tool_versions DROP CONSTRAINT tool_versions_state_check;
ALTER TABLE catalog.tool_versions
    ADD CONSTRAINT tool_versions_state_check CHECK (state IN ('published','disabled','retired'));

CREATE FUNCTION catalog.guard_published_tool_version_immutable() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
    IF OLD.state='published' AND (
        NEW.id IS DISTINCT FROM OLD.id OR
        NEW.tool_id IS DISTINCT FROM OLD.tool_id OR
        NEW.version IS DISTINCT FROM OLD.version OR
        NEW.provider_id IS DISTINCT FROM OLD.provider_id OR
        NEW.price_version_id IS DISTINCT FROM OLD.price_version_id OR
        NEW.deployment_revision IS DISTINCT FROM OLD.deployment_revision OR
        NEW.title IS DISTINCT FROM OLD.title OR
        NEW.description IS DISTINCT FROM OLD.description OR
        NEW.input_schema IS DISTINCT FROM OLD.input_schema OR
        NEW.output_schema IS DISTINCT FROM OLD.output_schema OR
        NEW.side_effect IS DISTINCT FROM OLD.side_effect OR
        NEW.idempotency IS DISTINCT FROM OLD.idempotency OR
        NEW.mcp_publishable IS DISTINCT FROM OLD.mcp_publishable OR
        NEW.published_at IS DISTINCT FROM OLD.published_at
    ) THEN
        RAISE EXCEPTION 'published ToolVersion facts are immutable' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER immutable_published_tool_version
    BEFORE UPDATE ON catalog.tool_versions
    FOR EACH ROW EXECUTE FUNCTION catalog.guard_published_tool_version_immutable();

CREATE TABLE catalog.tool_version_management (
    workspace_id text NOT NULL CHECK (workspace_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    tool_version_id text NOT NULL CHECK (tool_version_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    tool_id text NOT NULL CHECK (tool_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    version text NOT NULL CHECK (version ~ '^[A-Za-z0-9][A-Za-z0-9._:+-]{0,127}$'),
    provider_id text NOT NULL CHECK (provider_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    price_version_id text NOT NULL CHECK (price_version_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    deployment_revision text NOT NULL CHECK (deployment_revision ~ '^[A-Za-z0-9_-]{1,128}$'),
    title text NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
    description text NOT NULL DEFAULT '' CHECK (length(description) <= 4000),
    input_schema jsonb NOT NULL CHECK (jsonb_typeof(input_schema)='object' AND input_schema->>'type'='object'),
    output_schema jsonb NOT NULL CHECK (jsonb_typeof(output_schema)='object'),
    side_effect text NOT NULL CHECK (side_effect IN ('read_only','write')),
    idempotency text NOT NULL CHECK (idempotency IN ('safe_read','idempotent','unsafe')),
    mcp_publishable boolean NOT NULL DEFAULT false,
    state text NOT NULL DEFAULT 'draft' CHECK (state IN ('draft','published','retired')),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    published_at timestamptz,
    retired_at timestamptz,
    PRIMARY KEY(workspace_id,tool_version_id),
    UNIQUE(workspace_id,tool_id,version),
    CHECK ((state='draft' AND published_at IS NULL AND retired_at IS NULL)
        OR (state='published' AND published_at IS NOT NULL AND retired_at IS NULL)
        OR (state='retired' AND published_at IS NOT NULL AND retired_at IS NOT NULL AND retired_at>=published_at))
);
ALTER TABLE catalog.tool_version_management ENABLE ROW LEVEL SECURITY;
ALTER TABLE catalog.tool_version_management FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON catalog.tool_version_management
    USING(workspace_id=nullif(current_setting('mender.workspace_id',true),''))
    WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));

CREATE FUNCTION catalog.guard_management_tool_version() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
    IF TG_OP='UPDATE' AND OLD.state<>'draft' AND (
        NEW.tool_version_id IS DISTINCT FROM OLD.tool_version_id OR
        NEW.tool_id IS DISTINCT FROM OLD.tool_id OR
        NEW.version IS DISTINCT FROM OLD.version OR
        NEW.provider_id IS DISTINCT FROM OLD.provider_id OR
        NEW.price_version_id IS DISTINCT FROM OLD.price_version_id OR
        NEW.deployment_revision IS DISTINCT FROM OLD.deployment_revision OR
        NEW.title IS DISTINCT FROM OLD.title OR
        NEW.description IS DISTINCT FROM OLD.description OR
        NEW.input_schema IS DISTINCT FROM OLD.input_schema OR
        NEW.output_schema IS DISTINCT FROM OLD.output_schema OR
        NEW.side_effect IS DISTINCT FROM OLD.side_effect OR
        NEW.idempotency IS DISTINCT FROM OLD.idempotency OR
        NEW.mcp_publishable IS DISTINCT FROM OLD.mcp_publishable
    ) THEN
        RAISE EXCEPTION 'published ToolVersion management facts are immutable' USING ERRCODE='23514';
    END IF;
    IF TG_OP='DELETE' AND OLD.state<>'draft' THEN
        RAISE EXCEPTION 'published ToolVersion management facts cannot be deleted' USING ERRCODE='23514';
    END IF;
    IF TG_OP='UPDATE' AND NEW.state=OLD.state THEN
        NEW.updated_at:=clock_timestamp();
    END IF;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER guard_management_tool_version
    BEFORE UPDATE OR DELETE ON catalog.tool_version_management
    FOR EACH ROW EXECUTE FUNCTION catalog.guard_management_tool_version();

CREATE TABLE distribution.toolsets (
    workspace_id text NOT NULL CHECK (workspace_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    id text NOT NULL CHECK (id ~ '^[A-Za-z0-9_-]{1,128}$'),
    state text NOT NULL DEFAULT 'draft' CHECK (state IN ('draft','published','retired')),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    published_at timestamptz,
    retired_at timestamptz,
    PRIMARY KEY(workspace_id,id),
    CHECK ((state='draft' AND published_at IS NULL AND retired_at IS NULL)
        OR (state='published' AND published_at IS NOT NULL AND retired_at IS NULL)
        OR (state='retired' AND published_at IS NOT NULL AND retired_at IS NOT NULL AND retired_at>=published_at))
);
ALTER TABLE distribution.toolsets ENABLE ROW LEVEL SECURITY;
ALTER TABLE distribution.toolsets FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON distribution.toolsets
    USING(workspace_id=nullif(current_setting('mender.workspace_id',true),''))
    WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));

INSERT INTO distribution.toolsets(workspace_id,id,state,created_at,updated_at,published_at,retired_at)
SELECT workspace_id,toolset_version_id,
       CASE WHEN bool_or(state='published') THEN 'published' ELSE 'retired' END,
       min(published_at),max(published_at),min(published_at),
       CASE WHEN bool_or(state='published') THEN NULL ELSE max(published_at) END
FROM distribution.toolset_bindings
GROUP BY workspace_id,toolset_version_id;

ALTER TABLE distribution.toolset_bindings DROP CONSTRAINT toolset_bindings_state_check;
ALTER TABLE distribution.toolset_bindings ALTER COLUMN state SET DEFAULT 'draft';
ALTER TABLE distribution.toolset_bindings ALTER COLUMN published_at DROP NOT NULL;
ALTER TABLE distribution.toolset_bindings
    ADD CONSTRAINT toolset_bindings_state_check CHECK (state IN ('draft','published','disabled','retired')),
    ADD CONSTRAINT toolset_bindings_publication_check CHECK ((state='draft' AND published_at IS NULL) OR (state<>'draft' AND published_at IS NOT NULL)),
    ADD CONSTRAINT toolset_bindings_toolset_fk FOREIGN KEY(workspace_id,toolset_version_id) REFERENCES distribution.toolsets(workspace_id,id);

CREATE FUNCTION distribution.guard_toolset_binding_mutation() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
DECLARE parent_state text;
BEGIN
    IF TG_OP='DELETE' AND OLD.state<>'draft' THEN
        RAISE EXCEPTION 'published Toolset binding cannot be deleted' USING ERRCODE='23514';
    END IF;
    IF TG_OP='UPDATE' AND OLD.state<>'draft' AND (
        NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR
        NEW.toolset_version_id IS DISTINCT FROM OLD.toolset_version_id OR
        NEW.tool_id IS DISTINCT FROM OLD.tool_id OR
        NEW.tool_version_label IS DISTINCT FROM OLD.tool_version_label OR
        NEW.tool_version_id IS DISTINCT FROM OLD.tool_version_id OR
        NEW.budget_id IS DISTINCT FROM OLD.budget_id OR
        NEW.connection_id IS DISTINCT FROM OLD.connection_id OR
        NEW.mcp_name IS DISTINCT FROM OLD.mcp_name OR
        NEW.mcp_exposed IS DISTINCT FROM OLD.mcp_exposed OR
        NEW.published_at IS DISTINCT FROM OLD.published_at
    ) THEN
        RAISE EXCEPTION 'published Toolset binding facts are immutable' USING ERRCODE='23514';
    END IF;
    -- Existing operator fixtures and privileged migrations may seed immutable
    -- published bindings directly. Restricted catalog-manager roles cannot set
    -- the state/published_at columns, so their inserts still default to draft.
    IF TG_OP='INSERT' AND NEW.state<>'draft' THEN
        INSERT INTO distribution.toolsets(workspace_id,id,state,created_at,updated_at,published_at,retired_at)
        VALUES(NEW.workspace_id,NEW.toolset_version_id,
            CASE WHEN NEW.state='published' THEN 'published' ELSE 'retired' END,
            NEW.published_at,NEW.published_at,NEW.published_at,
            CASE WHEN NEW.state='published' THEN NULL ELSE NEW.published_at END)
        ON CONFLICT(workspace_id,id) DO NOTHING;
        RETURN NEW;
    END IF;
    IF TG_OP='INSERT' OR (TG_OP='UPDATE' AND OLD.state='draft') THEN
        SELECT state INTO parent_state FROM distribution.toolsets
        WHERE workspace_id=NEW.workspace_id AND id=NEW.toolset_version_id;
        IF parent_state IS DISTINCT FROM 'draft' THEN
            RAISE EXCEPTION 'Toolset bindings are editable only while the Toolset is draft' USING ERRCODE='23514';
        END IF;
    END IF;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER guard_toolset_binding_mutation
    BEFORE INSERT OR UPDATE OR DELETE ON distribution.toolset_bindings
    FOR EACH ROW EXECUTE FUNCTION distribution.guard_toolset_binding_mutation();

REVOKE ALL ON catalog.tool_version_management,distribution.toolsets FROM PUBLIC;
REVOKE ALL ON FUNCTION catalog.guard_published_tool_version_immutable() FROM PUBLIC;
REVOKE ALL ON FUNCTION catalog.guard_management_tool_version() FROM PUBLIC;
REVOKE ALL ON FUNCTION distribution.guard_toolset_binding_mutation() FROM PUBLIC;
