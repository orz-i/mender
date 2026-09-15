-- Owner: supply. S4-A introduces Workspace-scoped Publisher, Plugin and
-- immutable PluginVersion manifest facts. A manifest is declarative metadata
-- only: it may reference reviewed API Tool, MCP Tool or Agent capabilities,
-- but it cannot carry executable artifacts, endpoints, secrets or policy code.

CREATE TABLE supply.publishers (
    workspace_id text NOT NULL CHECK (workspace_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    id text NOT NULL CHECK (id ~ '^[A-Za-z0-9_-]{1,128}$'),
    owner_user_id text NOT NULL CHECK (owner_user_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    display_name text NOT NULL CHECK (length(display_name) BETWEEN 1 AND 200),
    state text NOT NULL DEFAULT 'active' CHECK (state IN ('active','frozen','disabled')),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY(workspace_id,id)
);
ALTER TABLE supply.publishers ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply.publishers FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON supply.publishers
    USING(workspace_id=nullif(current_setting('mender.workspace_id',true),''))
    WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));

CREATE TABLE supply.plugins (
    workspace_id text NOT NULL,
    id text NOT NULL CHECK (id ~ '^[a-z][a-z0-9.-]{2,127}$'),
    publisher_id text NOT NULL CHECK (publisher_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    created_by_user_id text NOT NULL CHECK (created_by_user_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY(workspace_id,id),
    UNIQUE(workspace_id,id,publisher_id),
    FOREIGN KEY(workspace_id,publisher_id) REFERENCES supply.publishers(workspace_id,id)
);
ALTER TABLE supply.plugins ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply.plugins FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON supply.plugins
    USING(workspace_id=nullif(current_setting('mender.workspace_id',true),''))
    WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));

CREATE TABLE supply.plugin_versions (
    workspace_id text NOT NULL,
    plugin_id text NOT NULL CHECK (plugin_id ~ '^[a-z][a-z0-9.-]{2,127}$'),
    version text NOT NULL CHECK (version ~ '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'),
    publisher_id text NOT NULL CHECK (publisher_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    state text NOT NULL DEFAULT 'draft' CHECK (state IN ('draft','submitted','approved','published','deprecated','disabled')),
    manifest_json jsonb NOT NULL,
    manifest_sha256 text NOT NULL CHECK (manifest_sha256 ~ '^[a-f0-9]{64}$'),
    created_by_user_id text NOT NULL CHECK (created_by_user_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    submitted_at timestamptz,
    approved_at timestamptz,
    published_at timestamptz,
    deprecated_at timestamptz,
    disabled_at timestamptz,
    PRIMARY KEY(workspace_id,plugin_id,version),
    FOREIGN KEY(workspace_id,plugin_id,publisher_id) REFERENCES supply.plugins(workspace_id,id,publisher_id),
    CHECK (jsonb_typeof(manifest_json)='object'),
    CHECK (manifest_json->>'apiVersion'='mender.io/plugin/v1alpha1'),
    CHECK (manifest_json->>'plugin_id'=plugin_id),
    CHECK (manifest_json->>'version'=version),
    CHECK (manifest_json->>'publisher_id'=publisher_id),
    CHECK (jsonb_typeof(manifest_json->'capabilities')='array'),
    CHECK (jsonb_array_length(manifest_json->'capabilities') BETWEEN 1 AND 64),
    CHECK ((manifest_json - ARRAY['apiVersion','plugin_id','version','publisher_id','display_name','description','capabilities'])='{}'::jsonb),
    CHECK (
        (state='draft' AND submitted_at IS NULL AND approved_at IS NULL AND published_at IS NULL AND deprecated_at IS NULL AND disabled_at IS NULL)
        OR (state='submitted' AND submitted_at IS NOT NULL AND approved_at IS NULL AND published_at IS NULL AND deprecated_at IS NULL AND disabled_at IS NULL)
        OR (state='approved' AND submitted_at IS NOT NULL AND approved_at IS NOT NULL AND published_at IS NULL AND deprecated_at IS NULL AND disabled_at IS NULL)
        OR (state='published' AND submitted_at IS NOT NULL AND approved_at IS NOT NULL AND published_at IS NOT NULL AND deprecated_at IS NULL AND disabled_at IS NULL)
        OR (state='deprecated' AND submitted_at IS NOT NULL AND approved_at IS NOT NULL AND published_at IS NOT NULL AND deprecated_at IS NOT NULL AND disabled_at IS NULL)
        OR (state='disabled' AND submitted_at IS NOT NULL AND approved_at IS NOT NULL AND published_at IS NOT NULL AND disabled_at IS NOT NULL)
    )
);
ALTER TABLE supply.plugin_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply.plugin_versions FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON supply.plugin_versions
    USING(workspace_id=nullif(current_setting('mender.workspace_id',true),''))
    WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));

