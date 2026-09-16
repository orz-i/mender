-- Owner split: governance owns the approval; supply owns the release mutation.
-- The old direct emergency function is replaced so even a release-manager DB
-- caller must present an approved, exact, one-time governance binding.

CREATE FUNCTION governance.request_release_emergency_approval(
    requested_workspace text,approval text,requester_id text,plan_id text,
    reason_text text,at_time timestamptz,expiry_time timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE plan_revision bigint;
BEGIN
    PERFORM governance.assert_dangerous_workspace(requested_workspace);
    SELECT revision INTO plan_revision FROM supply.release_plans
     WHERE workspace_id=requested_workspace AND id=plan_id;
    IF plan_revision IS NULL THEN
        RAISE EXCEPTION 'ReleasePlan unavailable for dangerous approval' USING ERRCODE='23514';
    END IF;
    PERFORM governance.submit_dangerous_operation(
      requested_workspace,approval,requester_id,'workspace_member',requester_id,
      'release.emergency_disable','release_plan',plan_id,plan_revision::text,
      '{"mode":"emergency_disable"}'::jsonb,
      'f2f39fe20f37679b6cdc443b04b23a4d1f9e943a7d5835e2724fdd0522c56adc',
      NULL,NULL,reason_text,at_time,expiry_time);
END $$;

DROP FUNCTION supply.emergency_disable_release(text,text,text,text,timestamptz);

CREATE FUNCTION supply.emergency_disable_release(
    requested_workspace text,plan_id text,approval_id text,actor_id text,reason_text text,at_time timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE p record; r record; next_plan_rev bigint; next_route_rev bigint;
BEGIN
    PERFORM supply.assert_publication_workspace(requested_workspace);
    SELECT * INTO p FROM supply.release_plans WHERE workspace_id=requested_workspace AND id=plan_id FOR UPDATE;
    IF NOT FOUND OR p.state NOT IN ('canary','active','draining') OR char_length(reason_text) NOT BETWEEN 1 AND 500 THEN
        RAISE EXCEPTION 'ReleasePlan cannot be emergency-disabled' USING ERRCODE='23514';
    END IF;
    -- The approval is consumed before mutation but in this same transaction;
    -- any later release failure rolls back consumption as well.
    PERFORM governance.consume_release_emergency_approval(
      requested_workspace,approval_id,actor_id,plan_id,p.revision,at_time);
    SELECT * INTO r FROM supply.release_routes
     WHERE workspace_id=requested_workspace AND toolset_version_id=p.toolset_version_id AND tool_version_id=p.tool_version_id FOR UPDATE;
    IF NOT FOUND OR r.release_plan_id<>plan_id THEN
        RAISE EXCEPTION 'release route changed' USING ERRCODE='23514';
    END IF;
    UPDATE supply.deployments SET state='disabled'
     WHERE revision=p.candidate_deployment_revision AND provider_id=p.provider_id AND state IN ('active','disabled');
    IF NOT FOUND THEN RAISE EXCEPTION 'candidate deployment unavailable' USING ERRCODE='23514'; END IF;
    next_plan_rev:=p.revision+1; next_route_rev:=r.revision+1;
    UPDATE supply.release_plans SET state='disabled',revision=next_plan_rev,updated_at=at_time,disabled_at=at_time
     WHERE workspace_id=requested_workspace AND id=plan_id AND state IN ('canary','active','draining');
    UPDATE supply.release_routes SET stable_deployment_revision=p.stable_deployment_revision,candidate_deployment_revision=p.candidate_deployment_revision,
      mode='disabled',release_plan_id=plan_id,revision=next_route_rev,updated_at=at_time
     WHERE workspace_id=requested_workspace AND toolset_version_id=p.toolset_version_id AND tool_version_id=p.tool_version_id AND revision=r.revision;
    IF NOT FOUND THEN RAISE EXCEPTION 'release route CAS failed' USING ERRCODE='40001'; END IF;
    PERFORM supply.append_release_audit(requested_workspace,plan_id,next_plan_rev,next_route_rev,'emergency_disabled','disabled',NULL,actor_id,reason_text,at_time);
END $$;

REVOKE ALL ON FUNCTION governance.request_release_emergency_approval(text,text,text,text,text,timestamptz,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION supply.emergency_disable_release(text,text,text,text,text,timestamptz) FROM PUBLIC;
