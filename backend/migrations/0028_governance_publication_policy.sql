-- Owner: governance owns publication policy revisions and decisions.
-- Alpha policies are deliberately declarative and bounded: no scripts, SQL,
-- expressions, secrets, arguments or quota/payment data are stored here.

CREATE TABLE governance.catalog_publication_policy_revisions (
    workspace_id text NOT NULL CHECK (workspace_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    id text NOT NULL CHECK (id ~ '^[A-Za-z0-9_-]{1,128}$'),
    revision bigint NOT NULL CHECK (revision > 0),
    state text NOT NULL CHECK (state IN ('draft','active','retired')),
    max_risk_level text NOT NULL CHECK (max_risk_level IN ('low','medium','high','critical')),
    deny_unsafe_write boolean NOT NULL,
    deny_mcp_unsafe_write boolean NOT NULL,
    created_by_user_id text CHECK (created_by_user_id IS NULL OR created_by_user_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    created_at timestamptz NOT NULL,
    activated_by_user_id text CHECK (activated_by_user_id IS NULL OR activated_by_user_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    activated_at timestamptz,
    retired_at timestamptz,
    PRIMARY KEY(workspace_id,id),
    UNIQUE(workspace_id,revision),
    CHECK ((state='draft' AND activated_by_user_id IS NULL AND activated_at IS NULL AND retired_at IS NULL)
        OR (state='active' AND activated_at IS NOT NULL AND retired_at IS NULL)
        OR (state='retired' AND activated_at IS NOT NULL AND retired_at IS NOT NULL)),
    CHECK (activated_at IS NULL OR activated_at>=created_at),
    CHECK (retired_at IS NULL OR retired_at>=activated_at)
);
CREATE UNIQUE INDEX one_active_catalog_publication_policy
    ON governance.catalog_publication_policy_revisions(workspace_id)
    WHERE state='active';

ALTER TABLE governance.catalog_publication_policy_revisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE governance.catalog_publication_policy_revisions FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON governance.catalog_publication_policy_revisions
    USING(workspace_id=nullif(current_setting('mender.workspace_id',true),''))
    WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));

CREATE FUNCTION governance.guard_catalog_publication_policy_revision() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        RAISE EXCEPTION 'publication policy revisions are immutable' USING ERRCODE='42501';
    END IF;
    IF NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.id IS DISTINCT FROM OLD.id
       OR NEW.revision IS DISTINCT FROM OLD.revision OR NEW.max_risk_level IS DISTINCT FROM OLD.max_risk_level
       OR NEW.deny_unsafe_write IS DISTINCT FROM OLD.deny_unsafe_write
       OR NEW.deny_mcp_unsafe_write IS DISTINCT FROM OLD.deny_mcp_unsafe_write
       OR NEW.created_by_user_id IS DISTINCT FROM OLD.created_by_user_id
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'publication policy revision rules are immutable' USING ERRCODE='42501';
    END IF;
    IF NOT ((OLD.state='draft' AND NEW.state='active' AND NEW.activated_at IS NOT NULL AND NEW.retired_at IS NULL)
         OR (OLD.state='active' AND NEW.state='retired' AND NEW.activated_at=OLD.activated_at AND NEW.retired_at IS NOT NULL)) THEN
        RAISE EXCEPTION 'invalid publication policy lifecycle transition' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER guard_catalog_publication_policy_revision
    BEFORE UPDATE OR DELETE ON governance.catalog_publication_policy_revisions
    FOR EACH ROW EXECUTE FUNCTION governance.guard_catalog_publication_policy_revision();

CREATE TABLE governance.catalog_publication_policy_decisions (
    sequence bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    workspace_id text NOT NULL CHECK (workspace_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    policy_revision_id text NOT NULL CHECK (policy_revision_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    policy_revision bigint NOT NULL CHECK (policy_revision > 0),
    target_kind text NOT NULL CHECK (target_kind IN ('tool_version','toolset')),
    target_id text NOT NULL CHECK (target_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    target_revision bigint NOT NULL CHECK (target_revision > 0),
    risk_level text NOT NULL CHECK (risk_level IN ('low','medium','high','critical')),
    outcome text NOT NULL CHECK (outcome IN ('allow','deny')),
    reason_codes text[] NOT NULL CHECK (
        cardinality(reason_codes) > 0
        AND array_position(reason_codes,NULL) IS NULL
        AND reason_codes <@ ARRAY[
          'tool_read_only_safe','tool_read_only_non_safe','tool_write_idempotent','tool_write_unsafe','tool_write_contract_mismatch',
          'toolset_empty','within_risk_ceiling','risk_above_ceiling','unsafe_write_denied','mcp_unsafe_write_denied'
        ]::text[]
    ),
    evaluated_at timestamptz NOT NULL
);
CREATE INDEX catalog_publication_policy_decisions_workspace_sequence
    ON governance.catalog_publication_policy_decisions(workspace_id,sequence DESC);
CREATE INDEX catalog_publication_policy_decisions_target_sequence
    ON governance.catalog_publication_policy_decisions(workspace_id,target_kind,target_id,sequence DESC);

ALTER TABLE governance.catalog_publication_policy_decisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE governance.catalog_publication_policy_decisions FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON governance.catalog_publication_policy_decisions
    USING(workspace_id=nullif(current_setting('mender.workspace_id',true),''))
    WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));

CREATE FUNCTION governance.guard_catalog_publication_policy_decision() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
    RAISE EXCEPTION 'publication policy decisions are immutable' USING ERRCODE='42501';
END $$;
CREATE TRIGGER immutable_catalog_publication_policy_decision
    BEFORE UPDATE OR DELETE ON governance.catalog_publication_policy_decisions
    FOR EACH ROW EXECUTE FUNCTION governance.guard_catalog_publication_policy_decision();

-- Preserve existing publication behavior for workspaces that already exist at
-- migration time. Future workspaces fail closed until an explicit policy is
-- created and activated through the reviewed governance policy surface.
INSERT INTO governance.catalog_publication_policy_revisions(
  workspace_id,id,revision,state,max_risk_level,deny_unsafe_write,deny_mcp_unsafe_write,
  created_by_user_id,created_at,activated_by_user_id,activated_at)
SELECT id,'policy_baseline_v1',1,'active','critical',false,false,NULL,clock_timestamp(),NULL,clock_timestamp()
FROM identity.workspaces;

CREATE FUNCTION governance.risk_rank(level text) RETURNS integer
LANGUAGE sql IMMUTABLE STRICT SET search_path=pg_catalog AS $$
SELECT CASE level WHEN 'low' THEN 1 WHEN 'medium' THEN 2 WHEN 'high' THEN 3 WHEN 'critical' THEN 4 ELSE 100 END
$$;

CREATE FUNCTION governance.catalog_publication_risk_facts(
    requested_workspace text,requested_kind text,requested_id text)
RETURNS TABLE(risk_level text,base_reason text,unsafe_write boolean,mcp_unsafe_write boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE v record; max_rank integer; unsafe_any boolean; mcp_unsafe_any boolean; row_count integer;
BEGIN
    PERFORM catalog.assert_management_workspace(requested_workspace);
    IF requested_kind='tool_version' THEN
        SELECT side_effect,idempotency INTO v
          FROM catalog.tool_version_management
         WHERE workspace_id=requested_workspace AND tool_version_id=requested_id AND state='draft';
        IF NOT FOUND THEN RAISE EXCEPTION 'publication risk target unavailable' USING ERRCODE='23514'; END IF;
        unsafe_write := v.side_effect='write' AND v.idempotency='unsafe';
        mcp_unsafe_write := false;
        IF v.side_effect='read_only' AND v.idempotency='safe_read' THEN risk_level:='low'; base_reason:='tool_read_only_safe';
        ELSIF v.side_effect='read_only' THEN risk_level:='medium'; base_reason:='tool_read_only_non_safe';
        ELSIF v.side_effect='write' AND v.idempotency='idempotent' THEN risk_level:='high'; base_reason:='tool_write_idempotent';
        ELSIF v.side_effect='write' AND v.idempotency='unsafe' THEN risk_level:='critical'; base_reason:='tool_write_unsafe';
        ELSE risk_level:='critical'; base_reason:='tool_write_contract_mismatch'; END IF;
        RETURN NEXT;
        RETURN;
    ELSIF requested_kind='toolset' THEN
        SELECT count(*),
               max(CASE
                 WHEN tv.side_effect='read_only' AND tv.idempotency='safe_read' THEN 1
                 WHEN tv.side_effect='read_only' THEN 2
                 WHEN tv.side_effect='write' AND tv.idempotency='idempotent' THEN 3
                 ELSE 4 END),
               coalesce(bool_or(tv.side_effect='write' AND tv.idempotency='unsafe'),false),
               coalesce(bool_or(b.mcp_exposed AND tv.side_effect='write' AND tv.idempotency='unsafe'),false)
          INTO row_count,max_rank,unsafe_any,mcp_unsafe_any
          FROM distribution.toolset_bindings b
          LEFT JOIN catalog.tool_versions tv ON tv.id=b.tool_version_id AND tv.state='published'
         WHERE b.workspace_id=requested_workspace AND b.toolset_version_id=requested_id AND b.state='draft';
        IF row_count=0 THEN
            risk_level:='critical'; base_reason:='toolset_empty'; unsafe_write:=false; mcp_unsafe_write:=false; RETURN NEXT; RETURN;
        END IF;
        IF EXISTS(SELECT 1 FROM distribution.toolset_bindings b LEFT JOIN catalog.tool_versions tv ON tv.id=b.tool_version_id AND tv.state='published'
                  WHERE b.workspace_id=requested_workspace AND b.toolset_version_id=requested_id AND b.state='draft' AND tv.id IS NULL) THEN
            RAISE EXCEPTION 'publication risk facts unavailable' USING ERRCODE='23514';
        END IF;
        risk_level:=CASE max_rank WHEN 1 THEN 'low' WHEN 2 THEN 'medium' WHEN 3 THEN 'high' ELSE 'critical' END;
        base_reason:=CASE max_rank WHEN 1 THEN 'tool_read_only_safe' WHEN 2 THEN 'tool_read_only_non_safe' WHEN 3 THEN 'tool_write_idempotent' ELSE 'tool_write_unsafe' END;
        unsafe_write:=unsafe_any; mcp_unsafe_write:=mcp_unsafe_any;
        RETURN NEXT;
        RETURN;
    END IF;
    RAISE EXCEPTION 'unsupported publication target' USING ERRCODE='22023';
END $$;

CREATE FUNCTION governance.evaluate_catalog_publication_policy(
    requested_workspace text,requested_kind text,requested_id text,at_time timestamptz) RETURNS bigint
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE p record; facts record; target_rev bigint; reasons text[]; result text; seq bigint;
BEGIN
    PERFORM catalog.assert_management_workspace(requested_workspace);
    SELECT * INTO p FROM governance.catalog_publication_policy_revisions
     WHERE workspace_id=requested_workspace AND state='active';
    IF NOT FOUND THEN RAISE EXCEPTION 'active publication policy is required' USING ERRCODE='23514'; END IF;
    target_rev:=governance.catalog_target_revision(requested_workspace,requested_kind,requested_id);
    SELECT * INTO facts FROM governance.catalog_publication_risk_facts(requested_workspace,requested_kind,requested_id);
    reasons:=ARRAY[facts.base_reason]::text[];
    result:='allow';
    IF governance.risk_rank(facts.risk_level)>governance.risk_rank(p.max_risk_level) THEN
        result:='deny'; reasons:=array_append(reasons,'risk_above_ceiling');
    ELSE
        reasons:=array_append(reasons,'within_risk_ceiling');
    END IF;
    IF p.deny_unsafe_write AND facts.unsafe_write THEN
        result:='deny'; reasons:=array_append(reasons,'unsafe_write_denied');
    END IF;
    IF p.deny_mcp_unsafe_write AND facts.mcp_unsafe_write THEN
        result:='deny'; reasons:=array_append(reasons,'mcp_unsafe_write_denied');
    END IF;
    INSERT INTO governance.catalog_publication_policy_decisions(
      workspace_id,policy_revision_id,policy_revision,target_kind,target_id,target_revision,
      risk_level,outcome,reason_codes,evaluated_at)
    VALUES(requested_workspace,p.id,p.revision,requested_kind,requested_id,target_rev,
      facts.risk_level,result,reasons,at_time)
    RETURNING sequence INTO seq;
    RETURN seq;
END $$;

CREATE FUNCTION governance.create_catalog_publication_policy(
    requested_workspace text,policy_id text,actor_id text,max_risk text,
    deny_unsafe boolean,deny_mcp_unsafe boolean,at_time timestamptz) RETURNS bigint
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE next_revision bigint;
BEGIN
    PERFORM catalog.assert_management_workspace(requested_workspace);
    IF actor_id !~ '^[A-Za-z0-9_-]{1,128}$' OR policy_id !~ '^[A-Za-z0-9_-]{1,128}$'
       OR max_risk NOT IN ('low','medium','high','critical') THEN
        RAISE EXCEPTION 'invalid publication policy' USING ERRCODE='22023';
    END IF;
    SELECT coalesce(max(revision),0)+1 INTO next_revision
      FROM governance.catalog_publication_policy_revisions WHERE workspace_id=requested_workspace;
    INSERT INTO governance.catalog_publication_policy_revisions(
      workspace_id,id,revision,state,max_risk_level,deny_unsafe_write,deny_mcp_unsafe_write,
      created_by_user_id,created_at)
    VALUES(requested_workspace,policy_id,next_revision,'draft',max_risk,deny_unsafe,deny_mcp_unsafe,actor_id,at_time);
    RETURN next_revision;
END $$;

CREATE FUNCTION governance.expire_catalog_publication_due_policy_change(
    requested_workspace text,at_time timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE a record;
BEGIN
    FOR a IN
        SELECT * FROM governance.catalog_publication_approvals
         WHERE workspace_id=requested_workspace AND state IN ('pending','approved')
         ORDER BY requested_at,id FOR UPDATE
    LOOP
        UPDATE governance.catalog_publication_approvals SET state='expired'
         WHERE workspace_id=requested_workspace AND id=a.id AND state IN ('pending','approved');
        PERFORM governance.append_catalog_publication_audit(
          requested_workspace,a.id,a.target_kind,a.target_id,a.target_revision,a.target_revision,
          'approval_expired',NULL,at_time,'policy_revision_changed','');
    END LOOP;
END $$;

CREATE FUNCTION governance.activate_catalog_publication_policy(
    requested_workspace text,policy_id text,actor_id text,at_time timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE candidate record;
BEGIN
    PERFORM catalog.assert_management_workspace(requested_workspace);
    IF actor_id !~ '^[A-Za-z0-9_-]{1,128}$' THEN
        RAISE EXCEPTION 'invalid publication policy actor' USING ERRCODE='22023';
    END IF;
    SELECT * INTO candidate FROM governance.catalog_publication_policy_revisions
     WHERE workspace_id=requested_workspace AND id=policy_id FOR UPDATE;
    IF NOT FOUND OR candidate.state<>'draft' THEN
        RAISE EXCEPTION 'publication policy is not draft' USING ERRCODE='23514';
    END IF;
    UPDATE governance.catalog_publication_policy_revisions
       SET state='retired',retired_at=at_time
     WHERE workspace_id=requested_workspace AND state='active';
    UPDATE governance.catalog_publication_policy_revisions
       SET state='active',activated_by_user_id=actor_id,activated_at=at_time
     WHERE workspace_id=requested_workspace AND id=policy_id AND state='draft';
    PERFORM governance.expire_catalog_publication_due_policy_change(requested_workspace,at_time);
END $$;

REVOKE ALL ON governance.catalog_publication_policy_revisions FROM PUBLIC;
REVOKE ALL ON governance.catalog_publication_policy_decisions FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.guard_catalog_publication_policy_revision() FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.guard_catalog_publication_policy_decision() FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.risk_rank(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.catalog_publication_risk_facts(text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.evaluate_catalog_publication_policy(text,text,text,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.create_catalog_publication_policy(text,text,text,text,boolean,boolean,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.expire_catalog_publication_due_policy_change(text,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.activate_catalog_publication_policy(text,text,text,timestamptz) FROM PUBLIC;
