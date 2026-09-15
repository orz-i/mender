-- Owner: governance. S4-A wraps PluginVersion lifecycle transitions with
-- maker/checker approval bound to an exact immutable manifest revision.
-- Publisher and reviewer roles only receive these SECURITY DEFINER wrappers;
-- raw supply lifecycle functions remain ungranted.

CREATE TABLE governance.plugin_publication_approvals (
    workspace_id text NOT NULL CHECK (workspace_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    id text NOT NULL CHECK (id ~ '^[A-Za-z0-9_-]{1,128}$'),
    plugin_id text NOT NULL CHECK (plugin_id ~ '^[a-z][a-z0-9.-]{2,127}$'),
    version text NOT NULL CHECK (version ~ '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'),
    target_revision bigint NOT NULL CHECK (target_revision > 0),
    requester_user_id text NOT NULL CHECK (requester_user_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    state text NOT NULL CHECK (state IN ('pending','approved','rejected','consumed','expired')),
    requested_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL CHECK (expires_at > requested_at),
    reviewer_user_id text CHECK (reviewer_user_id IS NULL OR reviewer_user_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    reviewed_at timestamptz,
    decision_note text NOT NULL DEFAULT '' CHECK (length(decision_note) <= 1000),
    consumed_at timestamptz,
    PRIMARY KEY(workspace_id,id),
    CHECK ((state='pending' AND reviewer_user_id IS NULL AND reviewed_at IS NULL AND consumed_at IS NULL)
        OR (state IN ('approved','rejected') AND reviewer_user_id IS NOT NULL AND reviewed_at IS NOT NULL AND consumed_at IS NULL)
        OR (state='consumed' AND reviewer_user_id IS NOT NULL AND reviewed_at IS NOT NULL AND consumed_at IS NOT NULL)
        OR (state='expired' AND consumed_at IS NULL)),
    CHECK (reviewer_user_id IS NULL OR reviewer_user_id<>requester_user_id),
    CHECK (reviewed_at IS NULL OR reviewed_at>=requested_at),
    CHECK (consumed_at IS NULL OR consumed_at>=reviewed_at)
);
CREATE UNIQUE INDEX one_active_plugin_publication_approval
    ON governance.plugin_publication_approvals(workspace_id,plugin_id,version)
    WHERE state IN ('pending','approved');
ALTER TABLE governance.plugin_publication_approvals ENABLE ROW LEVEL SECURITY;
ALTER TABLE governance.plugin_publication_approvals FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON governance.plugin_publication_approvals
    USING(workspace_id=nullif(current_setting('mender.workspace_id',true),''))
    WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));

CREATE TABLE governance.plugin_publication_audit_events (
    sequence bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    workspace_id text NOT NULL CHECK (workspace_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    approval_id text NOT NULL CHECK (approval_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    plugin_id text NOT NULL CHECK (plugin_id ~ '^[a-z][a-z0-9.-]{2,127}$'),
    version text NOT NULL CHECK (version ~ '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'),
    target_revision bigint NOT NULL CHECK (target_revision > 0),
    event_kind text NOT NULL CHECK (event_kind IN (
        'approval_submitted','approval_approved','approval_rejected','approval_expired',
        'approval_consumed','publication_committed'
    )),
    actor_user_id text CHECK (actor_user_id IS NULL OR actor_user_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    occurred_at timestamptz NOT NULL,
    note text NOT NULL DEFAULT '' CHECK (length(note) <= 1000),
    CHECK (event_kind NOT IN ('approval_submitted','approval_approved','approval_rejected') OR actor_user_id IS NOT NULL)
);
CREATE INDEX plugin_publication_audit_workspace_sequence
    ON governance.plugin_publication_audit_events(workspace_id,sequence DESC);
CREATE INDEX plugin_publication_audit_target_sequence
    ON governance.plugin_publication_audit_events(workspace_id,plugin_id,version,sequence DESC);
ALTER TABLE governance.plugin_publication_audit_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE governance.plugin_publication_audit_events FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON governance.plugin_publication_audit_events
    USING(workspace_id=nullif(current_setting('mender.workspace_id',true),''))
    WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));

CREATE FUNCTION governance.guard_plugin_publication_audit_immutable() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
    RAISE EXCEPTION 'plugin publication audit events are immutable' USING ERRCODE='42501';
END $$;
CREATE TRIGGER immutable_plugin_publication_audit
    BEFORE UPDATE OR DELETE ON governance.plugin_publication_audit_events
    FOR EACH ROW EXECUTE FUNCTION governance.guard_plugin_publication_audit_immutable();

CREATE FUNCTION governance.append_plugin_publication_audit(
    requested_workspace text,approval text,requested_plugin text,requested_version text,
    target_rev bigint,event_name text,actor_id text,at_time timestamptz,note_text text) RETURNS bigint
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE seq bigint;
BEGIN
    INSERT INTO governance.plugin_publication_audit_events(
      workspace_id,approval_id,plugin_id,version,target_revision,event_kind,actor_user_id,occurred_at,note)
    VALUES(requested_workspace,approval,requested_plugin,requested_version,target_rev,event_name,actor_id,at_time,coalesce(note_text,''))
    RETURNING sequence INTO seq;
    RETURN seq;
END $$;

-- Re-evaluate reviewed capabilities both at submission and at final publish.
-- Approved is intentionally accepted here so publication can fail closed if a
-- referenced capability becomes unavailable after review but before publish.
CREATE OR REPLACE FUNCTION supply.plugin_version_publish_issues(
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
    IF v.state NOT IN ('draft','submitted','approved') THEN
        RETURN QUERY SELECT 'not_publishable'::text,(requested_plugin||'@'||requested_version)::text;
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

CREATE FUNCTION governance.submit_plugin_publication(
    requested_workspace text,request_id text,requested_plugin text,requested_version text,
    requester_id text,at_time timestamptz,expiry_time timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE target_rev bigint;
BEGIN
    PERFORM supply.assert_publication_workspace(requested_workspace);
    IF expiry_time<=at_time OR expiry_time>at_time+interval '24 hours' THEN
        RAISE EXCEPTION 'plugin approval expiry is invalid' USING ERRCODE='22023';
    END IF;
    IF NOT EXISTS(
      SELECT 1 FROM supply.plugin_versions v
      JOIN supply.publishers p ON (p.workspace_id,p.id)=(v.workspace_id,v.publisher_id)
      WHERE v.workspace_id=requested_workspace AND v.plugin_id=requested_plugin AND v.version=requested_version
        AND p.owner_user_id=requester_id AND p.state='active') THEN
        RAISE EXCEPTION 'publisher owner is required' USING ERRCODE='42501';
    END IF;
    SELECT revision INTO target_rev FROM supply.plugin_versions
     WHERE workspace_id=requested_workspace AND plugin_id=requested_plugin AND version=requested_version AND state='draft'
     FOR UPDATE;
    IF target_rev IS NULL THEN
        RAISE EXCEPTION 'PluginVersion is not a draft' USING ERRCODE='23514';
    END IF;
    IF EXISTS(SELECT 1 FROM supply.plugin_version_publish_issues(requested_workspace,requested_plugin,requested_version)) THEN
        RAISE EXCEPTION 'PluginVersion publication preflight failed' USING ERRCODE='23514';
    END IF;
    IF EXISTS(SELECT 1 FROM governance.plugin_publication_approvals
      WHERE workspace_id=requested_workspace AND plugin_id=requested_plugin AND version=requested_version
        AND state IN ('pending','approved')) THEN
        RAISE EXCEPTION 'active plugin publication approval already exists' USING ERRCODE='23505';
    END IF;
    PERFORM supply.mark_plugin_submitted(requested_workspace,requested_plugin,requested_version,at_time);
    INSERT INTO governance.plugin_publication_approvals(
      workspace_id,id,plugin_id,version,target_revision,requester_user_id,state,requested_at,expires_at)
    VALUES(requested_workspace,request_id,requested_plugin,requested_version,target_rev,requester_id,'pending',at_time,expiry_time);
    PERFORM governance.append_plugin_publication_audit(
      requested_workspace,request_id,requested_plugin,requested_version,target_rev,
      'approval_submitted',requester_id,at_time,'');
END $$;

CREATE FUNCTION governance.approve_plugin_publication(
    requested_workspace text,request_id text,reviewer_id text,at_time timestamptz,note text) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE a record; current_rev bigint; current_state text;
BEGIN
    PERFORM supply.assert_publication_workspace(requested_workspace);
    SELECT * INTO a FROM governance.plugin_publication_approvals
     WHERE workspace_id=requested_workspace AND id=request_id FOR UPDATE;
    IF NOT FOUND OR a.state<>'pending' THEN
        RAISE EXCEPTION 'plugin publication approval is not pending' USING ERRCODE='23514';
    END IF;
    IF a.requester_user_id=reviewer_id THEN
        RAISE EXCEPTION 'requester cannot approve own plugin publication' USING ERRCODE='42501';
    END IF;
    SELECT revision,state INTO current_rev,current_state FROM supply.plugin_versions
     WHERE workspace_id=requested_workspace AND plugin_id=a.plugin_id AND version=a.version FOR UPDATE;
    IF current_rev IS NULL OR current_rev<>a.target_revision OR current_state<>'submitted' THEN
        RAISE EXCEPTION 'plugin publication target changed' USING ERRCODE='23514';
    END IF;
    IF a.expires_at<=at_time THEN
        PERFORM supply.reopen_plugin_draft(requested_workspace,a.plugin_id,a.version,a.target_revision);
        UPDATE governance.plugin_publication_approvals SET state='expired'
         WHERE workspace_id=requested_workspace AND id=request_id AND state='pending';
        PERFORM governance.append_plugin_publication_audit(
          requested_workspace,a.id,a.plugin_id,a.version,a.target_revision,
          'approval_expired',NULL,at_time,'');
        RETURN;
    END IF;
    IF EXISTS(SELECT 1 FROM supply.plugin_version_publish_issues(requested_workspace,a.plugin_id,a.version)) THEN
        RAISE EXCEPTION 'PluginVersion publication preflight failed' USING ERRCODE='23514';
    END IF;
    PERFORM supply.mark_plugin_approved(requested_workspace,a.plugin_id,a.version,a.target_revision,at_time);
    UPDATE governance.plugin_publication_approvals
       SET state='approved',reviewer_user_id=reviewer_id,reviewed_at=at_time,decision_note=coalesce(note,'')
     WHERE workspace_id=requested_workspace AND id=request_id AND state='pending';
    PERFORM governance.append_plugin_publication_audit(
      requested_workspace,a.id,a.plugin_id,a.version,a.target_revision,
      'approval_approved',reviewer_id,at_time,coalesce(note,''));
END $$;

CREATE FUNCTION governance.reject_plugin_publication(
    requested_workspace text,request_id text,reviewer_id text,at_time timestamptz,note text) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE a record; current_rev bigint; current_state text;
BEGIN
    PERFORM supply.assert_publication_workspace(requested_workspace);
    SELECT * INTO a FROM governance.plugin_publication_approvals
     WHERE workspace_id=requested_workspace AND id=request_id FOR UPDATE;
    IF NOT FOUND OR a.state<>'pending' THEN
        RAISE EXCEPTION 'plugin publication approval is not pending' USING ERRCODE='23514';
    END IF;
    IF a.requester_user_id=reviewer_id THEN
        RAISE EXCEPTION 'requester cannot review own plugin publication' USING ERRCODE='42501';
    END IF;
    SELECT revision,state INTO current_rev,current_state FROM supply.plugin_versions
     WHERE workspace_id=requested_workspace AND plugin_id=a.plugin_id AND version=a.version FOR UPDATE;
    IF current_rev IS NULL OR current_rev<>a.target_revision OR current_state<>'submitted' THEN
        RAISE EXCEPTION 'plugin publication target changed' USING ERRCODE='23514';
    END IF;
    PERFORM supply.reopen_plugin_draft(requested_workspace,a.plugin_id,a.version,a.target_revision);
    UPDATE governance.plugin_publication_approvals
       SET state='rejected',reviewer_user_id=reviewer_id,reviewed_at=at_time,decision_note=coalesce(note,'')
     WHERE workspace_id=requested_workspace AND id=request_id AND state='pending';
    PERFORM governance.append_plugin_publication_audit(
      requested_workspace,a.id,a.plugin_id,a.version,a.target_revision,
      'approval_rejected',reviewer_id,at_time,coalesce(note,''));
END $$;

CREATE FUNCTION governance.publish_approved_plugin(
    requested_workspace text,requested_plugin text,requested_version text,publisher_user_id text,at_time timestamptz)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE a record; current_rev bigint; current_state text;
BEGIN
    PERFORM supply.assert_publication_workspace(requested_workspace);
    IF NOT EXISTS(
      SELECT 1 FROM supply.plugin_versions v
      JOIN supply.publishers p ON (p.workspace_id,p.id)=(v.workspace_id,v.publisher_id)
      WHERE v.workspace_id=requested_workspace AND v.plugin_id=requested_plugin AND v.version=requested_version
        AND p.owner_user_id=publisher_user_id AND p.state='active') THEN
        RAISE EXCEPTION 'publisher owner is required' USING ERRCODE='42501';
    END IF;
    SELECT * INTO a FROM governance.plugin_publication_approvals
     WHERE workspace_id=requested_workspace AND plugin_id=requested_plugin AND version=requested_version
       AND state='approved' FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'approved plugin publication approval is required' USING ERRCODE='23514';
    END IF;
    SELECT revision,state INTO current_rev,current_state FROM supply.plugin_versions
     WHERE workspace_id=requested_workspace AND plugin_id=requested_plugin AND version=requested_version FOR UPDATE;
    IF current_rev IS NULL OR current_rev<>a.target_revision OR current_state<>'approved' THEN
        RAISE EXCEPTION 'approved PluginVersion changed' USING ERRCODE='23514';
    END IF;
    IF a.expires_at<=at_time THEN
        PERFORM supply.reopen_plugin_draft(requested_workspace,a.plugin_id,a.version,a.target_revision);
        UPDATE governance.plugin_publication_approvals SET state='expired'
         WHERE workspace_id=requested_workspace AND id=a.id AND state='approved';
        PERFORM governance.append_plugin_publication_audit(
          requested_workspace,a.id,a.plugin_id,a.version,a.target_revision,
          'approval_expired',NULL,at_time,'');
        RETURN NULL;
    END IF;
    IF EXISTS(SELECT 1 FROM supply.plugin_version_publish_issues(requested_workspace,requested_plugin,requested_version)) THEN
        RAISE EXCEPTION 'PluginVersion publication preflight failed' USING ERRCODE='23514';
    END IF;
    PERFORM supply.publish_plugin_version(requested_workspace,requested_plugin,requested_version,a.target_revision,at_time);
    UPDATE governance.plugin_publication_approvals
       SET state='consumed',consumed_at=at_time
     WHERE workspace_id=requested_workspace AND id=a.id AND state='approved';
    PERFORM governance.append_plugin_publication_audit(
      requested_workspace,a.id,a.plugin_id,a.version,a.target_revision,
      'approval_consumed',NULL,at_time,'');
    PERFORM governance.append_plugin_publication_audit(
      requested_workspace,a.id,a.plugin_id,a.version,a.target_revision,
      'publication_committed',NULL,at_time,'');
    RETURN a.id;
END $$;

REVOKE ALL ON governance.plugin_publication_approvals,governance.plugin_publication_audit_events FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.guard_plugin_publication_audit_immutable() FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.append_plugin_publication_audit(text,text,text,text,bigint,text,text,timestamptz,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.submit_plugin_publication(text,text,text,text,text,timestamptz,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.approve_plugin_publication(text,text,text,timestamptz,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.reject_plugin_publication(text,text,text,timestamptz,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.publish_approved_plugin(text,text,text,text,timestamptz) FROM PUBLIC;

