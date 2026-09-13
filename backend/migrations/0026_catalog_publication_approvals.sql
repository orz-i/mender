-- Owner: governance owns publication approval requests and decisions.
-- Catalog/Distribution still own publication facts. Approval is a maker/checker
-- capability bound to an exact draft revision; it is not billing authority.

ALTER TABLE catalog.tool_version_management
    ADD COLUMN revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0);

CREATE OR REPLACE FUNCTION catalog.guard_management_tool_version() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
DECLARE material_changed boolean;
BEGIN
    material_changed :=
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
        NEW.mcp_publishable IS DISTINCT FROM OLD.mcp_publishable;
    IF TG_OP='UPDATE' AND OLD.state<>'draft' AND material_changed THEN
        RAISE EXCEPTION 'published ToolVersion management facts are immutable' USING ERRCODE='23514';
    END IF;
    IF TG_OP='DELETE' AND OLD.state<>'draft' THEN
        RAISE EXCEPTION 'published ToolVersion management facts cannot be deleted' USING ERRCODE='23514';
    END IF;
    IF TG_OP='UPDATE' THEN
        IF OLD.state='draft' AND material_changed THEN
            NEW.revision:=OLD.revision+1;
        ELSE
            NEW.revision:=OLD.revision;
        END IF;
        IF NEW.state=OLD.state THEN NEW.updated_at:=clock_timestamp(); END IF;
    END IF;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END $$;

ALTER TABLE distribution.toolsets
    ADD COLUMN revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0);

CREATE FUNCTION distribution.bump_draft_toolset_revision() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE w text; s text; changed boolean;
BEGIN
    IF TG_OP='DELETE' THEN
        w:=OLD.workspace_id; s:=OLD.toolset_version_id; changed:=true;
    ELSIF TG_OP='INSERT' THEN
        w:=NEW.workspace_id; s:=NEW.toolset_version_id; changed:=true;
    ELSE
        w:=NEW.workspace_id; s:=NEW.toolset_version_id;
        changed :=
            NEW.tool_id IS DISTINCT FROM OLD.tool_id OR
            NEW.tool_version_label IS DISTINCT FROM OLD.tool_version_label OR
            NEW.tool_version_id IS DISTINCT FROM OLD.tool_version_id OR
            NEW.budget_id IS DISTINCT FROM OLD.budget_id OR
            NEW.connection_id IS DISTINCT FROM OLD.connection_id OR
            NEW.mcp_name IS DISTINCT FROM OLD.mcp_name OR
            NEW.mcp_exposed IS DISTINCT FROM OLD.mcp_exposed;
    END IF;
    IF changed THEN
        UPDATE distribution.toolsets
           SET revision=revision+1,updated_at=clock_timestamp()
         WHERE workspace_id=w AND id=s AND state='draft';
    END IF;
    RETURN NULL;
END $$;
CREATE TRIGGER bump_draft_toolset_revision
    AFTER INSERT OR UPDATE OR DELETE ON distribution.toolset_bindings
    FOR EACH ROW EXECUTE FUNCTION distribution.bump_draft_toolset_revision();

CREATE SCHEMA governance;
CREATE TABLE governance.catalog_publication_approvals (
    workspace_id text NOT NULL CHECK (workspace_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    id text NOT NULL CHECK (id ~ '^[A-Za-z0-9_-]{1,128}$'),
    target_kind text NOT NULL CHECK (target_kind IN ('tool_version','toolset')),
    target_id text NOT NULL CHECK (target_id ~ '^[A-Za-z0-9_-]{1,128}$'),
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
CREATE UNIQUE INDEX one_active_catalog_publication_approval
    ON governance.catalog_publication_approvals(workspace_id,target_kind,target_id)
    WHERE state IN ('pending','approved');
ALTER TABLE governance.catalog_publication_approvals ENABLE ROW LEVEL SECURITY;
ALTER TABLE governance.catalog_publication_approvals FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON governance.catalog_publication_approvals
    USING(workspace_id=nullif(current_setting('mender.workspace_id',true),''))
    WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));

