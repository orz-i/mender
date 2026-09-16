-- S4-D / S4-05 Platform Admin control-plane facts. Workspace and provider
-- operational metadata is separated from tenant-readable identity/deployment
-- rows. The dedicated Admin role receives only SECURITY DEFINER functions,
-- never direct tenant/runtime table access.

CREATE TABLE identity.workspace_admin_states (
    workspace_id text PRIMARY KEY REFERENCES identity.workspaces(id),
    revision bigint NOT NULL CHECK (revision > 0),
    reason text NOT NULL CHECK (char_length(reason) BETWEEN 1 AND 1000),
    actor_user_id text NOT NULL CHECK (actor_user_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    updated_at timestamptz NOT NULL
);
INSERT INTO identity.workspace_admin_states(workspace_id,revision,reason,actor_user_id,updated_at)
SELECT id,1,'workspace provisioned','system_migration',created_at FROM identity.workspaces;
REVOKE ALL ON identity.workspace_admin_states FROM PUBLIC;

CREATE TABLE supply.provider_admin_states (
    provider_id text PRIMARY KEY CHECK (provider_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    state text NOT NULL CHECK (state IN ('active','quarantined')),
    revision bigint NOT NULL CHECK (revision > 0),
    reason text NOT NULL CHECK (char_length(reason) BETWEEN 1 AND 1000),
    actor_user_id text NOT NULL CHECK (actor_user_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    updated_at timestamptz NOT NULL
);
INSERT INTO supply.provider_admin_states(provider_id,state,revision,reason,actor_user_id,updated_at)
SELECT provider_id,'active',1,'provider observed','system_migration',min(created_at)
FROM supply.deployments GROUP BY provider_id;
REVOKE ALL ON supply.provider_admin_states FROM PUBLIC;

CREATE TABLE governance.platform_incidents (
    id text PRIMARY KEY CHECK (id ~ '^[A-Za-z0-9_-]{1,128}$'),
    target_kind text NOT NULL CHECK (target_kind IN ('workspace','provider')),
    target_id text NOT NULL CHECK (target_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    severity text NOT NULL CHECK (severity IN ('info','warning','critical')),
    code text NOT NULL CHECK (code ~ '^[A-Za-z0-9_.:-]{1,128}$'),
    state text NOT NULL CHECK (state IN ('open','resolved')),
    opened_by_user_id text NOT NULL CHECK (opened_by_user_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    open_reason text NOT NULL CHECK (char_length(open_reason) BETWEEN 1 AND 1000),
    resolved_by_user_id text CHECK (resolved_by_user_id IS NULL OR resolved_by_user_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    resolution text NOT NULL DEFAULT '' CHECK (char_length(resolution) <= 1000),
    revision bigint NOT NULL CHECK (revision > 0),
    opened_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    resolved_at timestamptz,
    CHECK (updated_at>=opened_at),
    CHECK ((state='open' AND resolved_by_user_id IS NULL AND resolution='' AND resolved_at IS NULL)
        OR (state='resolved' AND resolved_by_user_id IS NOT NULL AND char_length(resolution) BETWEEN 1 AND 1000 AND resolved_at>=opened_at AND updated_at>=resolved_at))
);
CREATE INDEX platform_incidents_queue ON governance.platform_incidents(state,updated_at DESC,id DESC);
CREATE INDEX platform_incidents_target ON governance.platform_incidents(target_kind,target_id,updated_at DESC,id DESC);
REVOKE ALL ON governance.platform_incidents FROM PUBLIC;

CREATE TABLE governance.platform_admin_audit_events (
    sequence bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_kind text NOT NULL CHECK (event_kind IN ('workspace_frozen','workspace_unfrozen','provider_quarantined','provider_restored','incident_opened','incident_resolved')),
    target_kind text NOT NULL CHECK (target_kind IN ('workspace','provider','incident')),
    target_id text NOT NULL CHECK (target_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    target_revision bigint NOT NULL CHECK (target_revision > 0),
    actor_user_id text NOT NULL CHECK (actor_user_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    reason text NOT NULL CHECK (char_length(reason) BETWEEN 1 AND 1000),
    occurred_at timestamptz NOT NULL
);
CREATE INDEX platform_admin_audit_sequence ON governance.platform_admin_audit_events(sequence DESC);
CREATE INDEX platform_admin_audit_target ON governance.platform_admin_audit_events(target_kind,target_id,sequence DESC);
REVOKE ALL ON governance.platform_admin_audit_events FROM PUBLIC;

CREATE FUNCTION identity.platform_staff_allows(user_id_value text,action_name text) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
SELECT EXISTS(
  SELECT 1
  FROM identity.platform_staff s
  JOIN identity.users u ON u.id=s.user_id
  WHERE s.user_id=user_id_value AND NOT s.disabled AND NOT u.disabled
    AND CASE action_name
      WHEN 'platform:operate' THEN s.role='operator'
      WHEN 'platform:review' THEN s.role IN ('reviewer','operator')
      WHEN 'platform:audit' THEN s.role IN ('reviewer','operator','auditor')
      ELSE false
    END
)
$$;

CREATE FUNCTION governance.guard_platform_admin_audit() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
    RAISE EXCEPTION 'platform admin audit is immutable' USING ERRCODE='42501';
END $$;
CREATE TRIGGER immutable_platform_admin_audit
    BEFORE UPDATE OR DELETE ON governance.platform_admin_audit_events
    FOR EACH ROW EXECUTE FUNCTION governance.guard_platform_admin_audit();

CREATE FUNCTION governance.guard_platform_incident() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        RAISE EXCEPTION 'platform incidents cannot be deleted' USING ERRCODE='42501';
    END IF;
    IF NEW.id IS DISTINCT FROM OLD.id OR NEW.target_kind IS DISTINCT FROM OLD.target_kind OR NEW.target_id IS DISTINCT FROM OLD.target_id
       OR NEW.severity IS DISTINCT FROM OLD.severity OR NEW.code IS DISTINCT FROM OLD.code
       OR NEW.opened_by_user_id IS DISTINCT FROM OLD.opened_by_user_id OR NEW.open_reason IS DISTINCT FROM OLD.open_reason
       OR NEW.opened_at IS DISTINCT FROM OLD.opened_at OR OLD.state<>'open' OR NEW.state<>'resolved'
       OR NEW.revision<>OLD.revision+1 OR NEW.resolved_by_user_id IS NULL OR NEW.resolution=''
       OR NEW.resolved_at IS NULL OR NEW.updated_at IS DISTINCT FROM NEW.resolved_at THEN
        RAISE EXCEPTION 'invalid platform incident mutation' USING ERRCODE='42501';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER guard_platform_incident
    BEFORE UPDATE OR DELETE ON governance.platform_incidents
    FOR EACH ROW EXECUTE FUNCTION governance.guard_platform_incident();

CREATE FUNCTION governance.append_platform_admin_audit(
    event_name text,target_type text,target text,target_rev bigint,actor_id text,reason_text text,at_time timestamptz) RETURNS bigint
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE seq bigint;
BEGIN
    INSERT INTO governance.platform_admin_audit_events(event_kind,target_kind,target_id,target_revision,actor_user_id,reason,occurred_at)
    VALUES(event_name,target_type,target,target_rev,actor_id,reason_text,at_time)
    RETURNING sequence INTO seq;
    RETURN seq;
END $$;

CREATE FUNCTION identity.ensure_workspace_admin_state() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
    INSERT INTO identity.workspace_admin_states(workspace_id,revision,reason,actor_user_id,updated_at)
    VALUES(NEW.id,1,'workspace provisioned','system_migration',NEW.created_at)
    ON CONFLICT (workspace_id) DO NOTHING;
    RETURN NEW;
END $$;
CREATE TRIGGER ensure_workspace_admin_state
    AFTER INSERT ON identity.workspaces
    FOR EACH ROW EXECUTE FUNCTION identity.ensure_workspace_admin_state();

CREATE FUNCTION supply.ensure_provider_admin_state() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
    INSERT INTO supply.provider_admin_states(provider_id,state,revision,reason,actor_user_id,updated_at)
    VALUES(NEW.provider_id,'active',1,'provider observed','system_migration',NEW.created_at)
    ON CONFLICT (provider_id) DO NOTHING;
    RETURN NEW;
END $$;
CREATE TRIGGER ensure_provider_admin_state
    AFTER INSERT ON supply.deployments
    FOR EACH ROW EXECUTE FUNCTION supply.ensure_provider_admin_state();

CREATE FUNCTION governance.platform_admin_workspaces(actor_id text)
RETURNS TABLE(workspace_id text,frozen boolean,revision bigint,reason text,actor_user_id text,updated_at timestamptz,created_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
    IF NOT identity.platform_staff_allows(actor_id,'platform:audit') THEN
        RAISE EXCEPTION 'platform audit authority required' USING ERRCODE='42501';
    END IF;
    RETURN QUERY
      SELECT w.id,w.disabled,s.revision,s.reason,s.actor_user_id,s.updated_at,w.created_at
      FROM identity.workspaces w
      JOIN identity.workspace_admin_states s ON s.workspace_id=w.id
      ORDER BY w.id;
END $$;

CREATE FUNCTION governance.platform_admin_set_workspace_frozen(
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
      FROM identity.workspaces w JOIN identity.workspace_admin_states s ON s.workspace_id=w.id
     WHERE w.id=workspace FOR UPDATE OF w,s;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'workspace not found' USING ERRCODE='P0002';
    END IF;
    IF current_revision<>expected_revision OR current_frozen=requested_frozen THEN
        RAISE EXCEPTION 'workspace admin revision conflict' USING ERRCODE='40001';
    END IF;
    next_revision:=current_revision+1;
    UPDATE identity.workspaces SET disabled=requested_frozen WHERE id=workspace;
    UPDATE identity.workspace_admin_states SET revision=next_revision,reason=reason_text,actor_user_id=actor_id,updated_at=at_time
     WHERE workspace_id=workspace AND revision=current_revision;
    PERFORM governance.append_platform_admin_audit(
      CASE WHEN requested_frozen THEN 'workspace_frozen' ELSE 'workspace_unfrozen' END,
      'workspace',workspace,next_revision,actor_id,reason_text,at_time);
    RETURN QUERY SELECT workspace,requested_frozen,next_revision,reason_text,actor_id,at_time,created;
END $$;

CREATE FUNCTION governance.platform_admin_providers(actor_id text)
RETURNS TABLE(provider_id text,state text,revision bigint,reason text,actor_user_id text,updated_at timestamptz,deployment_count bigint,active_deployment_count bigint)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
    IF NOT identity.platform_staff_allows(actor_id,'platform:review') THEN
        RAISE EXCEPTION 'platform review authority required' USING ERRCODE='42501';
    END IF;
    RETURN QUERY
      SELECT s.provider_id,s.state,s.revision,s.reason,s.actor_user_id,s.updated_at,
             count(d.revision),count(d.revision) FILTER (WHERE d.state='active')
      FROM supply.provider_admin_states s
      LEFT JOIN supply.deployments d ON d.provider_id=s.provider_id
      GROUP BY s.provider_id,s.state,s.revision,s.reason,s.actor_user_id,s.updated_at
      ORDER BY s.provider_id;
END $$;

CREATE FUNCTION governance.platform_admin_set_provider_state(
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
      FROM supply.provider_admin_states s WHERE s.provider_id=provider FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'provider not found' USING ERRCODE='P0002';
    END IF;
    IF current_revision<>expected_revision OR current_state=requested_state THEN
        RAISE EXCEPTION 'provider admin revision conflict' USING ERRCODE='40001';
    END IF;
    next_revision:=current_revision+1;
    UPDATE supply.provider_admin_states SET state=requested_state,revision=next_revision,reason=reason_text,actor_user_id=actor_id,updated_at=at_time
     WHERE provider_id=provider AND revision=current_revision;
    PERFORM governance.append_platform_admin_audit(
      CASE WHEN requested_state='quarantined' THEN 'provider_quarantined' ELSE 'provider_restored' END,
      'provider',provider,next_revision,actor_id,reason_text,at_time);
    RETURN QUERY
      SELECT s.provider_id,s.state,s.revision,s.reason,s.actor_user_id,s.updated_at,
             count(d.revision),count(d.revision) FILTER (WHERE d.state='active')
      FROM supply.provider_admin_states s LEFT JOIN supply.deployments d ON d.provider_id=s.provider_id
      WHERE s.provider_id=provider
      GROUP BY s.provider_id,s.state,s.revision,s.reason,s.actor_user_id,s.updated_at;
END $$;

CREATE FUNCTION governance.platform_admin_open_incident(
    incident_id text,target_type text,target text,severity_value text,code_value text,actor_id text,reason_text text,at_time timestamptz)
RETURNS TABLE(id text,target_kind text,target_id text,severity text,code text,state text,opened_by_user_id text,open_reason text,resolved_by_user_id text,resolution text,revision bigint,opened_at timestamptz,updated_at timestamptz,resolved_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
    IF NOT identity.platform_staff_allows(actor_id,'platform:operate')
       OR incident_id !~ '^[A-Za-z0-9_-]{1,128}$' OR target !~ '^[A-Za-z0-9_-]{1,128}$'
       OR target_type NOT IN ('workspace','provider') OR severity_value NOT IN ('info','warning','critical')
       OR code_value !~ '^[A-Za-z0-9_.:-]{1,128}$' OR char_length(reason_text) NOT BETWEEN 1 AND 1000 OR at_time IS NULL THEN
        RAISE EXCEPTION 'invalid platform incident' USING ERRCODE='22023';
    END IF;
    IF (target_type='workspace' AND NOT EXISTS(SELECT 1 FROM identity.workspaces w WHERE w.id=target))
       OR (target_type='provider' AND NOT EXISTS(SELECT 1 FROM supply.provider_admin_states s WHERE s.provider_id=target)) THEN
        RAISE EXCEPTION 'platform incident target not found' USING ERRCODE='P0002';
    END IF;
    INSERT INTO governance.platform_incidents(
      id,target_kind,target_id,severity,code,state,opened_by_user_id,open_reason,revision,opened_at,updated_at)
    VALUES(incident_id,target_type,target,severity_value,code_value,'open',actor_id,reason_text,1,at_time,at_time);
    PERFORM governance.append_platform_admin_audit('incident_opened','incident',incident_id,1,actor_id,reason_text,at_time);
    RETURN QUERY SELECT i.id,i.target_kind,i.target_id,i.severity,i.code,i.state,i.opened_by_user_id,i.open_reason,
      coalesce(i.resolved_by_user_id,''),i.resolution,i.revision,i.opened_at,i.updated_at,i.resolved_at
      FROM governance.platform_incidents i WHERE i.id=incident_id;
END $$;

CREATE FUNCTION governance.platform_admin_resolve_incident(
    incident_id text,expected_revision bigint,actor_id text,resolution_text text,at_time timestamptz)
RETURNS TABLE(id text,target_kind text,target_id text,severity text,code text,state text,opened_by_user_id text,open_reason text,resolved_by_user_id text,resolution text,revision bigint,opened_at timestamptz,updated_at timestamptz,resolved_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE current_revision bigint; current_state text; next_revision bigint;
BEGIN
    IF NOT identity.platform_staff_allows(actor_id,'platform:operate') OR expected_revision<1
       OR char_length(resolution_text) NOT BETWEEN 1 AND 1000 OR at_time IS NULL THEN
        RAISE EXCEPTION 'platform incident resolution forbidden' USING ERRCODE='42501';
    END IF;
    SELECT i.revision,i.state INTO current_revision,current_state FROM governance.platform_incidents i WHERE i.id=incident_id FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'platform incident not found' USING ERRCODE='P0002';
    END IF;
    IF current_revision<>expected_revision OR current_state<>'open' THEN
        RAISE EXCEPTION 'platform incident revision conflict' USING ERRCODE='40001';
    END IF;
    next_revision:=current_revision+1;
    UPDATE governance.platform_incidents SET state='resolved',resolved_by_user_id=actor_id,resolution=resolution_text,
      revision=next_revision,updated_at=at_time,resolved_at=at_time WHERE id=incident_id AND revision=current_revision;
    PERFORM governance.append_platform_admin_audit('incident_resolved','incident',incident_id,next_revision,actor_id,resolution_text,at_time);
    RETURN QUERY SELECT i.id,i.target_kind,i.target_id,i.severity,i.code,i.state,i.opened_by_user_id,i.open_reason,
      coalesce(i.resolved_by_user_id,''),i.resolution,i.revision,i.opened_at,i.updated_at,i.resolved_at
      FROM governance.platform_incidents i WHERE i.id=incident_id;
END $$;

CREATE FUNCTION governance.platform_admin_incidents(
    actor_id text,state_filter text,target_kind_filter text,target_id_filter text,before_updated_at timestamptz,before_id text,limit_value integer)
RETURNS TABLE(id text,target_kind text,target_id text,severity text,code text,state text,opened_by_user_id text,open_reason text,resolved_by_user_id text,resolution text,revision bigint,opened_at timestamptz,updated_at timestamptz,resolved_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
    IF NOT identity.platform_staff_allows(actor_id,'platform:audit') OR limit_value NOT BETWEEN 1 AND 100
       OR state_filter NOT IN ('','open','resolved') OR target_kind_filter NOT IN ('','workspace','provider')
       OR (target_id_filter<>'' AND target_id_filter !~ '^[A-Za-z0-9_-]{1,128}$')
       OR ((before_updated_at IS NULL)<>(before_id='')) THEN
        RAISE EXCEPTION 'invalid platform incident query' USING ERRCODE='22023';
    END IF;
    RETURN QUERY
      SELECT i.id,i.target_kind,i.target_id,i.severity,i.code,i.state,i.opened_by_user_id,i.open_reason,
             coalesce(i.resolved_by_user_id,''),i.resolution,i.revision,i.opened_at,i.updated_at,i.resolved_at
      FROM governance.platform_incidents i
      WHERE (state_filter='' OR i.state=state_filter)
        AND (target_kind_filter='' OR i.target_kind=target_kind_filter)
        AND (target_id_filter='' OR i.target_id=target_id_filter)
        AND (before_updated_at IS NULL OR (i.updated_at,i.id)<(before_updated_at,before_id))
      ORDER BY i.updated_at DESC,i.id DESC LIMIT limit_value;
END $$;

CREATE FUNCTION governance.platform_admin_audit_export(actor_id text,before_sequence bigint,limit_value integer)
RETURNS TABLE(sequence bigint,event_kind text,target_kind text,target_id text,target_revision bigint,actor_user_id text,reason text,occurred_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
    IF NOT identity.platform_staff_allows(actor_id,'platform:audit') OR limit_value NOT BETWEEN 1 AND 500
       OR before_sequence<0 THEN
        RAISE EXCEPTION 'invalid platform audit export' USING ERRCODE='22023';
    END IF;
    RETURN QUERY
      SELECT e.sequence,e.event_kind,e.target_kind,e.target_id,e.target_revision,e.actor_user_id,e.reason,e.occurred_at
      FROM governance.platform_admin_audit_events e
      WHERE before_sequence=0 OR e.sequence<before_sequence
      ORDER BY e.sequence DESC LIMIT limit_value;
END $$;

REVOKE ALL ON FUNCTION identity.platform_staff_allows(text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION identity.ensure_workspace_admin_state() FROM PUBLIC;
REVOKE ALL ON FUNCTION supply.ensure_provider_admin_state() FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.guard_platform_admin_audit() FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.guard_platform_incident() FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.append_platform_admin_audit(text,text,text,bigint,text,text,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.platform_admin_workspaces(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.platform_admin_set_workspace_frozen(text,bigint,boolean,text,text,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.platform_admin_providers(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.platform_admin_set_provider_state(text,bigint,text,text,text,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.platform_admin_open_incident(text,text,text,text,text,text,text,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.platform_admin_resolve_incident(text,bigint,text,text,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.platform_admin_incidents(text,text,text,text,timestamptz,text,integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.platform_admin_audit_export(text,bigint,integer) FROM PUBLIC;
