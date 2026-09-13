-- Enforce the active publication policy at every positive publication gate.
-- A deny is persisted as a PolicyDecision and returns normally so the evidence
-- is not rolled back. Existing technical preflight and maker/checker remain.

CREATE FUNCTION governance.submit_catalog_publication_with_policy(
    requested_workspace text,request_id text,requested_kind text,requested_id text,
    requester_id text,at_time timestamptz,expiry_time timestamptz) RETURNS bigint
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE target_rev bigint; decision_seq bigint; decision_outcome text;
BEGIN
    PERFORM catalog.assert_management_workspace(requested_workspace);
    IF expiry_time<=at_time OR expiry_time>at_time+interval '24 hours' THEN
        RAISE EXCEPTION 'publication approval expiry is invalid' USING ERRCODE='22023';
    END IF;
    PERFORM governance.assert_catalog_preflight(requested_workspace,requested_kind,requested_id,at_time);
    target_rev:=governance.catalog_target_revision(requested_workspace,requested_kind,requested_id);
    decision_seq:=governance.evaluate_catalog_publication_policy(requested_workspace,requested_kind,requested_id,at_time);
    SELECT outcome INTO decision_outcome FROM governance.catalog_publication_policy_decisions WHERE sequence=decision_seq;
    IF decision_outcome<>'allow' THEN
        RETURN decision_seq;
    END IF;
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
    RETURN decision_seq;
END $$;

CREATE OR REPLACE FUNCTION governance.submit_catalog_publication(
    requested_workspace text,request_id text,requested_kind text,requested_id text,
    requester_id text,at_time timestamptz,expiry_time timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
    PERFORM governance.submit_catalog_publication_with_policy(
      requested_workspace,request_id,requested_kind,requested_id,requester_id,at_time,expiry_time);
END $$;

CREATE OR REPLACE FUNCTION governance.approve_catalog_publication(
    requested_workspace text,request_id text,reviewer_id text,at_time timestamptz,note text) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE a record; current_rev bigint; decision_seq bigint; decision_outcome text;
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
    decision_seq:=governance.evaluate_catalog_publication_policy(requested_workspace,a.target_kind,a.target_id,at_time);
    SELECT outcome INTO decision_outcome FROM governance.catalog_publication_policy_decisions WHERE sequence=decision_seq;
    IF decision_outcome<>'allow' THEN
        UPDATE governance.catalog_publication_approvals SET state='expired'
         WHERE workspace_id=requested_workspace AND id=request_id AND state='pending';
        PERFORM governance.append_catalog_publication_audit(
          requested_workspace,a.id,a.target_kind,a.target_id,a.target_revision,current_rev,
          'approval_expired',NULL,at_time,'policy_denied','');
        RETURN;
    END IF;
    UPDATE governance.catalog_publication_approvals
       SET state='approved',reviewer_user_id=reviewer_id,reviewed_at=at_time,decision_note=coalesce(note,'')
     WHERE workspace_id=requested_workspace AND id=request_id AND state='pending';
    PERFORM governance.append_catalog_publication_audit(
      requested_workspace,a.id,a.target_kind,a.target_id,a.target_revision,current_rev,
      'approval_approved',reviewer_id,at_time,'',coalesce(note,''));
END $$;

CREATE OR REPLACE FUNCTION governance.consume_catalog_publication(
    requested_workspace text,requested_kind text,requested_id text,at_time timestamptz) RETURNS text
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE a record; current_rev bigint; decision_seq bigint; decision_outcome text;
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
    decision_seq:=governance.evaluate_catalog_publication_policy(requested_workspace,requested_kind,requested_id,at_time);
    SELECT outcome INTO decision_outcome FROM governance.catalog_publication_policy_decisions WHERE sequence=decision_seq;
    IF decision_outcome<>'allow' THEN
        UPDATE governance.catalog_publication_approvals SET state='expired'
         WHERE workspace_id=requested_workspace AND id=a.id AND state='approved';
        PERFORM governance.append_catalog_publication_audit(
          requested_workspace,a.id,a.target_kind,a.target_id,a.target_revision,current_rev,
          'approval_expired',NULL,at_time,'policy_denied','');
        RETURN NULL;
    END IF;
    UPDATE governance.catalog_publication_approvals
       SET state='consumed',consumed_at=at_time
     WHERE workspace_id=requested_workspace AND id=a.id AND state='approved';
    PERFORM governance.append_catalog_publication_audit(
      requested_workspace,a.id,a.target_kind,a.target_id,a.target_revision,current_rev,
      'approval_consumed',NULL,at_time,'','');
    RETURN a.id;
END $$;

REVOKE ALL ON FUNCTION governance.submit_catalog_publication_with_policy(text,text,text,text,text,timestamptz,timestamptz) FROM PUBLIC;
