-- S4-D hardening: PL/pgSQL RETURNS TABLE names are variables inside the
-- function body. Qualify mutable-table columns so output names such as
-- workspace_id/provider_id/id/revision cannot shadow SQL column references.

CREATE OR REPLACE FUNCTION governance.platform_admin_set_workspace_frozen(
    workspace text,expected_revision bigint,requested_frozen boolean,actor_id text,reason_text text,at_time timestamptz)
RETURNS TABLE(workspace_id text,frozen boolean,revision bigint,reason text,actor_user_id text,updated_at timestamptz,created_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE current_frozen boolean; current_revision bigint; next_revision bigint; created timestamptz;
BEGIN
    IF NOT identity.platform_staff_allows(actor_id,'platform:operate') OR expected_revision<1
       OR char_length(reason_text) NOT BETWEEN 1 AND 1000 OR at_time IS NULL THEN
        RAISE EXCEPTION 'platform workspace operation forbidden' USING ERRCODE='42501';
    END IF;
    SELECT w.disabled,s.revision,w.created_at INTO current_frozen,current_revision,created
      FROM identity.workspaces AS w JOIN identity.workspace_admin_states AS s ON s.workspace_id=w.id
     WHERE w.id=workspace FOR UPDATE OF w,s;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'workspace not found' USING ERRCODE='P0002';
    END IF;
    IF current_revision<>expected_revision OR current_frozen=requested_frozen THEN
        RAISE EXCEPTION 'workspace admin revision conflict' USING ERRCODE='40001';
    END IF;
    next_revision:=current_revision+1;
    UPDATE identity.workspaces AS w SET disabled=requested_frozen WHERE w.id=workspace;
    UPDATE identity.workspace_admin_states AS s
       SET revision=next_revision,reason=reason_text,actor_user_id=actor_id,updated_at=at_time
     WHERE s.workspace_id=workspace AND s.revision=current_revision;
    PERFORM governance.append_platform_admin_audit(
      CASE WHEN requested_frozen THEN 'workspace_frozen' ELSE 'workspace_unfrozen' END,
      'workspace',workspace,next_revision,actor_id,reason_text,at_time);
    RETURN QUERY SELECT workspace,requested_frozen,next_revision,reason_text,actor_id,at_time,created;
END $$;

CREATE OR REPLACE FUNCTION governance.platform_admin_set_provider_state(
    provider text,expected_revision bigint,requested_state text,actor_id text,reason_text text,at_time timestamptz)
RETURNS TABLE(provider_id text,state text,revision bigint,reason text,actor_user_id text,updated_at timestamptz,deployment_count bigint,active_deployment_count bigint)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE current_state text; current_revision bigint; next_revision bigint;
BEGIN
    IF NOT identity.platform_staff_allows(actor_id,'platform:operate') OR expected_revision<1
       OR requested_state NOT IN ('active','quarantined') OR char_length(reason_text) NOT BETWEEN 1 AND 1000 OR at_time IS NULL THEN
        RAISE EXCEPTION 'platform provider operation forbidden' USING ERRCODE='42501';
    END IF;
    SELECT s.state,s.revision INTO current_state,current_revision
      FROM supply.provider_admin_states AS s WHERE s.provider_id=provider FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'provider not found' USING ERRCODE='P0002';
    END IF;
    IF current_revision<>expected_revision OR current_state=requested_state THEN
        RAISE EXCEPTION 'provider admin revision conflict' USING ERRCODE='40001';
    END IF;
    next_revision:=current_revision+1;
    UPDATE supply.provider_admin_states AS s
       SET state=requested_state,revision=next_revision,reason=reason_text,actor_user_id=actor_id,updated_at=at_time
     WHERE s.provider_id=provider AND s.revision=current_revision;
    PERFORM governance.append_platform_admin_audit(
      CASE WHEN requested_state='quarantined' THEN 'provider_quarantined' ELSE 'provider_restored' END,
      'provider',provider,next_revision,actor_id,reason_text,at_time);
    RETURN QUERY
      SELECT s.provider_id,s.state,s.revision,s.reason,s.actor_user_id,s.updated_at,
             count(d.revision),count(d.revision) FILTER (WHERE d.state='active')
      FROM supply.provider_admin_states AS s LEFT JOIN supply.deployments AS d ON d.provider_id=s.provider_id
      WHERE s.provider_id=provider
      GROUP BY s.provider_id,s.state,s.revision,s.reason,s.actor_user_id,s.updated_at;
END $$;

CREATE OR REPLACE FUNCTION governance.platform_admin_resolve_incident(
    incident_id text,expected_revision bigint,actor_id text,resolution_text text,at_time timestamptz)
RETURNS TABLE(id text,target_kind text,target_id text,severity text,code text,state text,opened_by_user_id text,open_reason text,resolved_by_user_id text,resolution text,revision bigint,opened_at timestamptz,updated_at timestamptz,resolved_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE current_revision bigint; current_state text; next_revision bigint;
BEGIN
    IF NOT identity.platform_staff_allows(actor_id,'platform:operate') OR expected_revision<1
       OR char_length(resolution_text) NOT BETWEEN 1 AND 1000 OR at_time IS NULL THEN
        RAISE EXCEPTION 'platform incident resolution forbidden' USING ERRCODE='42501';
    END IF;
    SELECT i.revision,i.state INTO current_revision,current_state
      FROM governance.platform_incidents AS i WHERE i.id=incident_id FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'platform incident not found' USING ERRCODE='P0002';
    END IF;
    IF current_revision<>expected_revision OR current_state<>'open' THEN
        RAISE EXCEPTION 'platform incident revision conflict' USING ERRCODE='40001';
    END IF;
    next_revision:=current_revision+1;
    UPDATE governance.platform_incidents AS i
       SET state='resolved',resolved_by_user_id=actor_id,resolution=resolution_text,
           revision=next_revision,updated_at=at_time,resolved_at=at_time
     WHERE i.id=incident_id AND i.revision=current_revision;
    PERFORM governance.append_platform_admin_audit('incident_resolved','incident',incident_id,next_revision,actor_id,resolution_text,at_time);
    RETURN QUERY SELECT i.id,i.target_kind,i.target_id,i.severity,i.code,i.state,i.opened_by_user_id,i.open_reason,
      coalesce(i.resolved_by_user_id,''),i.resolution,i.revision,i.opened_at,i.updated_at,i.resolved_at
      FROM governance.platform_incidents AS i WHERE i.id=incident_id;
END $$;

REVOKE ALL ON FUNCTION governance.platform_admin_set_workspace_frozen(text,bigint,boolean,text,text,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.platform_admin_set_provider_state(text,bigint,text,text,text,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.platform_admin_resolve_incident(text,bigint,text,text,timestamptz) FROM PUBLIC;
