-- Owner: governance owns the append-only publication audit timeline.
-- Approval rows remain the mutable current-state model; audit events preserve
-- immutable governance history. Audit payloads contain governance metadata only.

CREATE TABLE governance.catalog_publication_audit_events (
    sequence bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    workspace_id text NOT NULL CHECK (workspace_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    approval_id text CHECK (approval_id IS NULL OR approval_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    target_kind text NOT NULL CHECK (target_kind IN ('tool_version','toolset')),
    target_id text NOT NULL CHECK (target_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    target_revision bigint NOT NULL CHECK (target_revision > 0),
    observed_revision bigint CHECK (observed_revision IS NULL OR observed_revision > 0),
    event_kind text NOT NULL CHECK (event_kind IN (
        'audit_baseline',
        'approval_submitted','approval_approved','approval_rejected','approval_expired','approval_consumed',
        'publication_committed','publication_retired'
    )),
    actor_user_id text CHECK (actor_user_id IS NULL OR actor_user_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    occurred_at timestamptz NOT NULL,
    reason_code text NOT NULL DEFAULT '' CHECK (reason_code ~ '^[a-z0-9_]{0,64}$'),
    note text NOT NULL DEFAULT '' CHECK (length(note) <= 1000),
    CHECK ((event_kind IN ('approval_submitted','approval_approved','approval_rejected','approval_expired','approval_consumed','publication_committed') AND approval_id IS NOT NULL)
        OR event_kind IN ('audit_baseline','publication_retired')),
    CHECK (event_kind NOT IN ('approval_submitted','approval_approved','approval_rejected') OR actor_user_id IS NOT NULL)
);
CREATE INDEX catalog_publication_audit_workspace_sequence
    ON governance.catalog_publication_audit_events(workspace_id,sequence DESC);
CREATE INDEX catalog_publication_audit_target_sequence
    ON governance.catalog_publication_audit_events(workspace_id,target_kind,target_id,sequence DESC);
CREATE INDEX catalog_publication_audit_approval_sequence
    ON governance.catalog_publication_audit_events(workspace_id,approval_id,sequence DESC)
    WHERE approval_id IS NOT NULL;

ALTER TABLE governance.catalog_publication_audit_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE governance.catalog_publication_audit_events FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON governance.catalog_publication_audit_events
    USING(workspace_id=nullif(current_setting('mender.workspace_id',true),''))
    WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));

CREATE FUNCTION governance.guard_catalog_publication_audit_immutable() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
    RAISE EXCEPTION 'publication audit events are immutable' USING ERRCODE='42501';
END $$;
CREATE TRIGGER immutable_catalog_publication_audit
    BEFORE UPDATE OR DELETE ON governance.catalog_publication_audit_events
    FOR EACH ROW EXECUTE FUNCTION governance.guard_catalog_publication_audit_immutable();

-- Existing approval state predates this audit timeline. Preserve an explicit,
-- honest activation baseline instead of fabricating historical transitions.
INSERT INTO governance.catalog_publication_audit_events(
  workspace_id,approval_id,target_kind,target_id,target_revision,observed_revision,
  event_kind,actor_user_id,occurred_at,reason_code,note)
SELECT workspace_id,id,target_kind,target_id,target_revision,target_revision,
       'audit_baseline',NULL,clock_timestamp(),state,''
FROM governance.catalog_publication_approvals;

CREATE FUNCTION governance.append_catalog_publication_audit(
    requested_workspace text,approval text,requested_kind text,requested_id text,
    target_rev bigint,observed_rev bigint,event_name text,actor_id text,
    at_time timestamptz,reason text,note_text text) RETURNS bigint
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE seq bigint;
BEGIN
    INSERT INTO governance.catalog_publication_audit_events(
      workspace_id,approval_id,target_kind,target_id,target_revision,observed_revision,
      event_kind,actor_user_id,occurred_at,reason_code,note)
    VALUES(requested_workspace,approval,requested_kind,requested_id,target_rev,observed_rev,
      event_name,actor_id,at_time,coalesce(reason,''),coalesce(note_text,''))
    RETURNING sequence INTO seq;
    RETURN seq;
END $$;

CREATE FUNCTION governance.expire_catalog_publication_for_target_change(
    requested_workspace text,requested_kind text,requested_id text,observed_rev bigint,
    at_time timestamptz,reason text) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE a record;
BEGIN
    IF reason NOT IN ('revision_drift','target_deleted') THEN
        RAISE EXCEPTION 'unsupported publication expiration reason' USING ERRCODE='22023';
    END IF;
    FOR a IN
        SELECT * FROM governance.catalog_publication_approvals
         WHERE workspace_id=requested_workspace AND target_kind=requested_kind AND target_id=requested_id
           AND state IN ('pending','approved')
           AND (reason='target_deleted' OR target_revision<>observed_rev)
         ORDER BY requested_at,id
         FOR UPDATE
    LOOP
        UPDATE governance.catalog_publication_approvals
           SET state='expired'
         WHERE workspace_id=requested_workspace AND id=a.id AND state IN ('pending','approved');
        PERFORM governance.append_catalog_publication_audit(
          requested_workspace,a.id,a.target_kind,a.target_id,a.target_revision,observed_rev,
          'approval_expired',NULL,at_time,reason,'');
    END LOOP;
END $$;

CREATE FUNCTION governance.expire_catalog_publication_due_time(
    requested_workspace text,requested_kind text,requested_id text,at_time timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE a record;
BEGIN
    FOR a IN
        SELECT * FROM governance.catalog_publication_approvals
         WHERE workspace_id=requested_workspace AND target_kind=requested_kind AND target_id=requested_id
           AND state IN ('pending','approved') AND expires_at<=at_time
         ORDER BY requested_at,id
         FOR UPDATE
    LOOP
        UPDATE governance.catalog_publication_approvals
           SET state='expired'
         WHERE workspace_id=requested_workspace AND id=a.id AND state IN ('pending','approved');
        PERFORM governance.append_catalog_publication_audit(
          requested_workspace,a.id,a.target_kind,a.target_id,a.target_revision,a.target_revision,
          'approval_expired',NULL,at_time,'ttl_elapsed','');
    END LOOP;
END $$;

CREATE FUNCTION governance.audit_tool_version_target_change() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        PERFORM governance.expire_catalog_publication_for_target_change(
          OLD.workspace_id,'tool_version',OLD.tool_version_id,NULL,clock_timestamp(),'target_deleted');
    ELSIF NEW.revision<>OLD.revision THEN
        PERFORM governance.expire_catalog_publication_for_target_change(
          NEW.workspace_id,'tool_version',NEW.tool_version_id,NEW.revision,clock_timestamp(),'revision_drift');
    END IF;
    RETURN NULL;
END $$;
CREATE TRIGGER audit_tool_version_target_change
    AFTER UPDATE OR DELETE ON catalog.tool_version_management
    FOR EACH ROW EXECUTE FUNCTION governance.audit_tool_version_target_change();

CREATE FUNCTION governance.audit_toolset_target_change() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
    IF NEW.revision<>OLD.revision THEN
        PERFORM governance.expire_catalog_publication_for_target_change(
          NEW.workspace_id,'toolset',NEW.id,NEW.revision,clock_timestamp(),'revision_drift');
    END IF;
    RETURN NULL;
END $$;
CREATE TRIGGER audit_toolset_target_change
    AFTER UPDATE ON distribution.toolsets
    FOR EACH ROW EXECUTE FUNCTION governance.audit_toolset_target_change();

CREATE OR REPLACE FUNCTION governance.submit_catalog_publication(
    requested_workspace text,request_id text,requested_kind text,requested_id text,
    requester_id text,at_time timestamptz,expiry_time timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE target_rev bigint;
BEGIN
    PERFORM catalog.assert_management_workspace(requested_workspace);
    IF expiry_time<=at_time OR expiry_time>at_time+interval '24 hours' THEN
        RAISE EXCEPTION 'publication approval expiry is invalid' USING ERRCODE='22023';
    END IF;
    PERFORM governance.assert_catalog_preflight(requested_workspace,requested_kind,requested_id,at_time);
    target_rev:=governance.catalog_target_revision(requested_workspace,requested_kind,requested_id);
    PERFORM governance.expire_catalog_publication_due_time(requested_workspace,requested_kind,requested_id,at_time);
    PERFORM governance.expire_catalog_publication_for_target_change(
      requested_workspace,requested_kind,requested_id,target_rev,at_time,'revision_drift');
    IF EXISTS(SELECT 1 FROM governance.catalog_publication_approvals
              WHERE workspace_id=requested_workspace AND target_kind=requested_kind AND target_id=requested_id
                AND state IN ('pending','approved')) THEN
        RAISE EXCEPTION 'active publication approval already exists' USING ERRCODE='23505';
    END IF;
    INSERT INTO governance.catalog_publication_approvals(
      workspace_id,id,target_kind,target_id,target_revision,requester_user_id,state,requested_at,expires_at)
    VALUES(requested_workspace,request_id,requested_kind,requested_id,target_rev,requester_id,'pending',at_time,expiry_time);
    PERFORM governance.append_catalog_publication_audit(
      requested_workspace,request_id,requested_kind,requested_id,target_rev,target_rev,
      'approval_submitted',requester_id,at_time,'','');
END $$;

CREATE OR REPLACE FUNCTION governance.approve_catalog_publication(
    requested_workspace text,request_id text,reviewer_id text,at_time timestamptz,note text) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE a record; current_rev bigint;
BEGIN
    PERFORM catalog.assert_management_workspace(requested_workspace);
    SELECT * INTO a FROM governance.catalog_publication_approvals
     WHERE workspace_id=requested_workspace AND id=request_id FOR UPDATE;
    IF NOT FOUND OR a.state<>'pending' THEN
        RAISE EXCEPTION 'publication approval is not pending' USING ERRCODE='23514';
    END IF;
    IF a.requester_user_id=reviewer_id THEN
        RAISE EXCEPTION 'requester cannot approve own publication' USING ERRCODE='42501';
    END IF;
    IF a.expires_at<=at_time THEN
        UPDATE governance.catalog_publication_approvals SET state='expired'
         WHERE workspace_id=requested_workspace AND id=request_id AND state='pending';
        PERFORM governance.append_catalog_publication_audit(
          requested_workspace,a.id,a.target_kind,a.target_id,a.target_revision,a.target_revision,
          'approval_expired',NULL,at_time,'ttl_elapsed','');
        RETURN;
    END IF;
    current_rev:=governance.catalog_target_revision(requested_workspace,a.target_kind,a.target_id);
    IF current_rev<>a.target_revision THEN
        UPDATE governance.catalog_publication_approvals SET state='expired'
         WHERE workspace_id=requested_workspace AND id=request_id AND state='pending';
        PERFORM governance.append_catalog_publication_audit(
          requested_workspace,a.id,a.target_kind,a.target_id,a.target_revision,current_rev,
          'approval_expired',NULL,at_time,'revision_drift','');
        RETURN;
    END IF;
    PERFORM governance.assert_catalog_preflight(requested_workspace,a.target_kind,a.target_id,at_time);
    UPDATE governance.catalog_publication_approvals
       SET state='approved',reviewer_user_id=reviewer_id,reviewed_at=at_time,decision_note=coalesce(note,'')
     WHERE workspace_id=requested_workspace AND id=request_id AND state='pending';
    PERFORM governance.append_catalog_publication_audit(
      requested_workspace,a.id,a.target_kind,a.target_id,a.target_revision,current_rev,
      'approval_approved',reviewer_id,at_time,'',coalesce(note,''));
END $$;

CREATE OR REPLACE FUNCTION governance.reject_catalog_publication(
    requested_workspace text,request_id text,reviewer_id text,at_time timestamptz,note text) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE a record; current_rev bigint;
BEGIN
    PERFORM catalog.assert_management_workspace(requested_workspace);
    SELECT * INTO a FROM governance.catalog_publication_approvals
     WHERE workspace_id=requested_workspace AND id=request_id FOR UPDATE;
    IF NOT FOUND OR a.state<>'pending' THEN
        RAISE EXCEPTION 'publication approval is not pending' USING ERRCODE='23514';
    END IF;
    IF a.requester_user_id=reviewer_id THEN
        RAISE EXCEPTION 'requester cannot review own publication' USING ERRCODE='42501';
    END IF;
    IF a.expires_at<=at_time THEN
        UPDATE governance.catalog_publication_approvals SET state='expired'
         WHERE workspace_id=requested_workspace AND id=request_id AND state='pending';
        PERFORM governance.append_catalog_publication_audit(
          requested_workspace,a.id,a.target_kind,a.target_id,a.target_revision,a.target_revision,
          'approval_expired',NULL,at_time,'ttl_elapsed','');
        RETURN;
    END IF;
    current_rev:=governance.catalog_target_revision(requested_workspace,a.target_kind,a.target_id);
    IF current_rev<>a.target_revision THEN
        UPDATE governance.catalog_publication_approvals SET state='expired'
         WHERE workspace_id=requested_workspace AND id=request_id AND state='pending';
        PERFORM governance.append_catalog_publication_audit(
          requested_workspace,a.id,a.target_kind,a.target_id,a.target_revision,current_rev,
          'approval_expired',NULL,at_time,'revision_drift','');
        RETURN;
    END IF;
    UPDATE governance.catalog_publication_approvals
       SET state='rejected',reviewer_user_id=reviewer_id,reviewed_at=at_time,decision_note=coalesce(note,'')
     WHERE workspace_id=requested_workspace AND id=request_id AND state='pending';
    PERFORM governance.append_catalog_publication_audit(
      requested_workspace,a.id,a.target_kind,a.target_id,a.target_revision,current_rev,
      'approval_rejected',reviewer_id,at_time,'',coalesce(note,''));
END $$;

CREATE OR REPLACE FUNCTION governance.consume_catalog_publication(
    requested_workspace text,requested_kind text,requested_id text,at_time timestamptz) RETURNS text
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE a record; current_rev bigint;
BEGIN
    PERFORM catalog.assert_management_workspace(requested_workspace);
    SELECT * INTO a FROM governance.catalog_publication_approvals
     WHERE workspace_id=requested_workspace AND target_kind=requested_kind AND target_id=requested_id
       AND state='approved' FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'approved publication approval is required' USING ERRCODE='23514';
    END IF;
    IF a.expires_at<=at_time THEN
        UPDATE governance.catalog_publication_approvals SET state='expired'
         WHERE workspace_id=requested_workspace AND id=a.id AND state='approved';
        PERFORM governance.append_catalog_publication_audit(
          requested_workspace,a.id,a.target_kind,a.target_id,a.target_revision,a.target_revision,
          'approval_expired',NULL,at_time,'ttl_elapsed','');
        RETURN NULL;
    END IF;
    current_rev:=governance.catalog_target_revision(requested_workspace,requested_kind,requested_id);
    IF current_rev<>a.target_revision THEN
        UPDATE governance.catalog_publication_approvals SET state='expired'
         WHERE workspace_id=requested_workspace AND id=a.id AND state='approved';
        PERFORM governance.append_catalog_publication_audit(
          requested_workspace,a.id,a.target_kind,a.target_id,a.target_revision,current_rev,
          'approval_expired',NULL,at_time,'revision_drift','');
        RETURN NULL;
    END IF;
    PERFORM governance.assert_catalog_preflight(requested_workspace,requested_kind,requested_id,at_time);
    UPDATE governance.catalog_publication_approvals
       SET state='consumed',consumed_at=at_time
     WHERE workspace_id=requested_workspace AND id=a.id AND state='approved';
    PERFORM governance.append_catalog_publication_audit(
      requested_workspace,a.id,a.target_kind,a.target_id,a.target_revision,current_rev,
      'approval_consumed',NULL,at_time,'','');
    RETURN a.id;
END $$;

CREATE OR REPLACE FUNCTION catalog.publish_tool_version(requested_workspace text, requested_id text, at_time timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE v record; approval text;
BEGIN
    PERFORM catalog.assert_management_workspace(requested_workspace);
    IF EXISTS(SELECT 1 FROM catalog.tool_version_publish_issues(requested_workspace,requested_id,at_time)) THEN
        RAISE EXCEPTION 'ToolVersion publication preflight failed' USING ERRCODE='23514';
    END IF;
    SELECT * INTO v FROM catalog.tool_version_management
      WHERE workspace_id=requested_workspace AND tool_version_id=requested_id AND state='draft'
      FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'ToolVersion is not a draft' USING ERRCODE='23514';
    END IF;
    approval:=governance.consume_catalog_publication(requested_workspace,'tool_version',requested_id,at_time);
    IF approval IS NULL THEN RETURN; END IF;
    INSERT INTO catalog.tool_versions(
      id,tool_id,version,provider_id,price_version_id,deployment_revision,title,description,
      input_schema,output_schema,side_effect,idempotency,mcp_publishable,state,published_at)
    VALUES(v.tool_version_id,v.tool_id,v.version,v.provider_id,v.price_version_id,v.deployment_revision,
      v.title,v.description,v.input_schema,v.output_schema,v.side_effect,v.idempotency,v.mcp_publishable,'published',at_time);
    UPDATE catalog.tool_version_management
      SET state='published',published_at=at_time,updated_at=at_time
      WHERE workspace_id=requested_workspace AND tool_version_id=requested_id AND state='draft';
    PERFORM governance.append_catalog_publication_audit(
      requested_workspace,approval,'tool_version',requested_id,v.revision,v.revision,
      'publication_committed',NULL,at_time,'','');
END $$;

CREATE OR REPLACE FUNCTION distribution.publish_toolset(requested_workspace text, requested_id text, at_time timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE s record; approval text;
BEGIN
    PERFORM catalog.assert_management_workspace(requested_workspace);
    IF EXISTS(SELECT 1 FROM distribution.toolset_publish_issues(requested_workspace,requested_id,at_time)) THEN
        RAISE EXCEPTION 'Toolset publication preflight failed' USING ERRCODE='23514';
    END IF;
    SELECT * INTO s FROM distribution.toolsets
      WHERE workspace_id=requested_workspace AND id=requested_id AND state='draft' FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'Toolset is not a draft' USING ERRCODE='23514';
    END IF;
    approval:=governance.consume_catalog_publication(requested_workspace,'toolset',requested_id,at_time);
    IF approval IS NULL THEN RETURN; END IF;
    UPDATE distribution.toolset_bindings
      SET state='published',published_at=at_time
      WHERE workspace_id=requested_workspace AND toolset_version_id=requested_id AND state='draft';
    UPDATE distribution.toolsets
      SET state='published',published_at=at_time,updated_at=at_time
      WHERE workspace_id=requested_workspace AND id=requested_id AND state='draft';
    PERFORM governance.append_catalog_publication_audit(
      requested_workspace,approval,'toolset',requested_id,s.revision,s.revision,
      'publication_committed',NULL,at_time,'','');
END $$;

CREATE OR REPLACE FUNCTION catalog.retire_tool_version(requested_workspace text, requested_id text, at_time timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE v record;
BEGIN
    PERFORM catalog.assert_management_workspace(requested_workspace);
    SELECT * INTO v FROM catalog.tool_version_management
      WHERE workspace_id=requested_workspace AND tool_version_id=requested_id AND state='published'
      FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'ToolVersion is not published' USING ERRCODE='23514';
    END IF;
    IF EXISTS(SELECT 1 FROM distribution.toolset_bindings b
              WHERE b.workspace_id=requested_workspace AND b.tool_version_id=requested_id AND b.state='published') THEN
        RAISE EXCEPTION 'ToolVersion is bound by a published Toolset' USING ERRCODE='23514';
    END IF;
    UPDATE catalog.tool_versions SET state='retired' WHERE id=requested_id AND state='published';
    IF NOT FOUND THEN
        RAISE EXCEPTION 'published ToolVersion fact unavailable' USING ERRCODE='23514';
    END IF;
    UPDATE catalog.tool_version_management
      SET state='retired',retired_at=at_time,updated_at=at_time
      WHERE workspace_id=requested_workspace AND tool_version_id=requested_id AND state='published';
    PERFORM governance.append_catalog_publication_audit(
      requested_workspace,NULL,'tool_version',requested_id,v.revision,v.revision,
      'publication_retired',NULL,at_time,'','');
END $$;

CREATE OR REPLACE FUNCTION distribution.retire_toolset(requested_workspace text, requested_id text, at_time timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE s record;
BEGIN
    PERFORM catalog.assert_management_workspace(requested_workspace);
    SELECT * INTO s FROM distribution.toolsets
      WHERE workspace_id=requested_workspace AND id=requested_id AND state='published' FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'Toolset is not published' USING ERRCODE='23514';
    END IF;
    UPDATE distribution.toolset_bindings SET state='retired'
      WHERE workspace_id=requested_workspace AND toolset_version_id=requested_id AND state='published';
    UPDATE distribution.toolsets
      SET state='retired',retired_at=at_time,updated_at=at_time
      WHERE workspace_id=requested_workspace AND id=requested_id AND state='published';
    PERFORM governance.append_catalog_publication_audit(
      requested_workspace,NULL,'toolset',requested_id,s.revision,s.revision,
      'publication_retired',NULL,at_time,'','');
END $$;

REVOKE ALL ON governance.catalog_publication_audit_events FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.guard_catalog_publication_audit_immutable() FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.append_catalog_publication_audit(text,text,text,text,bigint,bigint,text,text,timestamptz,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.expire_catalog_publication_for_target_change(text,text,text,bigint,timestamptz,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.expire_catalog_publication_due_time(text,text,text,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.audit_tool_version_target_change() FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.audit_toolset_target_change() FROM PUBLIC;