CREATE FUNCTION governance.catalog_target_revision(requested_workspace text, requested_kind text, requested_id text)
RETURNS bigint LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE r bigint; s text;
BEGIN
    PERFORM catalog.assert_management_workspace(requested_workspace);
    IF requested_kind='tool_version' THEN
        SELECT revision,state INTO r,s FROM catalog.tool_version_management
         WHERE workspace_id=requested_workspace AND tool_version_id=requested_id;
    ELSIF requested_kind='toolset' THEN
        SELECT revision,state INTO r,s FROM distribution.toolsets
         WHERE workspace_id=requested_workspace AND id=requested_id;
    ELSE
        RAISE EXCEPTION 'unsupported publication target' USING ERRCODE='22023';
    END IF;
    IF r IS NULL OR s<>'draft' THEN
        RAISE EXCEPTION 'publication target is not a draft' USING ERRCODE='23514';
    END IF;
    RETURN r;
END $$;

CREATE FUNCTION governance.assert_catalog_preflight(requested_workspace text, requested_kind text, requested_id text, at_time timestamptz)
RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
    IF requested_kind='tool_version' THEN
        IF EXISTS(SELECT 1 FROM catalog.tool_version_publish_issues(requested_workspace,requested_id,at_time)) THEN
            RAISE EXCEPTION 'ToolVersion publication preflight failed' USING ERRCODE='23514';
        END IF;
    ELSIF requested_kind='toolset' THEN
        IF EXISTS(SELECT 1 FROM distribution.toolset_publish_issues(requested_workspace,requested_id,at_time)) THEN
            RAISE EXCEPTION 'Toolset publication preflight failed' USING ERRCODE='23514';
        END IF;
    ELSE
        RAISE EXCEPTION 'unsupported publication target' USING ERRCODE='22023';
    END IF;
END $$;

