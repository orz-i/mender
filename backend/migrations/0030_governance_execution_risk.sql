-- Owner: governance. Execution-risk policy is evaluated after immutable
-- ToolVersion resolution and before Admission reserves quota or creates Run/Job.
-- Decisions and confirmations contain hashes/identifiers only; raw arguments,
-- secrets, quota amounts and payment/accounting facts never enter Governance.

CREATE TABLE governance.execution_policy_revisions (
    workspace_id text NOT NULL CHECK (workspace_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    id text NOT NULL CHECK (id ~ '^[A-Za-z0-9_-]{1,128}$'),
    revision bigint NOT NULL CHECK (revision > 0),
    state text NOT NULL CHECK (state IN ('draft','active','retired')),
    max_unconfirmed_risk_level text NOT NULL CHECK (max_unconfirmed_risk_level IN ('low','medium','high','critical')),
    deny_unsafe_write boolean NOT NULL,
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
CREATE UNIQUE INDEX one_active_execution_policy
    ON governance.execution_policy_revisions(workspace_id)
    WHERE state='active';

ALTER TABLE governance.execution_policy_revisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE governance.execution_policy_revisions FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON governance.execution_policy_revisions
    USING(workspace_id=nullif(current_setting('mender.workspace_id',true),''))
    WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));

CREATE FUNCTION governance.guard_execution_policy_revision() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        RAISE EXCEPTION 'execution policy revisions are immutable' USING ERRCODE='42501';
    END IF;
    IF NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.id IS DISTINCT FROM OLD.id
       OR NEW.revision IS DISTINCT FROM OLD.revision
       OR NEW.max_unconfirmed_risk_level IS DISTINCT FROM OLD.max_unconfirmed_risk_level
       OR NEW.deny_unsafe_write IS DISTINCT FROM OLD.deny_unsafe_write
       OR NEW.created_by_user_id IS DISTINCT FROM OLD.created_by_user_id
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'execution policy revision rules are immutable' USING ERRCODE='42501';
    END IF;
    IF NOT ((OLD.state='draft' AND NEW.state='active' AND NEW.activated_at IS NOT NULL AND NEW.retired_at IS NULL)
         OR (OLD.state='active' AND NEW.state='retired' AND NEW.activated_at=OLD.activated_at AND NEW.retired_at IS NOT NULL)) THEN
        RAISE EXCEPTION 'invalid execution policy lifecycle transition' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER guard_execution_policy_revision
    BEFORE UPDATE OR DELETE ON governance.execution_policy_revisions
    FOR EACH ROW EXECUTE FUNCTION governance.guard_execution_policy_revision();