CREATE UNIQUE INDEX supply_plugin_manifest_digest_unique
    ON supply.plugin_versions(workspace_id,plugin_id,manifest_sha256);

CREATE FUNCTION supply.guard_plugin_version_mutation() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
DECLARE material_changed boolean;
BEGIN
    material_changed :=
        NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR
        NEW.plugin_id IS DISTINCT FROM OLD.plugin_id OR
        NEW.version IS DISTINCT FROM OLD.version OR
        NEW.publisher_id IS DISTINCT FROM OLD.publisher_id OR
        NEW.manifest_json IS DISTINCT FROM OLD.manifest_json OR
        NEW.manifest_sha256 IS DISTINCT FROM OLD.manifest_sha256 OR
        NEW.created_by_user_id IS DISTINCT FROM OLD.created_by_user_id;
    IF TG_OP='UPDATE' AND OLD.state<>'draft' AND material_changed THEN
        RAISE EXCEPTION 'submitted PluginVersion manifest is immutable' USING ERRCODE='23514';
    END IF;
    IF TG_OP='DELETE' AND OLD.state<>'draft' THEN
        RAISE EXCEPTION 'submitted PluginVersion cannot be deleted' USING ERRCODE='23514';
    END IF;
    IF TG_OP='UPDATE' THEN
        IF OLD.state='draft' AND material_changed THEN
            NEW.revision:=OLD.revision+1;
        ELSE
            NEW.revision:=OLD.revision;
        END IF;
        NEW.updated_at:=clock_timestamp();
    END IF;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER guard_plugin_version_mutation
    BEFORE UPDATE OR DELETE ON supply.plugin_versions
    FOR EACH ROW EXECUTE FUNCTION supply.guard_plugin_version_mutation();

CREATE FUNCTION supply.assert_publication_workspace(requested_workspace text) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
    IF requested_workspace IS NULL
       OR requested_workspace<>nullif(current_setting('mender.workspace_id',true),'') THEN
        RAISE EXCEPTION 'workspace scope mismatch' USING ERRCODE='42501';
    END IF;
END $$;

CREATE FUNCTION supply.plugin_version_publish_issues(
    requested_workspace text, requested_plugin text, requested_version text)