CREATE FUNCTION governance.submit_catalog_publication(
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
    UPDATE governance.catalog_publication_approvals
       SET state='expired'
     WHERE workspace_id=requested_workspace AND target_kind=requested_kind AND target_id=requested_id
       AND state IN ('pending','approved')
       AND (expires_at<=at_time OR target_revision<>target_rev);
    IF EXISTS(SELECT 1 FROM governance.catalog_publication_approvals
              WHERE workspace_id=requested_workspace AND target_kind=requested_kind AND target_id=requested_id
                AND state IN ('pending','approved')) THEN
        RAISE EXCEPTION 'active publication approval already exists' USING ERRCODE='23505';
    END IF;
    INSERT INTO governance.catalog_publication_approvals(
      workspace_id,id,target_kind,target_id,target_revision,requester_user_id,state,requested_at,expires_at)
    VALUES(requested_workspace,request_id,requested_kind,requested_id,target_rev,requester_id,'pending',at_time,expiry_time);
END $$;

CREATE FUNCTION governance.approve_catalog_publication(
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
        RAISE EXCEPTION 'publication approval expired' USING ERRCODE='23514';
    END IF;
    current_rev:=governance.catalog_target_revision(requested_workspace,a.target_kind,a.target_id);
    IF current_rev<>a.target_revision THEN
        RAISE EXCEPTION 'publication target changed after submission' USING ERRCODE='23514';
    END IF;
    PERFORM governance.assert_catalog_preflight(requested_workspace,a.target_kind,a.target_id,at_time);
    UPDATE governance.catalog_publication_approvals
       SET state='approved',reviewer_user_id=reviewer_id,reviewed_at=at_time,decision_note=coalesce(note,'')
     WHERE workspace_id=requested_workspace AND id=request_id AND state='pending';
END $$;

CREATE FUNCTION governance.reject_catalog_publication(
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
        RAISE EXCEPTION 'publication approval expired' USING ERRCODE='23514';
    END IF;
    current_rev:=governance.catalog_target_revision(requested_workspace,a.target_kind,a.target_id);
    IF current_rev<>a.target_revision THEN
        RAISE EXCEPTION 'publication target changed after submission' USING ERRCODE='23514';
    END IF;
    UPDATE governance.catalog_publication_approvals
       SET state='rejected',reviewer_user_id=reviewer_id,reviewed_at=at_time,decision_note=coalesce(note,'')
     WHERE workspace_id=requested_workspace AND id=request_id AND state='pending';
END $$;

CREATE FUNCTION governance.consume_catalog_publication(
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
        RAISE EXCEPTION 'publication approval expired' USING ERRCODE='23514';
    END IF;
    current_rev:=governance.catalog_target_revision(requested_workspace,requested_kind,requested_id);
    IF current_rev<>a.target_revision THEN
        RAISE EXCEPTION 'publication target changed after approval' USING ERRCODE='23514';
    END IF;
    PERFORM governance.assert_catalog_preflight(requested_workspace,requested_kind,requested_id,at_time);
    UPDATE governance.catalog_publication_approvals
       SET state='consumed',consumed_at=at_time
     WHERE workspace_id=requested_workspace AND id=a.id AND state='approved';
    RETURN a.id;
END $$;

CREATE OR REPLACE FUNCTION catalog.publish_tool_version(requested_workspace text, requested_id text, at_time timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE v record;
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
    PERFORM governance.consume_catalog_publication(requested_workspace,'tool_version',requested_id,at_time);
    INSERT INTO catalog.tool_versions(
      id,tool_id,version,provider_id,price_version_id,deployment_revision,title,description,
      input_schema,output_schema,side_effect,idempotency,mcp_publishable,state,published_at)
    VALUES(v.tool_version_id,v.tool_id,v.version,v.provider_id,v.price_version_id,v.deployment_revision,
      v.title,v.description,v.input_schema,v.output_schema,v.side_effect,v.idempotency,v.mcp_publishable,'published',at_time);
    UPDATE catalog.tool_version_management
      SET state='published',published_at=at_time,updated_at=at_time
      WHERE workspace_id=requested_workspace AND tool_version_id=requested_id AND state='draft';
END $$;

CREATE OR REPLACE FUNCTION distribution.publish_toolset(requested_workspace text, requested_id text, at_time timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
    PERFORM catalog.assert_management_workspace(requested_workspace);
    IF EXISTS(SELECT 1 FROM distribution.toolset_publish_issues(requested_workspace,requested_id,at_time)) THEN
        RAISE EXCEPTION 'Toolset publication preflight failed' USING ERRCODE='23514';
    END IF;
    PERFORM 1 FROM distribution.toolsets
      WHERE workspace_id=requested_workspace AND id=requested_id AND state='draft' FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'Toolset is not a draft' USING ERRCODE='23514';
    END IF;
    PERFORM governance.consume_catalog_publication(requested_workspace,'toolset',requested_id,at_time);
    UPDATE distribution.toolset_bindings
      SET state='published',published_at=at_time
      WHERE workspace_id=requested_workspace AND toolset_version_id=requested_id AND state='draft';
    UPDATE distribution.toolsets
      SET state='published',published_at=at_time,updated_at=at_time
      WHERE workspace_id=requested_workspace AND id=requested_id AND state='draft';
END $$;

REVOKE ALL ON SCHEMA governance FROM PUBLIC;
REVOKE ALL ON governance.catalog_publication_approvals FROM PUBLIC;
REVOKE ALL ON FUNCTION distribution.bump_draft_toolset_revision() FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.catalog_target_revision(text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.assert_catalog_preflight(text,text,text,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.submit_catalog_publication(text,text,text,text,text,timestamptz,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.approve_catalog_publication(text,text,text,timestamptz,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.reject_catalog_publication(text,text,text,timestamptz,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.consume_catalog_publication(text,text,text,timestamptz) FROM PUBLIC;