CREATE TABLE governance.execution_policy_decisions (
    sequence bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    workspace_id text NOT NULL CHECK (workspace_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    policy_revision_id text NOT NULL CHECK (policy_revision_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    policy_revision bigint NOT NULL CHECK (policy_revision > 0),
    subject_kind text NOT NULL CHECK (subject_kind IN ('human','machine')),
    subject_id text NOT NULL CHECK (subject_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    toolset_version_id text NOT NULL CHECK (toolset_version_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    tool_version_id text NOT NULL CHECK (tool_version_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    connection_id text NOT NULL CHECK (connection_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    arguments_hash text NOT NULL CHECK (arguments_hash ~ '^[a-f0-9]{64}$'),
    idempotency_key_hash text NOT NULL CHECK (idempotency_key_hash ~ '^[a-f0-9]{64}$'),
    risk_level text NOT NULL CHECK (risk_level IN ('low','medium','high','critical')),
    outcome text NOT NULL CHECK (outcome IN ('allow','confirmation_required','deny')),
    reason_codes text[] NOT NULL CHECK (
        cardinality(reason_codes) > 0
        AND array_position(reason_codes,NULL) IS NULL
        AND reason_codes <@ ARRAY[
          'tool_read_only_safe','tool_read_only_non_safe','tool_write_idempotent','tool_write_unsafe','tool_write_contract_mismatch',
          'within_unconfirmed_risk','human_confirmation_required','machine_confirmation_unavailable','unsafe_write_denied'
        ]::text[]
    ),
    evaluated_at timestamptz NOT NULL
);
CREATE INDEX execution_policy_decisions_workspace_sequence
    ON governance.execution_policy_decisions(workspace_id,sequence DESC);
CREATE INDEX execution_policy_decisions_request
    ON governance.execution_policy_decisions(workspace_id,toolset_version_id,tool_version_id,connection_id,arguments_hash,sequence DESC);

ALTER TABLE governance.execution_policy_decisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE governance.execution_policy_decisions FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON governance.execution_policy_decisions
    USING(workspace_id=nullif(current_setting('mender.workspace_id',true),''))
    WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));

CREATE FUNCTION governance.guard_execution_policy_decision() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
    RAISE EXCEPTION 'execution policy decisions are immutable' USING ERRCODE='42501';
END $$;
CREATE TRIGGER immutable_execution_policy_decision
    BEFORE UPDATE OR DELETE ON governance.execution_policy_decisions
    FOR EACH ROW EXECUTE FUNCTION governance.guard_execution_policy_decision();

CREATE TABLE governance.execution_confirmations (
    workspace_id text NOT NULL CHECK (workspace_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    id text NOT NULL CHECK (id ~ '^[A-Za-z0-9_-]{1,128}$'),
    user_id text NOT NULL CHECK (user_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    policy_revision_id text NOT NULL CHECK (policy_revision_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    policy_revision bigint NOT NULL CHECK (policy_revision > 0),
    toolset_version_id text NOT NULL CHECK (toolset_version_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    tool_version_id text NOT NULL CHECK (tool_version_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    connection_id text NOT NULL CHECK (connection_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    arguments_hash text NOT NULL CHECK (arguments_hash ~ '^[a-f0-9]{64}$'),
    idempotency_key_hash text NOT NULL CHECK (idempotency_key_hash ~ '^[a-f0-9]{64}$'),
    risk_level text NOT NULL CHECK (risk_level IN ('low','medium','high','critical')),
    state text NOT NULL CHECK (state IN ('active','consumed','expired')),
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL CHECK (expires_at>created_at),
    consumed_at timestamptz,
    expired_at timestamptz,
    PRIMARY KEY(workspace_id,id),
    CHECK ((state='active' AND consumed_at IS NULL AND expired_at IS NULL)
        OR (state='consumed' AND consumed_at IS NOT NULL AND expired_at IS NULL)
        OR (state='expired' AND consumed_at IS NULL AND expired_at IS NOT NULL)),
    CHECK (consumed_at IS NULL OR consumed_at>=created_at),
    CHECK (expired_at IS NULL OR expired_at>=created_at)
);
CREATE UNIQUE INDEX one_active_execution_confirmation
    ON governance.execution_confirmations(
      workspace_id,user_id,policy_revision,toolset_version_id,tool_version_id,connection_id,arguments_hash,idempotency_key_hash)
    WHERE state='active';
CREATE INDEX execution_confirmations_workspace_expiry
    ON governance.execution_confirmations(workspace_id,expires_at)
    WHERE state='active';

ALTER TABLE governance.execution_confirmations ENABLE ROW LEVEL SECURITY;
ALTER TABLE governance.execution_confirmations FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON governance.execution_confirmations
    USING(workspace_id=nullif(current_setting('mender.workspace_id',true),''))
    WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));

CREATE FUNCTION governance.guard_execution_confirmation() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        RAISE EXCEPTION 'execution confirmations are immutable' USING ERRCODE='42501';
    END IF;
    IF NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.id IS DISTINCT FROM OLD.id
       OR NEW.user_id IS DISTINCT FROM OLD.user_id OR NEW.policy_revision_id IS DISTINCT FROM OLD.policy_revision_id
       OR NEW.policy_revision IS DISTINCT FROM OLD.policy_revision OR NEW.toolset_version_id IS DISTINCT FROM OLD.toolset_version_id
       OR NEW.tool_version_id IS DISTINCT FROM OLD.tool_version_id OR NEW.connection_id IS DISTINCT FROM OLD.connection_id
       OR NEW.arguments_hash IS DISTINCT FROM OLD.arguments_hash OR NEW.idempotency_key_hash IS DISTINCT FROM OLD.idempotency_key_hash
       OR NEW.risk_level IS DISTINCT FROM OLD.risk_level OR NEW.created_at IS DISTINCT FROM OLD.created_at
       OR NEW.expires_at IS DISTINCT FROM OLD.expires_at THEN
        RAISE EXCEPTION 'execution confirmation binding is immutable' USING ERRCODE='42501';
    END IF;
    IF NOT ((OLD.state='active' AND NEW.state='consumed' AND NEW.consumed_at IS NOT NULL AND NEW.expired_at IS NULL)
         OR (OLD.state='active' AND NEW.state='expired' AND NEW.expired_at IS NOT NULL AND NEW.consumed_at IS NULL)) THEN
        RAISE EXCEPTION 'invalid execution confirmation lifecycle transition' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER guard_execution_confirmation
    BEFORE UPDATE OR DELETE ON governance.execution_confirmations
    FOR EACH ROW EXECUTE FUNCTION governance.guard_execution_confirmation();

-- Compatibility baseline for already-existing workspaces. New workspaces fail
-- closed until an execution policy is explicitly created and activated.
INSERT INTO governance.execution_policy_revisions(
  workspace_id,id,revision,state,max_unconfirmed_risk_level,deny_unsafe_write,
  created_by_user_id,created_at,activated_by_user_id,activated_at)
SELECT id,'execution_policy_baseline_v1',1,'active','critical',false,
       NULL,clock_timestamp(),NULL,clock_timestamp()
FROM identity.workspaces;

CREATE FUNCTION governance.assert_execution_workspace(requested_workspace text) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
    IF requested_workspace !~ '^[A-Za-z0-9_-]{1,128}$'
       OR requested_workspace IS DISTINCT FROM nullif(current_setting('mender.workspace_id',true),'') THEN
        RAISE EXCEPTION 'workspace scope mismatch' USING ERRCODE='42501';
    END IF;
    IF NOT EXISTS(SELECT 1 FROM identity.workspaces WHERE id=requested_workspace AND NOT disabled) THEN
        RAISE EXCEPTION 'workspace unavailable' USING ERRCODE='42501';
    END IF;
END $$;

CREATE FUNCTION governance.execution_risk_facts(
    requested_workspace text,requested_toolset text,requested_tool_version text)
RETURNS TABLE(risk_level text,base_reason text,unsafe_write boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE v record;
BEGIN
    PERFORM governance.assert_execution_workspace(requested_workspace);
    IF NOT EXISTS(
        SELECT 1 FROM distribution.toolset_bindings b
         WHERE b.workspace_id=requested_workspace AND b.toolset_version_id=requested_toolset
           AND b.tool_version_id=requested_tool_version AND b.state='published') THEN
        RAISE EXCEPTION 'execution policy target unavailable' USING ERRCODE='23514';
    END IF;
    SELECT side_effect,idempotency INTO v
      FROM catalog.tool_versions
     WHERE id=requested_tool_version AND state='published';
    IF NOT FOUND THEN
        RAISE EXCEPTION 'execution policy ToolVersion unavailable' USING ERRCODE='23514';
    END IF;
    unsafe_write:=v.side_effect='write' AND v.idempotency='unsafe';
    IF v.side_effect='read_only' AND v.idempotency='safe_read' THEN risk_level:='low'; base_reason:='tool_read_only_safe';
    ELSIF v.side_effect='read_only' THEN risk_level:='medium'; base_reason:='tool_read_only_non_safe';
    ELSIF v.side_effect='write' AND v.idempotency='idempotent' THEN risk_level:='high'; base_reason:='tool_write_idempotent';
    ELSIF v.side_effect='write' AND v.idempotency='unsafe' THEN risk_level:='critical'; base_reason:='tool_write_unsafe';
    ELSE risk_level:='critical'; base_reason:='tool_write_contract_mismatch'; END IF;
    RETURN NEXT;
END $$;

CREATE FUNCTION governance.evaluate_execution_policy(
    requested_workspace text,subject_kind_value text,subject_id_value text,
    requested_toolset text,requested_tool_version text,requested_connection text,
    arguments_digest text,idempotency_digest text,at_time timestamptz) RETURNS bigint
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE p record; facts record; reasons text[]; result text; seq bigint;
BEGIN
    PERFORM governance.assert_execution_workspace(requested_workspace);
    IF subject_kind_value NOT IN ('human','machine') OR subject_id_value !~ '^[A-Za-z0-9_-]{1,128}$'
       OR requested_toolset !~ '^[A-Za-z0-9_-]{1,128}$' OR requested_tool_version !~ '^[A-Za-z0-9_-]{1,128}$'
       OR requested_connection !~ '^[A-Za-z0-9_-]{1,128}$' OR arguments_digest !~ '^[a-f0-9]{64}$'
       OR idempotency_digest !~ '^[a-f0-9]{64}$' THEN
        RAISE EXCEPTION 'invalid execution policy input' USING ERRCODE='22023';
    END IF;
    SELECT * INTO p FROM governance.execution_policy_revisions
     WHERE workspace_id=requested_workspace AND state='active';
    IF NOT FOUND THEN RAISE EXCEPTION 'active execution policy is required' USING ERRCODE='23514'; END IF;
    SELECT * INTO facts FROM governance.execution_risk_facts(requested_workspace,requested_toolset,requested_tool_version);
    reasons:=ARRAY[facts.base_reason]::text[];
    IF p.deny_unsafe_write AND facts.unsafe_write THEN
        result:='deny'; reasons:=array_append(reasons,'unsafe_write_denied');
    ELSIF governance.risk_rank(facts.risk_level)<=governance.risk_rank(p.max_unconfirmed_risk_level) THEN
        result:='allow'; reasons:=array_append(reasons,'within_unconfirmed_risk');
    ELSIF subject_kind_value='human' THEN
        result:='confirmation_required'; reasons:=array_append(reasons,'human_confirmation_required');
    ELSE
        result:='deny'; reasons:=array_append(reasons,'machine_confirmation_unavailable');
    END IF;
    INSERT INTO governance.execution_policy_decisions(
      workspace_id,policy_revision_id,policy_revision,subject_kind,subject_id,
      toolset_version_id,tool_version_id,connection_id,arguments_hash,idempotency_key_hash,
      risk_level,outcome,reason_codes,evaluated_at)
    VALUES(requested_workspace,p.id,p.revision,subject_kind_value,subject_id_value,
      requested_toolset,requested_tool_version,requested_connection,arguments_digest,idempotency_digest,
      facts.risk_level,result,reasons,at_time)
    RETURNING sequence INTO seq;
    RETURN seq;
END $$;

CREATE FUNCTION governance.create_execution_confirmation(
    requested_workspace text,confirmation_id text,user_id_value text,
    requested_toolset text,requested_tool_version text,requested_connection text,
    arguments_digest text,idempotency_digest text,at_time timestamptz,expiry_time timestamptz)
RETURNS TABLE(decision_sequence bigint,confirmation_id_result text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE p record; d record;
BEGIN
    PERFORM governance.assert_execution_workspace(requested_workspace);
    IF confirmation_id !~ '^[A-Za-z0-9_-]{1,128}$' OR user_id_value !~ '^[A-Za-z0-9_-]{1,128}$'
       OR expiry_time<=at_time OR expiry_time>at_time+interval '10 minutes' THEN
        RAISE EXCEPTION 'invalid execution confirmation' USING ERRCODE='22023';
    END IF;
    decision_sequence:=governance.evaluate_execution_policy(
      requested_workspace,'human',user_id_value,requested_toolset,requested_tool_version,requested_connection,
      arguments_digest,idempotency_digest,at_time);
    SELECT * INTO d FROM governance.execution_policy_decisions WHERE sequence=decision_sequence;
    IF d.outcome<>'confirmation_required' THEN
        confirmation_id_result:=NULL;
        RETURN NEXT;
        RETURN;
    END IF;
    SELECT * INTO p FROM governance.execution_policy_revisions
     WHERE workspace_id=requested_workspace AND state='active';
    INSERT INTO governance.execution_confirmations(
      workspace_id,id,user_id,policy_revision_id,policy_revision,toolset_version_id,tool_version_id,
      connection_id,arguments_hash,idempotency_key_hash,risk_level,state,created_at,expires_at)
    VALUES(requested_workspace,confirmation_id,user_id_value,p.id,p.revision,requested_toolset,requested_tool_version,
      requested_connection,arguments_digest,idempotency_digest,d.risk_level,'active',at_time,expiry_time);
    confirmation_id_result:=confirmation_id;
    RETURN NEXT;
END $$;

CREATE FUNCTION governance.consume_execution_confirmation(
    requested_workspace text,user_id_value text,requested_toolset text,requested_tool_version text,
    requested_connection text,arguments_digest text,idempotency_digest text,at_time timestamptz) RETURNS text
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE p record; c record;
BEGIN
    PERFORM governance.assert_execution_workspace(requested_workspace);
    UPDATE governance.execution_confirmations
       SET state='expired',expired_at=at_time
     WHERE workspace_id=requested_workspace AND state='active' AND expires_at<=at_time;
    SELECT * INTO p FROM governance.execution_policy_revisions
     WHERE workspace_id=requested_workspace AND state='active';
    IF NOT FOUND THEN RETURN NULL; END IF;
    SELECT * INTO c FROM governance.execution_confirmations
     WHERE workspace_id=requested_workspace AND user_id=user_id_value AND policy_revision=p.revision
       AND toolset_version_id=requested_toolset AND tool_version_id=requested_tool_version
       AND connection_id=requested_connection AND arguments_hash=arguments_digest
       AND idempotency_key_hash=idempotency_digest AND state='active' AND expires_at>at_time
     ORDER BY created_at,id LIMIT 1 FOR UPDATE;
    IF NOT FOUND THEN RETURN NULL; END IF;
    UPDATE governance.execution_confirmations
       SET state='consumed',consumed_at=at_time
     WHERE workspace_id=requested_workspace AND id=c.id AND state='active';
    RETURN c.id;
END $$;

CREATE FUNCTION governance.create_execution_policy(
    requested_workspace text,policy_id text,actor_id text,max_unconfirmed_risk text,
    deny_unsafe boolean,at_time timestamptz) RETURNS bigint
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE next_revision bigint;
BEGIN
    PERFORM governance.assert_execution_workspace(requested_workspace);
    IF actor_id !~ '^[A-Za-z0-9_-]{1,128}$' OR policy_id !~ '^[A-Za-z0-9_-]{1,128}$'
       OR max_unconfirmed_risk NOT IN ('low','medium','high','critical') THEN
        RAISE EXCEPTION 'invalid execution policy' USING ERRCODE='22023';
    END IF;
    SELECT coalesce(max(revision),0)+1 INTO next_revision
      FROM governance.execution_policy_revisions WHERE workspace_id=requested_workspace;
    INSERT INTO governance.execution_policy_revisions(
      workspace_id,id,revision,state,max_unconfirmed_risk_level,deny_unsafe_write,created_by_user_id,created_at)
    VALUES(requested_workspace,policy_id,next_revision,'draft',max_unconfirmed_risk,deny_unsafe,actor_id,at_time);
    RETURN next_revision;
END $$;

CREATE FUNCTION governance.activate_execution_policy(
    requested_workspace text,policy_id text,actor_id text,at_time timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE candidate record;
BEGIN
    PERFORM governance.assert_execution_workspace(requested_workspace);
    IF actor_id !~ '^[A-Za-z0-9_-]{1,128}$' THEN
        RAISE EXCEPTION 'invalid execution policy actor' USING ERRCODE='22023';
    END IF;
    SELECT * INTO candidate FROM governance.execution_policy_revisions
     WHERE workspace_id=requested_workspace AND id=policy_id FOR UPDATE;
    IF NOT FOUND OR candidate.state<>'draft' THEN
        RAISE EXCEPTION 'execution policy is not draft' USING ERRCODE='23514';
    END IF;
    UPDATE governance.execution_policy_revisions SET state='retired',retired_at=at_time
     WHERE workspace_id=requested_workspace AND state='active';
    UPDATE governance.execution_policy_revisions
       SET state='active',activated_by_user_id=actor_id,activated_at=at_time
     WHERE workspace_id=requested_workspace AND id=policy_id AND state='draft';
    UPDATE governance.execution_confirmations SET state='expired',expired_at=at_time
     WHERE workspace_id=requested_workspace AND state='active';
END $$;

REVOKE ALL ON governance.execution_policy_revisions FROM PUBLIC;
REVOKE ALL ON governance.execution_policy_decisions FROM PUBLIC;
REVOKE ALL ON governance.execution_confirmations FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.guard_execution_policy_revision() FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.guard_execution_policy_decision() FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.guard_execution_confirmation() FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.assert_execution_workspace(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.execution_risk_facts(text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.evaluate_execution_policy(text,text,text,text,text,text,text,text,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.create_execution_confirmation(text,text,text,text,text,text,text,text,timestamptz,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.consume_execution_confirmation(text,text,text,text,text,text,text,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.create_execution_policy(text,text,text,text,boolean,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.activate_execution_policy(text,text,text,timestamptz) FROM PUBLIC;