RETURNS TABLE(code text,target_id text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE v record; cap jsonb; kind text; target text;
BEGIN
    PERFORM supply.assert_publication_workspace(requested_workspace);
    SELECT * INTO v FROM supply.plugin_versions
      WHERE workspace_id=requested_workspace AND plugin_id=requested_plugin AND version=requested_version;
    IF NOT FOUND THEN
        RETURN QUERY SELECT 'not_found'::text,(requested_plugin||'@'||requested_version)::text;
        RETURN;
    END IF;
    IF v.state NOT IN ('draft','submitted') THEN
        RETURN QUERY SELECT 'not_submittable'::text,(requested_plugin||'@'||requested_version)::text;
        RETURN;
    END IF;
    IF NOT EXISTS(SELECT 1 FROM supply.publishers p
                  WHERE p.workspace_id=requested_workspace AND p.id=v.publisher_id AND p.state='active') THEN
        RETURN QUERY SELECT 'publisher_unavailable'::text,v.publisher_id;
    END IF;
    FOR cap IN SELECT value FROM jsonb_array_elements(v.manifest_json->'capabilities') LOOP
        IF jsonb_typeof(cap)<>'object' THEN
            RETURN QUERY SELECT 'capability_invalid'::text,(requested_plugin||'@'||requested_version)::text;
            CONTINUE;
        END IF;
        kind:=cap->>'kind';
        IF kind='api_tool' THEN
            target:=cap->>'tool_version_id';
            IF target IS NULL OR cap ? 'deployment_revision' OR (cap - ARRAY['kind','tool_version_id'])<>'{}'::jsonb
               OR NOT EXISTS(SELECT 1 FROM catalog.tool_version_management t
                             WHERE t.workspace_id=requested_workspace AND t.tool_version_id=target AND t.state='published') THEN
                RETURN QUERY SELECT 'api_tool_unavailable'::text,coalesce(target,'');
            END IF;
        ELSIF kind='mcp_tool' THEN
            target:=cap->>'tool_version_id';
            IF target IS NULL OR cap ? 'deployment_revision' OR (cap - ARRAY['kind','tool_version_id'])<>'{}'::jsonb
               OR NOT EXISTS(SELECT 1 FROM catalog.tool_version_management t
                             WHERE t.workspace_id=requested_workspace AND t.tool_version_id=target AND t.state='published' AND t.mcp_publishable) THEN
                RETURN QUERY SELECT 'mcp_tool_unavailable'::text,coalesce(target,'');
            END IF;
        ELSIF kind='agent' THEN
            target:=cap->>'deployment_revision';
            IF target IS NULL OR cap ? 'tool_version_id' OR (cap - ARRAY['kind','deployment_revision'])<>'{}'::jsonb
               OR NOT EXISTS(SELECT 1 FROM supply.deployments d
                             WHERE d.revision=target AND d.transport_kind='agent_http' AND d.state='active') THEN
                RETURN QUERY SELECT 'agent_unavailable'::text,coalesce(target,'');
            END IF;
        ELSE
            RETURN QUERY SELECT 'capability_invalid'::text,coalesce(kind,'');
        END IF;
    END LOOP;
END $$;

-- Lifecycle helpers are deliberately not granted to the publisher-manager yet.
-- S4 governance wraps them with maker/checker approval before exposure.
CREATE FUNCTION supply.mark_plugin_submitted(
    requested_workspace text, requested_plugin text, requested_version text, at_time timestamptz) RETURNS bigint
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE rev bigint;
BEGIN
    PERFORM supply.assert_publication_workspace(requested_workspace);
    IF EXISTS(SELECT 1 FROM supply.plugin_version_publish_issues(requested_workspace,requested_plugin,requested_version)) THEN
        RAISE EXCEPTION 'PluginVersion publication preflight failed' USING ERRCODE='23514';
    END IF;
    UPDATE supply.plugin_versions
       SET state='submitted',submitted_at=at_time
     WHERE workspace_id=requested_workspace AND plugin_id=requested_plugin AND version=requested_version AND state='draft'
     RETURNING revision INTO rev;
    IF rev IS NULL THEN
        RAISE EXCEPTION 'PluginVersion is not a draft' USING ERRCODE='23514';
    END IF;
    RETURN rev;
END $$;

CREATE FUNCTION supply.mark_plugin_approved(
    requested_workspace text, requested_plugin text, requested_version text, expected_revision bigint, at_time timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
    PERFORM supply.assert_publication_workspace(requested_workspace);
    UPDATE supply.plugin_versions
       SET state='approved',approved_at=at_time
     WHERE workspace_id=requested_workspace AND plugin_id=requested_plugin AND version=requested_version
       AND revision=expected_revision AND state='submitted';
    IF NOT FOUND THEN
        RAISE EXCEPTION 'PluginVersion approval target changed' USING ERRCODE='23514';
    END IF;
END $$;

CREATE FUNCTION supply.reopen_plugin_draft(
    requested_workspace text, requested_plugin text, requested_version text, expected_revision bigint) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
    PERFORM supply.assert_publication_workspace(requested_workspace);
    UPDATE supply.plugin_versions
       SET state='draft',submitted_at=NULL,approved_at=NULL
     WHERE workspace_id=requested_workspace AND plugin_id=requested_plugin AND version=requested_version
       AND revision=expected_revision AND state IN ('submitted','approved');
    IF NOT FOUND THEN
        RAISE EXCEPTION 'PluginVersion cannot return to draft' USING ERRCODE='23514';
    END IF;
END $$;

CREATE FUNCTION supply.publish_plugin_version(
    requested_workspace text, requested_plugin text, requested_version text, expected_revision bigint, at_time timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
    PERFORM supply.assert_publication_workspace(requested_workspace);
    UPDATE supply.plugin_versions
       SET state='published',published_at=at_time
     WHERE workspace_id=requested_workspace AND plugin_id=requested_plugin AND version=requested_version
       AND revision=expected_revision AND state='approved';
    IF NOT FOUND THEN
        RAISE EXCEPTION 'approved PluginVersion is required' USING ERRCODE='23514';
    END IF;
END $$;

REVOKE ALL ON supply.publishers,supply.plugins,supply.plugin_versions FROM PUBLIC;
REVOKE ALL ON FUNCTION supply.guard_plugin_version_mutation() FROM PUBLIC;
REVOKE ALL ON FUNCTION supply.assert_publication_workspace(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION supply.plugin_version_publish_issues(text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION supply.mark_plugin_submitted(text,text,text,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION supply.mark_plugin_approved(text,text,text,bigint,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION supply.reopen_plugin_draft(text,text,text,bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION supply.publish_plugin_version(text,text,text,bigint,timestamptz) FROM PUBLIC;

