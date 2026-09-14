-- Productize execution governance policy operations without widening the
-- execution engine. Policies remain immutable revisions with bounded,
-- declarative risk ceilings and confirmation lifetime.

ALTER TABLE governance.execution_policy_revisions
    ADD COLUMN max_machine_risk_level text,
    ADD COLUMN confirmation_ttl_seconds integer;

-- Preserve the exact legacy behavior: before this migration machine callers
-- shared the unconfirmed ceiling, and Human confirmations requested five
-- minutes by default.
UPDATE governance.execution_policy_revisions
   SET max_machine_risk_level=max_unconfirmed_risk_level,
       confirmation_ttl_seconds=300;

ALTER TABLE governance.execution_policy_revisions
    ALTER COLUMN max_machine_risk_level SET NOT NULL,
    ALTER COLUMN confirmation_ttl_seconds SET NOT NULL,
    ALTER COLUMN confirmation_ttl_seconds SET DEFAULT 300,
    ADD CONSTRAINT execution_policy_machine_risk_check
        CHECK (max_machine_risk_level IN ('low','medium','high','critical')),
    ADD CONSTRAINT execution_policy_confirmation_ttl_check
        CHECK (confirmation_ttl_seconds BETWEEN 30 AND 600),
    ADD CONSTRAINT execution_policy_machine_ceiling_check
        CHECK (governance.risk_rank(max_machine_risk_level)<=governance.risk_rank(max_unconfirmed_risk_level));

CREATE OR REPLACE FUNCTION governance.guard_execution_policy_revision() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        RAISE EXCEPTION 'execution policy revisions are immutable' USING ERRCODE='42501';
    END IF;
    IF NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.id IS DISTINCT FROM OLD.id
       OR NEW.revision IS DISTINCT FROM OLD.revision
       OR NEW.max_unconfirmed_risk_level IS DISTINCT FROM OLD.max_unconfirmed_risk_level
       OR NEW.max_machine_risk_level IS DISTINCT FROM OLD.max_machine_risk_level
       OR NEW.deny_unsafe_write IS DISTINCT FROM OLD.deny_unsafe_write
       OR NEW.confirmation_ttl_seconds IS DISTINCT FROM OLD.confirmation_ttl_seconds
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

ALTER TABLE governance.execution_policy_decisions
    DROP CONSTRAINT execution_policy_decisions_reason_codes_check;
ALTER TABLE governance.execution_policy_decisions
    ADD CONSTRAINT execution_policy_decisions_reason_codes_check CHECK (
        cardinality(reason_codes) > 0
        AND array_position(reason_codes,NULL) IS NULL
        AND reason_codes <@ ARRAY[
          'tool_read_only_safe','tool_read_only_non_safe','tool_write_idempotent','tool_write_unsafe','tool_write_contract_mismatch',
          'within_unconfirmed_risk','human_confirmation_required','machine_confirmation_unavailable','unsafe_write_denied',
          'within_machine_risk','machine_risk_above_ceiling'
        ]::text[]
    );

CREATE OR REPLACE FUNCTION governance.evaluate_execution_policy(
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
    ELSIF subject_kind_value='machine' THEN
        IF governance.risk_rank(facts.risk_level)<=governance.risk_rank(p.max_machine_risk_level) THEN
            result:='allow'; reasons:=array_append(reasons,'within_machine_risk');
        ELSE
            result:='deny'; reasons:=array_append(reasons,'machine_risk_above_ceiling');
        END IF;
    ELSIF governance.risk_rank(facts.risk_level)<=governance.risk_rank(p.max_unconfirmed_risk_level) THEN
        result:='allow'; reasons:=array_append(reasons,'within_unconfirmed_risk');
    ELSE
        result:='confirmation_required'; reasons:=array_append(reasons,'human_confirmation_required');
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

CREATE OR REPLACE FUNCTION governance.create_execution_confirmation(
    requested_workspace text,confirmation_id text,user_id_value text,
    requested_toolset text,requested_tool_version text,requested_connection text,
    arguments_digest text,idempotency_digest text,at_time timestamptz,expiry_time timestamptz)
RETURNS TABLE(decision_sequence bigint,confirmation_id_result text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE p record; d record; effective_expiry timestamptz;
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
    effective_expiry:=least(expiry_time,at_time+(p.confirmation_ttl_seconds*interval '1 second'));
    INSERT INTO governance.execution_confirmations(
      workspace_id,id,user_id,policy_revision_id,policy_revision,toolset_version_id,tool_version_id,
      connection_id,arguments_hash,idempotency_key_hash,risk_level,state,created_at,expires_at)
    VALUES(requested_workspace,confirmation_id,user_id_value,p.id,p.revision,requested_toolset,requested_tool_version,
      requested_connection,arguments_digest,idempotency_digest,d.risk_level,'active',at_time,effective_expiry);
    confirmation_id_result:=confirmation_id;
    RETURN NEXT;
END $$;

DROP FUNCTION governance.create_execution_policy(text,text,text,text,boolean,timestamptz);
CREATE FUNCTION governance.create_execution_policy(
    requested_workspace text,policy_id text,actor_id text,max_unconfirmed_risk text,
    max_machine_risk text,deny_unsafe boolean,confirmation_ttl integer,at_time timestamptz) RETURNS bigint
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE next_revision bigint;
BEGIN
    PERFORM governance.assert_execution_workspace(requested_workspace);
    IF actor_id !~ '^[A-Za-z0-9_-]{1,128}$' OR policy_id !~ '^[A-Za-z0-9_-]{1,128}$'
       OR max_unconfirmed_risk NOT IN ('low','medium','high','critical')
       OR max_machine_risk NOT IN ('low','medium','high','critical')
       OR governance.risk_rank(max_machine_risk)>governance.risk_rank(max_unconfirmed_risk)
       OR confirmation_ttl<30 OR confirmation_ttl>600 THEN
        RAISE EXCEPTION 'invalid execution policy' USING ERRCODE='22023';
    END IF;
    SELECT coalesce(max(revision),0)+1 INTO next_revision
      FROM governance.execution_policy_revisions WHERE workspace_id=requested_workspace;
    INSERT INTO governance.execution_policy_revisions(
      workspace_id,id,revision,state,max_unconfirmed_risk_level,max_machine_risk_level,
      deny_unsafe_write,confirmation_ttl_seconds,created_by_user_id,created_at)
    VALUES(requested_workspace,policy_id,next_revision,'draft',max_unconfirmed_risk,max_machine_risk,
      deny_unsafe,confirmation_ttl,actor_id,at_time);
    RETURN next_revision;
END $$;

REVOKE ALL ON FUNCTION governance.create_execution_policy(text,text,text,text,text,boolean,integer,timestamptz) FROM PUBLIC;
