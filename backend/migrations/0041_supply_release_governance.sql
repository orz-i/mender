-- Owner: supply. S4-B Release Governance separates immutable published
-- contracts from mutable new-admission routing. Release plans keep the exact
-- stable/candidate facts; release_routes is the current atomic route snapshot.
-- Historical execution.run_admissions rows are never rewritten by this schema.

CREATE TABLE supply.release_plans (
    workspace_id text NOT NULL CHECK (workspace_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    id text NOT NULL CHECK (id ~ '^[A-Za-z0-9_-]{1,128}$'),
    plugin_id text NOT NULL CHECK (plugin_id ~ '^[a-z][a-z0-9.-]{2,127}$'),
    plugin_version text NOT NULL CHECK (plugin_version ~ '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'),
    toolset_version_id text NOT NULL CHECK (toolset_version_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    tool_version_id text NOT NULL CHECK (tool_version_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    provider_id text NOT NULL CHECK (provider_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    stable_deployment_revision text NOT NULL CHECK (stable_deployment_revision ~ '^[A-Za-z0-9_-]{1,128}$'),
    candidate_deployment_revision text NOT NULL CHECK (candidate_deployment_revision ~ '^[A-Za-z0-9_-]{1,128}$'),
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    state text NOT NULL DEFAULT 'draft' CHECK (state IN ('draft','canary','active','draining','rolled_back','disabled')),
    created_by_user_id text NOT NULL CHECK (created_by_user_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    canary_started_at timestamptz,
    observation_until timestamptz,
    activated_at timestamptz,
    draining_at timestamptz,
    rolled_back_at timestamptz,
    disabled_at timestamptz,
    PRIMARY KEY(workspace_id,id),
    FOREIGN KEY(workspace_id,plugin_id,plugin_version) REFERENCES supply.plugin_versions(workspace_id,plugin_id,version),
    FOREIGN KEY(workspace_id,toolset_version_id) REFERENCES distribution.toolsets(workspace_id,id),
    FOREIGN KEY(workspace_id,tool_version_id) REFERENCES catalog.tool_version_management(workspace_id,tool_version_id),
    FOREIGN KEY(stable_deployment_revision) REFERENCES supply.deployments(revision),
    FOREIGN KEY(candidate_deployment_revision) REFERENCES supply.deployments(revision),
    CHECK (stable_deployment_revision<>candidate_deployment_revision),
    CHECK (updated_at>=created_at),
    CHECK (
      (state='draft' AND canary_started_at IS NULL AND observation_until IS NULL AND activated_at IS NULL AND draining_at IS NULL AND rolled_back_at IS NULL AND disabled_at IS NULL)
      OR (state='canary' AND canary_started_at IS NOT NULL AND observation_until>canary_started_at AND activated_at IS NULL AND draining_at IS NULL AND rolled_back_at IS NULL AND disabled_at IS NULL)
      OR (state='active' AND canary_started_at IS NOT NULL AND observation_until>canary_started_at AND activated_at>=observation_until AND draining_at IS NULL AND rolled_back_at IS NULL AND disabled_at IS NULL)
      OR (state='draining' AND canary_started_at IS NOT NULL AND observation_until>canary_started_at AND draining_at>=canary_started_at AND rolled_back_at IS NULL AND disabled_at IS NULL)
      OR (state='rolled_back' AND canary_started_at IS NOT NULL AND observation_until>canary_started_at AND rolled_back_at>=canary_started_at AND (disabled_at IS NULL OR rolled_back_at>=disabled_at))
      OR (state='disabled' AND canary_started_at IS NOT NULL AND observation_until>canary_started_at AND disabled_at>=canary_started_at AND rolled_back_at IS NULL)
    )
);
CREATE UNIQUE INDEX one_open_release_plan
    ON supply.release_plans(workspace_id,toolset_version_id,tool_version_id)
    WHERE state IN ('draft','canary','draining');
ALTER TABLE supply.release_plans ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply.release_plans FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON supply.release_plans
    USING(workspace_id=nullif(current_setting('mender.workspace_id',true),''))
    WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));

CREATE TABLE supply.release_routes (
    workspace_id text NOT NULL CHECK (workspace_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    toolset_version_id text NOT NULL CHECK (toolset_version_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    tool_version_id text NOT NULL CHECK (tool_version_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    stable_deployment_revision text NOT NULL CHECK (stable_deployment_revision ~ '^[A-Za-z0-9_-]{1,128}$'),
    candidate_deployment_revision text CHECK (candidate_deployment_revision IS NULL OR candidate_deployment_revision ~ '^[A-Za-z0-9_-]{1,128}$'),
    mode text NOT NULL CHECK (mode IN ('stable','canary','draining','disabled')),
    release_plan_id text,
    revision bigint NOT NULL CHECK (revision > 0),
    updated_at timestamptz NOT NULL,
    PRIMARY KEY(workspace_id,toolset_version_id,tool_version_id),
    FOREIGN KEY(workspace_id,toolset_version_id) REFERENCES distribution.toolsets(workspace_id,id),
    FOREIGN KEY(workspace_id,tool_version_id) REFERENCES catalog.tool_version_management(workspace_id,tool_version_id),
    FOREIGN KEY(stable_deployment_revision) REFERENCES supply.deployments(revision),
    FOREIGN KEY(candidate_deployment_revision) REFERENCES supply.deployments(revision),
    FOREIGN KEY(workspace_id,release_plan_id) REFERENCES supply.release_plans(workspace_id,id),
    CHECK ((mode='stable' AND candidate_deployment_revision IS NULL)
        OR (mode IN ('canary','draining','disabled') AND candidate_deployment_revision IS NOT NULL AND release_plan_id IS NOT NULL)),
    CHECK (candidate_deployment_revision IS NULL OR candidate_deployment_revision<>stable_deployment_revision)
);
ALTER TABLE supply.release_routes ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply.release_routes FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON supply.release_routes
    USING(workspace_id=nullif(current_setting('mender.workspace_id',true),''))
    WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));

CREATE TABLE supply.release_audit_events (
    sequence bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    workspace_id text NOT NULL CHECK (workspace_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    release_plan_id text NOT NULL CHECK (release_plan_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    plan_revision bigint NOT NULL CHECK (plan_revision > 0),
    route_revision bigint NOT NULL CHECK (route_revision > 0),
    event_kind text NOT NULL CHECK (event_kind IN ('created','canary_started','promoted','draining','rolled_back','emergency_disabled')),
    route_mode text NOT NULL CHECK (route_mode IN ('stable','canary','draining','disabled')),
    selected_deployment_revision text CHECK (selected_deployment_revision IS NULL OR selected_deployment_revision ~ '^[A-Za-z0-9_-]{1,128}$'),
    actor_user_id text NOT NULL CHECK (actor_user_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    reason text NOT NULL DEFAULT '' CHECK (length(reason) BETWEEN 1 AND 500),
    occurred_at timestamptz NOT NULL,
    FOREIGN KEY(workspace_id,release_plan_id) REFERENCES supply.release_plans(workspace_id,id)
);
CREATE INDEX release_audit_workspace_sequence ON supply.release_audit_events(workspace_id,sequence DESC);
CREATE INDEX release_audit_plan_sequence ON supply.release_audit_events(workspace_id,release_plan_id,sequence DESC);
ALTER TABLE supply.release_audit_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply.release_audit_events FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON supply.release_audit_events
    USING(workspace_id=nullif(current_setting('mender.workspace_id',true),''))
    WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));

CREATE FUNCTION supply.guard_release_plan_material() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        RAISE EXCEPTION 'ReleasePlan history cannot be deleted' USING ERRCODE='42501';
    END IF;
    IF NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.id IS DISTINCT FROM OLD.id
       OR NEW.plugin_id IS DISTINCT FROM OLD.plugin_id OR NEW.plugin_version IS DISTINCT FROM OLD.plugin_version
       OR NEW.toolset_version_id IS DISTINCT FROM OLD.toolset_version_id OR NEW.tool_version_id IS DISTINCT FROM OLD.tool_version_id
       OR NEW.provider_id IS DISTINCT FROM OLD.provider_id
       OR NEW.stable_deployment_revision IS DISTINCT FROM OLD.stable_deployment_revision
       OR NEW.candidate_deployment_revision IS DISTINCT FROM OLD.candidate_deployment_revision
       OR NEW.created_by_user_id IS DISTINCT FROM OLD.created_by_user_id OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'ReleasePlan material facts are immutable' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER guard_release_plan_material
    BEFORE UPDATE OR DELETE ON supply.release_plans
    FOR EACH ROW EXECUTE FUNCTION supply.guard_release_plan_material();

CREATE FUNCTION supply.guard_release_audit_immutable() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
    RAISE EXCEPTION 'release audit events are immutable' USING ERRCODE='42501';
END $$;
CREATE TRIGGER immutable_release_audit
    BEFORE UPDATE OR DELETE ON supply.release_audit_events
    FOR EACH ROW EXECUTE FUNCTION supply.guard_release_audit_immutable();

CREATE FUNCTION supply.append_release_audit(
    requested_workspace text,plan_id text,plan_rev bigint,route_rev bigint,event_name text,route_state text,
    selected_revision text,actor_id text,reason_text text,at_time timestamptz) RETURNS bigint
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE seq bigint;
BEGIN
    INSERT INTO supply.release_audit_events(
      workspace_id,release_plan_id,plan_revision,route_revision,event_kind,route_mode,
      selected_deployment_revision,actor_user_id,reason,occurred_at)
    VALUES(requested_workspace,plan_id,plan_rev,route_rev,event_name,route_state,
      selected_revision,actor_id,reason_text,at_time)
    RETURNING sequence INTO seq;
    RETURN seq;
END $$;

CREATE FUNCTION supply.release_plan_issues(requested_workspace text,plan_id text)
RETURNS TABLE(code text,target_id text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE p record; tool record; stable record; candidate record;
BEGIN
    PERFORM supply.assert_publication_workspace(requested_workspace);
    SELECT * INTO p FROM supply.release_plans WHERE workspace_id=requested_workspace AND id=plan_id;
    IF NOT FOUND THEN
        RETURN QUERY SELECT 'not_found'::text,plan_id;
        RETURN;
    END IF;
    SELECT provider_id,deployment_revision,state INTO tool
      FROM catalog.tool_version_management
     WHERE workspace_id=requested_workspace AND tool_version_id=p.tool_version_id;
    IF NOT FOUND OR tool.state<>'published' OR tool.provider_id<>p.provider_id THEN
        RETURN QUERY SELECT 'tool_version_unavailable'::text,p.tool_version_id;
    END IF;
    IF NOT EXISTS(SELECT 1 FROM distribution.toolsets s WHERE s.workspace_id=requested_workspace AND s.id=p.toolset_version_id AND s.state='published')
       OR NOT EXISTS(SELECT 1 FROM distribution.toolset_bindings b WHERE b.workspace_id=requested_workspace AND b.toolset_version_id=p.toolset_version_id AND b.tool_version_id=p.tool_version_id AND b.state='published') THEN
        RETURN QUERY SELECT 'toolset_binding_unavailable'::text,p.toolset_version_id;
    END IF;
    SELECT provider_id,state INTO stable FROM supply.deployments WHERE revision=p.stable_deployment_revision;
    IF NOT FOUND OR stable.provider_id<>p.provider_id OR stable.state<>'active' THEN
        RETURN QUERY SELECT 'stable_deployment_unavailable'::text,p.stable_deployment_revision;
    END IF;
    SELECT provider_id,state INTO candidate FROM supply.deployments WHERE revision=p.candidate_deployment_revision;
    IF NOT FOUND OR candidate.provider_id<>p.provider_id OR candidate.state<>'active' THEN
        RETURN QUERY SELECT 'candidate_deployment_unavailable'::text,p.candidate_deployment_revision;
    END IF;
    IF NOT EXISTS(
        SELECT 1
          FROM supply.plugin_versions v
          JOIN supply.publishers pub ON (pub.workspace_id,pub.id)=(v.workspace_id,v.publisher_id)
         WHERE v.workspace_id=requested_workspace AND v.plugin_id=p.plugin_id AND v.version=p.plugin_version
           AND v.state='published' AND pub.state='active'
           AND EXISTS(
             SELECT 1 FROM pg_catalog.jsonb_array_elements(v.manifest_json->'capabilities') cap
              WHERE ((cap->>'kind' IN ('api_tool','mcp_tool')) AND cap->>'tool_version_id'=p.tool_version_id)
                 OR ((cap->>'kind')='agent' AND cap->>'deployment_revision'=p.candidate_deployment_revision)
           )
    ) THEN
        RETURN QUERY SELECT 'published_plugin_evidence_missing'::text,(p.plugin_id||'@'||p.plugin_version)::text;
    END IF;
END $$;

CREATE FUNCTION supply.create_release_plan(
    requested_workspace text,plan_id text,plugin text,plugin_ver text,toolset_id text,tool_version text,
    provider text,stable_revision text,candidate_revision text,actor_id text,reason_text text,at_time timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE current_stable text; current_mode text; route_rev bigint; default_stable text;
BEGIN
    PERFORM supply.assert_publication_workspace(requested_workspace);
    IF char_length(reason_text) NOT BETWEEN 1 AND 500 THEN
        RAISE EXCEPTION 'release reason is required' USING ERRCODE='22023';
    END IF;
    SELECT stable_deployment_revision,mode,revision INTO current_stable,current_mode,route_rev
      FROM supply.release_routes
     WHERE workspace_id=requested_workspace AND toolset_version_id=toolset_id AND tool_version_id=tool_version
     FOR UPDATE;
    IF NOT FOUND THEN
        SELECT deployment_revision INTO default_stable FROM catalog.tool_version_management
         WHERE workspace_id=requested_workspace AND tool_version_id=tool_version AND state='published' AND provider_id=provider;
        IF default_stable IS NULL THEN
            RAISE EXCEPTION 'published ToolVersion unavailable for release' USING ERRCODE='23514';
        END IF;
        current_stable:=default_stable;
        current_mode:='stable';
        route_rev:=1;
    END IF;
    IF current_mode<>'stable' OR current_stable<>stable_revision THEN
        RAISE EXCEPTION 'release stable route changed' USING ERRCODE='23514';
    END IF;
    INSERT INTO supply.release_plans(
      workspace_id,id,plugin_id,plugin_version,toolset_version_id,tool_version_id,provider_id,
      stable_deployment_revision,candidate_deployment_revision,revision,state,created_by_user_id,created_at,updated_at)
    VALUES(requested_workspace,plan_id,plugin,plugin_ver,toolset_id,tool_version,provider,
      stable_revision,candidate_revision,1,'draft',actor_id,at_time,at_time);
    IF EXISTS(SELECT 1 FROM supply.release_plan_issues(requested_workspace,plan_id)) THEN
        RAISE EXCEPTION 'ReleasePlan preflight failed' USING ERRCODE='23514';
    END IF;
    INSERT INTO supply.release_routes(
      workspace_id,toolset_version_id,tool_version_id,stable_deployment_revision,candidate_deployment_revision,
      mode,release_plan_id,revision,updated_at)
    VALUES(requested_workspace,toolset_id,tool_version,stable_revision,NULL,'stable',plan_id,route_rev,at_time)
    ON CONFLICT (workspace_id,toolset_version_id,tool_version_id) DO UPDATE
      SET release_plan_id=EXCLUDED.release_plan_id,updated_at=EXCLUDED.updated_at
      WHERE supply.release_routes.mode='stable' AND supply.release_routes.stable_deployment_revision=EXCLUDED.stable_deployment_revision;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'release route changed during plan creation' USING ERRCODE='40001';
    END IF;
    SELECT revision INTO route_rev FROM supply.release_routes
      WHERE workspace_id=requested_workspace AND toolset_version_id=toolset_id AND tool_version_id=tool_version;
    PERFORM supply.append_release_audit(requested_workspace,plan_id,1,route_rev,'created','stable',stable_revision,actor_id,reason_text,at_time);
END $$;

CREATE FUNCTION supply.start_release_canary(
    requested_workspace text,plan_id text,actor_id text,reason_text text,at_time timestamptz,observation_end timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE p record; r record; next_plan_rev bigint; next_route_rev bigint;
BEGIN
    PERFORM supply.assert_publication_workspace(requested_workspace);
    IF observation_end<at_time+interval '1 minute' OR observation_end>at_time+interval '24 hours' OR char_length(reason_text) NOT BETWEEN 1 AND 500 THEN
        RAISE EXCEPTION 'invalid canary observation window' USING ERRCODE='22023';
    END IF;
    SELECT * INTO p FROM supply.release_plans WHERE workspace_id=requested_workspace AND id=plan_id FOR UPDATE;
    IF NOT FOUND OR p.state<>'draft' THEN
        RAISE EXCEPTION 'ReleasePlan is not draft' USING ERRCODE='23514';
    END IF;
    SELECT * INTO r FROM supply.release_routes
     WHERE workspace_id=requested_workspace AND toolset_version_id=p.toolset_version_id AND tool_version_id=p.tool_version_id FOR UPDATE;
    IF NOT FOUND OR r.mode<>'stable' OR r.stable_deployment_revision<>p.stable_deployment_revision THEN
        RAISE EXCEPTION 'release stable route changed' USING ERRCODE='23514';
    END IF;
    IF EXISTS(SELECT 1 FROM supply.release_plan_issues(requested_workspace,plan_id)) THEN
        RAISE EXCEPTION 'ReleasePlan preflight failed' USING ERRCODE='23514';
    END IF;
    next_plan_rev:=p.revision+1; next_route_rev:=r.revision+1;
    UPDATE supply.release_plans SET state='canary',revision=next_plan_rev,updated_at=at_time,canary_started_at=at_time,observation_until=observation_end
     WHERE workspace_id=requested_workspace AND id=plan_id AND state='draft';
    UPDATE supply.release_routes SET candidate_deployment_revision=p.candidate_deployment_revision,mode='canary',release_plan_id=plan_id,revision=next_route_rev,updated_at=at_time
     WHERE workspace_id=requested_workspace AND toolset_version_id=p.toolset_version_id AND tool_version_id=p.tool_version_id AND revision=r.revision;
    IF NOT FOUND THEN RAISE EXCEPTION 'release route CAS failed' USING ERRCODE='40001'; END IF;
    PERFORM supply.append_release_audit(requested_workspace,plan_id,next_plan_rev,next_route_rev,'canary_started','canary',p.candidate_deployment_revision,actor_id,reason_text,at_time);
END $$;

CREATE FUNCTION supply.promote_release(
    requested_workspace text,plan_id text,actor_id text,reason_text text,at_time timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE p record; r record; next_plan_rev bigint; next_route_rev bigint;
BEGIN
    PERFORM supply.assert_publication_workspace(requested_workspace);
    SELECT * INTO p FROM supply.release_plans WHERE workspace_id=requested_workspace AND id=plan_id FOR UPDATE;
    IF NOT FOUND OR p.state<>'canary' OR at_time<p.observation_until OR char_length(reason_text) NOT BETWEEN 1 AND 500 THEN
        RAISE EXCEPTION 'ReleasePlan cannot be promoted' USING ERRCODE='23514';
    END IF;
    SELECT * INTO r FROM supply.release_routes
     WHERE workspace_id=requested_workspace AND toolset_version_id=p.toolset_version_id AND tool_version_id=p.tool_version_id FOR UPDATE;
    IF NOT FOUND OR r.mode<>'canary' OR r.release_plan_id<>plan_id OR r.candidate_deployment_revision<>p.candidate_deployment_revision THEN
        RAISE EXCEPTION 'canary route changed' USING ERRCODE='23514';
    END IF;
    IF EXISTS(SELECT 1 FROM supply.release_plan_issues(requested_workspace,plan_id)) THEN
        RAISE EXCEPTION 'ReleasePlan preflight failed' USING ERRCODE='23514';
    END IF;
    next_plan_rev:=p.revision+1; next_route_rev:=r.revision+1;
    UPDATE supply.release_plans SET state='active',revision=next_plan_rev,updated_at=at_time,activated_at=at_time
     WHERE workspace_id=requested_workspace AND id=plan_id AND state='canary';
    UPDATE supply.release_routes SET stable_deployment_revision=p.candidate_deployment_revision,candidate_deployment_revision=NULL,
      mode='stable',release_plan_id=plan_id,revision=next_route_rev,updated_at=at_time
     WHERE workspace_id=requested_workspace AND toolset_version_id=p.toolset_version_id AND tool_version_id=p.tool_version_id AND revision=r.revision;
    IF NOT FOUND THEN RAISE EXCEPTION 'release route CAS failed' USING ERRCODE='40001'; END IF;
    PERFORM supply.append_release_audit(requested_workspace,plan_id,next_plan_rev,next_route_rev,'promoted','stable',p.candidate_deployment_revision,actor_id,reason_text,at_time);
END $$;

CREATE FUNCTION supply.drain_release(
    requested_workspace text,plan_id text,actor_id text,reason_text text,at_time timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE p record; r record; next_plan_rev bigint; next_route_rev bigint;
BEGIN
    PERFORM supply.assert_publication_workspace(requested_workspace);
    SELECT * INTO p FROM supply.release_plans WHERE workspace_id=requested_workspace AND id=plan_id FOR UPDATE;
    IF NOT FOUND OR p.state NOT IN ('canary','active') OR char_length(reason_text) NOT BETWEEN 1 AND 500 THEN
        RAISE EXCEPTION 'ReleasePlan cannot drain' USING ERRCODE='23514';
    END IF;
    SELECT * INTO r FROM supply.release_routes
     WHERE workspace_id=requested_workspace AND toolset_version_id=p.toolset_version_id AND tool_version_id=p.tool_version_id FOR UPDATE;
    IF NOT FOUND OR r.release_plan_id<>plan_id OR r.mode NOT IN ('canary','stable') THEN
        RAISE EXCEPTION 'release route changed' USING ERRCODE='23514';
    END IF;
    next_plan_rev:=p.revision+1; next_route_rev:=r.revision+1;
    UPDATE supply.release_plans SET state='draining',revision=next_plan_rev,updated_at=at_time,draining_at=at_time
     WHERE workspace_id=requested_workspace AND id=plan_id AND state IN ('canary','active');
    UPDATE supply.release_routes SET stable_deployment_revision=p.stable_deployment_revision,candidate_deployment_revision=p.candidate_deployment_revision,
      mode='draining',release_plan_id=plan_id,revision=next_route_rev,updated_at=at_time
     WHERE workspace_id=requested_workspace AND toolset_version_id=p.toolset_version_id AND tool_version_id=p.tool_version_id AND revision=r.revision;
    IF NOT FOUND THEN RAISE EXCEPTION 'release route CAS failed' USING ERRCODE='40001'; END IF;
    PERFORM supply.append_release_audit(requested_workspace,plan_id,next_plan_rev,next_route_rev,'draining','draining',p.stable_deployment_revision,actor_id,reason_text,at_time);
END $$;

CREATE FUNCTION supply.rollback_release(
    requested_workspace text,plan_id text,actor_id text,reason_text text,at_time timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE p record; r record; next_plan_rev bigint; next_route_rev bigint;
BEGIN
    PERFORM supply.assert_publication_workspace(requested_workspace);
    SELECT * INTO p FROM supply.release_plans WHERE workspace_id=requested_workspace AND id=plan_id FOR UPDATE;
    IF NOT FOUND OR p.state NOT IN ('canary','active','draining','disabled') OR char_length(reason_text) NOT BETWEEN 1 AND 500 THEN
        RAISE EXCEPTION 'ReleasePlan cannot rollback' USING ERRCODE='23514';
    END IF;
    SELECT * INTO r FROM supply.release_routes
     WHERE workspace_id=requested_workspace AND toolset_version_id=p.toolset_version_id AND tool_version_id=p.tool_version_id FOR UPDATE;
    IF NOT FOUND OR r.release_plan_id<>plan_id THEN
        RAISE EXCEPTION 'release route changed' USING ERRCODE='23514';
    END IF;
    IF NOT EXISTS(SELECT 1 FROM supply.deployments d WHERE d.revision=p.stable_deployment_revision AND d.provider_id=p.provider_id AND d.state='active') THEN
        RAISE EXCEPTION 'stable deployment unavailable' USING ERRCODE='23514';
    END IF;
    next_plan_rev:=p.revision+1; next_route_rev:=r.revision+1;
    UPDATE supply.release_plans SET state='rolled_back',revision=next_plan_rev,updated_at=at_time,rolled_back_at=at_time
     WHERE workspace_id=requested_workspace AND id=plan_id AND state IN ('canary','active','draining','disabled');
    UPDATE supply.release_routes SET stable_deployment_revision=p.stable_deployment_revision,candidate_deployment_revision=NULL,
      mode='stable',release_plan_id=plan_id,revision=next_route_rev,updated_at=at_time
     WHERE workspace_id=requested_workspace AND toolset_version_id=p.toolset_version_id AND tool_version_id=p.tool_version_id AND revision=r.revision;
    IF NOT FOUND THEN RAISE EXCEPTION 'release route CAS failed' USING ERRCODE='40001'; END IF;
    PERFORM supply.append_release_audit(requested_workspace,plan_id,next_plan_rev,next_route_rev,'rolled_back','stable',p.stable_deployment_revision,actor_id,reason_text,at_time);
END $$;

CREATE FUNCTION supply.emergency_disable_release(
    requested_workspace text,plan_id text,actor_id text,reason_text text,at_time timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE p record; r record; next_plan_rev bigint; next_route_rev bigint;
BEGIN
    PERFORM supply.assert_publication_workspace(requested_workspace);
    SELECT * INTO p FROM supply.release_plans WHERE workspace_id=requested_workspace AND id=plan_id FOR UPDATE;
    IF NOT FOUND OR p.state NOT IN ('canary','active','draining') OR char_length(reason_text) NOT BETWEEN 1 AND 500 THEN
        RAISE EXCEPTION 'ReleasePlan cannot be emergency-disabled' USING ERRCODE='23514';
    END IF;
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

CREATE FUNCTION supply.resolve_release_route(
    requested_workspace text,toolset_id text,tool_version text,default_revision text)
RETURNS TABLE(deployment_revision text,release_plan_id text,release_state text,release_revision bigint,routed boolean,blocked boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE r record;
BEGIN
    PERFORM supply.assert_publication_workspace(requested_workspace);
    SELECT * INTO r FROM supply.release_routes
     WHERE workspace_id=requested_workspace AND toolset_version_id=toolset_id AND tool_version_id=tool_version;
    IF NOT FOUND THEN
        RETURN QUERY SELECT default_revision,NULL::text,'stable'::text,0::bigint,false,false;
        RETURN;
    END IF;
    IF r.mode='disabled' THEN
        RETURN QUERY SELECT NULL::text,r.release_plan_id,r.mode,r.revision,true,true;
    ELSIF r.mode='canary' THEN
        RETURN QUERY SELECT r.candidate_deployment_revision,r.release_plan_id,r.mode,r.revision,true,false;
    ELSE
        RETURN QUERY SELECT r.stable_deployment_revision,r.release_plan_id,r.mode,r.revision,true,false;
    END IF;
END $$;

REVOKE ALL ON supply.release_plans,supply.release_routes,supply.release_audit_events FROM PUBLIC;
REVOKE ALL ON FUNCTION supply.guard_release_plan_material() FROM PUBLIC;
REVOKE ALL ON FUNCTION supply.guard_release_audit_immutable() FROM PUBLIC;
REVOKE ALL ON FUNCTION supply.append_release_audit(text,text,bigint,bigint,text,text,text,text,text,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION supply.release_plan_issues(text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION supply.create_release_plan(text,text,text,text,text,text,text,text,text,text,text,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION supply.start_release_canary(text,text,text,text,timestamptz,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION supply.promote_release(text,text,text,text,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION supply.drain_release(text,text,text,text,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION supply.rollback_release(text,text,text,text,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION supply.emergency_disable_release(text,text,text,text,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION supply.resolve_release_route(text,text,text,text) FROM PUBLIC;

