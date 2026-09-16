-- Owners: identity owns global Platform Staff identity facts; governance owns
-- dangerous-operation approvals, one-time consumption and workspace-scoped JIT
-- support grants. Platform Staff is deliberately not represented as a tenant
-- workspace membership and therefore receives no tenant authority by existence.

CREATE TABLE identity.platform_staff (
    user_id text PRIMARY KEY REFERENCES identity.users(id),
    role text NOT NULL CHECK (role IN ('support','reviewer','operator','auditor')),
    disabled boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL
);
REVOKE ALL ON identity.platform_staff FROM PUBLIC;

CREATE TABLE governance.dangerous_operation_approvals (
    workspace_id text NOT NULL CHECK (workspace_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    id text NOT NULL CHECK (id ~ '^[A-Za-z0-9_-]{1,128}$'),
    requester_user_id text NOT NULL CHECK (requester_user_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    subject_kind text NOT NULL CHECK (subject_kind IN ('workspace_member','platform_staff')),
    subject_id text NOT NULL CHECK (subject_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    action text NOT NULL CHECK (action IN ('release.emergency_disable','support.workspace_read')),
    target_kind text NOT NULL CHECK (target_kind IN ('release_plan','workspace')),
    target_id text NOT NULL CHECK (target_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    target_version text NOT NULL CHECK (char_length(target_version) <= 128),
    parameters_json jsonb NOT NULL CHECK (jsonb_typeof(parameters_json)='object' AND octet_length(parameters_json::text) <= 8192),
    parameters_sha256 text NOT NULL CHECK (parameters_sha256 ~ '^[a-f0-9]{64}$'),
    amount_micro bigint CHECK (amount_micro IS NULL OR amount_micro>=0),
    currency text CHECK (currency IS NULL OR currency ~ '^[A-Z]{3}$'),
    reason text NOT NULL CHECK (char_length(reason) BETWEEN 1 AND 1000),
    state text NOT NULL CHECK (state IN ('pending','approved','rejected','consumed','expired')),
    requested_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL CHECK (expires_at>=requested_at+interval '1 minute' AND expires_at<=requested_at+interval '30 minutes'),
    reviewer_user_id text CHECK (reviewer_user_id IS NULL OR reviewer_user_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    reviewed_at timestamptz,
    decision_note text NOT NULL DEFAULT '' CHECK (char_length(decision_note) <= 1000),
    consumed_at timestamptz,
    PRIMARY KEY(workspace_id,id),
    CHECK ((amount_micro IS NULL)=(currency IS NULL)),
    CHECK (subject_id=requester_user_id),
    CHECK ((state='pending' AND reviewer_user_id IS NULL AND reviewed_at IS NULL AND consumed_at IS NULL)
        OR (state IN ('approved','rejected') AND reviewer_user_id IS NOT NULL AND reviewed_at IS NOT NULL AND consumed_at IS NULL)
        OR (state='consumed' AND reviewer_user_id IS NOT NULL AND reviewed_at IS NOT NULL AND consumed_at IS NOT NULL)
        OR (state='expired' AND consumed_at IS NULL)),
    CHECK (reviewer_user_id IS NULL OR reviewer_user_id<>requester_user_id),
    CHECK (reviewed_at IS NULL OR (reviewed_at>=requested_at AND reviewed_at<expires_at)),
    CHECK (consumed_at IS NULL OR (consumed_at>=reviewed_at AND consumed_at<expires_at))
);
CREATE UNIQUE INDEX one_live_dangerous_operation_approval
    ON governance.dangerous_operation_approvals(
      workspace_id,requester_user_id,action,target_kind,target_id,target_version,parameters_sha256,
      coalesce(amount_micro,-1),coalesce(currency,''))
    WHERE state IN ('pending','approved');
CREATE INDEX dangerous_operation_approval_queue
    ON governance.dangerous_operation_approvals(workspace_id,state,requested_at,id);
ALTER TABLE governance.dangerous_operation_approvals ENABLE ROW LEVEL SECURITY;
ALTER TABLE governance.dangerous_operation_approvals FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON governance.dangerous_operation_approvals
    USING(workspace_id=nullif(current_setting('mender.workspace_id',true),''))
    WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));

CREATE TABLE governance.dangerous_operation_audit_events (
    sequence bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    workspace_id text NOT NULL CHECK (workspace_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    approval_id text NOT NULL CHECK (approval_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    event_kind text NOT NULL CHECK (event_kind IN ('requested','approved','rejected','expired','consumed','jit_granted','jit_revoked')),
    actor_user_id text CHECK (actor_user_id IS NULL OR actor_user_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    occurred_at timestamptz NOT NULL,
    note text NOT NULL DEFAULT '' CHECK (char_length(note) <= 1000)
);
CREATE INDEX dangerous_operation_audit_workspace_sequence
    ON governance.dangerous_operation_audit_events(workspace_id,sequence DESC);
CREATE INDEX dangerous_operation_audit_approval_sequence
    ON governance.dangerous_operation_audit_events(workspace_id,approval_id,sequence DESC);
ALTER TABLE governance.dangerous_operation_audit_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE governance.dangerous_operation_audit_events FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON governance.dangerous_operation_audit_events
    USING(workspace_id=nullif(current_setting('mender.workspace_id',true),''))
    WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));

CREATE TABLE governance.jit_support_grants (
    workspace_id text NOT NULL CHECK (workspace_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    id text NOT NULL CHECK (id ~ '^[A-Za-z0-9_-]{1,128}$'),
    approval_id text NOT NULL CHECK (approval_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    user_id text NOT NULL REFERENCES identity.users(id),
    scopes text[] NOT NULL CHECK (
      cardinality(scopes) BETWEEN 1 AND 3
      AND scopes <@ ARRAY['workspace:read','run:read','usage:read']::text[]
      AND array_position(scopes,NULL) IS NULL),
    reason text NOT NULL CHECK (char_length(reason) BETWEEN 1 AND 1000),
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL CHECK (expires_at>=created_at+interval '5 minutes' AND expires_at<=created_at+interval '1 hour'),
    revoked_at timestamptz,
    PRIMARY KEY(workspace_id,id),
    UNIQUE(workspace_id,approval_id),
    FOREIGN KEY(workspace_id,approval_id) REFERENCES governance.dangerous_operation_approvals(workspace_id,id),
    CHECK (revoked_at IS NULL OR revoked_at>=created_at)
);
CREATE INDEX jit_support_user_expiry ON governance.jit_support_grants(workspace_id,user_id,expires_at) WHERE revoked_at IS NULL;
ALTER TABLE governance.jit_support_grants ENABLE ROW LEVEL SECURITY;
ALTER TABLE governance.jit_support_grants FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON governance.jit_support_grants
    USING(workspace_id=nullif(current_setting('mender.workspace_id',true),''))
    WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));

CREATE FUNCTION governance.guard_dangerous_operation_approval() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        RAISE EXCEPTION 'dangerous operation approvals are immutable' USING ERRCODE='42501';
    END IF;
    IF NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.id IS DISTINCT FROM OLD.id
       OR NEW.requester_user_id IS DISTINCT FROM OLD.requester_user_id OR NEW.subject_kind IS DISTINCT FROM OLD.subject_kind
       OR NEW.subject_id IS DISTINCT FROM OLD.subject_id OR NEW.action IS DISTINCT FROM OLD.action
       OR NEW.target_kind IS DISTINCT FROM OLD.target_kind OR NEW.target_id IS DISTINCT FROM OLD.target_id
       OR NEW.target_version IS DISTINCT FROM OLD.target_version OR NEW.parameters_json IS DISTINCT FROM OLD.parameters_json
       OR NEW.parameters_sha256 IS DISTINCT FROM OLD.parameters_sha256 OR NEW.amount_micro IS DISTINCT FROM OLD.amount_micro
       OR NEW.currency IS DISTINCT FROM OLD.currency OR NEW.reason IS DISTINCT FROM OLD.reason
       OR NEW.requested_at IS DISTINCT FROM OLD.requested_at OR NEW.expires_at IS DISTINCT FROM OLD.expires_at THEN
        RAISE EXCEPTION 'dangerous operation approval binding is immutable' USING ERRCODE='42501';
    END IF;
    IF NOT ((OLD.state='pending' AND NEW.state IN ('approved','rejected','expired'))
         OR (OLD.state='approved' AND NEW.state IN ('consumed','expired'))) THEN
        RAISE EXCEPTION 'invalid dangerous operation approval transition' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER guard_dangerous_operation_approval
    BEFORE UPDATE OR DELETE ON governance.dangerous_operation_approvals
    FOR EACH ROW EXECUTE FUNCTION governance.guard_dangerous_operation_approval();

CREATE FUNCTION governance.guard_jit_support_grant() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        RAISE EXCEPTION 'JIT support grants are immutable' USING ERRCODE='42501';
    END IF;
    IF NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.id IS DISTINCT FROM OLD.id
       OR NEW.approval_id IS DISTINCT FROM OLD.approval_id OR NEW.user_id IS DISTINCT FROM OLD.user_id
       OR NEW.scopes IS DISTINCT FROM OLD.scopes OR NEW.reason IS DISTINCT FROM OLD.reason
       OR NEW.created_at IS DISTINCT FROM OLD.created_at OR NEW.expires_at IS DISTINCT FROM OLD.expires_at
       OR OLD.revoked_at IS NOT NULL OR NEW.revoked_at IS NULL OR NEW.revoked_at<NEW.created_at THEN
        RAISE EXCEPTION 'invalid JIT support grant mutation' USING ERRCODE='42501';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER guard_jit_support_grant
    BEFORE UPDATE OR DELETE ON governance.jit_support_grants
    FOR EACH ROW EXECUTE FUNCTION governance.guard_jit_support_grant();

CREATE FUNCTION governance.guard_dangerous_operation_audit() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
    RAISE EXCEPTION 'dangerous operation audit is immutable' USING ERRCODE='42501';
END $$;
CREATE TRIGGER immutable_dangerous_operation_audit
    BEFORE UPDATE OR DELETE ON governance.dangerous_operation_audit_events
    FOR EACH ROW EXECUTE FUNCTION governance.guard_dangerous_operation_audit();

CREATE FUNCTION governance.assert_dangerous_workspace(requested_workspace text) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
    IF requested_workspace !~ '^[A-Za-z0-9_-]{1,128}$'
       OR requested_workspace IS DISTINCT FROM nullif(current_setting('mender.workspace_id',true),'')
       OR NOT EXISTS(SELECT 1 FROM identity.workspaces w WHERE w.id=requested_workspace AND NOT w.disabled) THEN
        RAISE EXCEPTION 'workspace scope mismatch' USING ERRCODE='42501';
    END IF;
END $$;

CREATE FUNCTION governance.append_dangerous_operation_audit(
    requested_workspace text,approval text,event_name text,actor_id text,at_time timestamptz,note_text text) RETURNS bigint
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE seq bigint;
BEGIN
    INSERT INTO governance.dangerous_operation_audit_events(workspace_id,approval_id,event_kind,actor_user_id,occurred_at,note)
    VALUES(requested_workspace,approval,event_name,actor_id,at_time,coalesce(note_text,'')) RETURNING sequence INTO seq;
    RETURN seq;
END $$;

CREATE FUNCTION governance.dangerous_requester_authorized(requested_workspace text,action_name text,user_id_value text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
    IF NOT EXISTS(SELECT 1 FROM identity.users u WHERE u.id=user_id_value AND NOT u.disabled) THEN RETURN false; END IF;
    IF action_name='release.emergency_disable' THEN
        RETURN EXISTS(SELECT 1 FROM identity.workspace_memberships m
          WHERE m.workspace_id=requested_workspace AND m.user_id=user_id_value AND NOT m.disabled AND m.role IN ('owner','admin'));
    ELSIF action_name='support.workspace_read' THEN
        RETURN EXISTS(SELECT 1 FROM identity.platform_staff s
          WHERE s.user_id=user_id_value AND NOT s.disabled AND s.role IN ('support','operator'));
    END IF;
    RETURN false;
END $$;

CREATE FUNCTION governance.dangerous_reviewer_authorized(requested_workspace text,action_name text,user_id_value text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
    IF NOT EXISTS(SELECT 1 FROM identity.users u WHERE u.id=user_id_value AND NOT u.disabled) THEN RETURN false; END IF;
    IF action_name='release.emergency_disable' THEN
        RETURN EXISTS(SELECT 1 FROM identity.workspace_memberships m
          WHERE m.workspace_id=requested_workspace AND m.user_id=user_id_value AND NOT m.disabled AND m.role IN ('owner','admin'));
    ELSIF action_name='support.workspace_read' THEN
        RETURN EXISTS(SELECT 1 FROM identity.platform_staff s
          WHERE s.user_id=user_id_value AND NOT s.disabled AND s.role IN ('reviewer','operator'));
    END IF;
    RETURN false;
END $$;

CREATE FUNCTION governance.valid_support_parameters(parameters jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE SET search_path=pg_catalog AS $$
DECLARE scope_count integer; distinct_count integer; ttl integer;
BEGIN
    IF jsonb_typeof(parameters)<>'object' OR (parameters-ARRAY['scopes','ttl_seconds'])<>'{}'::jsonb
       OR jsonb_typeof(parameters->'scopes')<>'array' OR jsonb_typeof(parameters->'ttl_seconds')<>'number'
       OR (parameters->>'ttl_seconds') !~ '^[0-9]+$' THEN RETURN false; END IF;
    ttl:=(parameters->>'ttl_seconds')::integer;
    IF ttl<300 OR ttl>3600 THEN RETURN false; END IF;
    SELECT count(*),count(DISTINCT value) INTO scope_count,distinct_count FROM jsonb_array_elements_text(parameters->'scopes');
    IF scope_count<1 OR scope_count>3 OR distinct_count<>scope_count THEN RETURN false; END IF;
    RETURN NOT EXISTS(SELECT 1 FROM jsonb_array_elements_text(parameters->'scopes') s(value)
      WHERE value NOT IN ('workspace:read','run:read','usage:read'));
EXCEPTION WHEN others THEN RETURN false;
END $$;

CREATE FUNCTION governance.submit_dangerous_operation(
    requested_workspace text,approval text,requester_id text,subject_kind_value text,subject_id_value text,
    action_name text,target_kind_value text,target_id_value text,target_version_value text,
    parameters jsonb,parameters_digest text,amount_value bigint,currency_value text,reason_text text,
    at_time timestamptz,expiry_time timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE release_rev bigint;
BEGIN
    PERFORM governance.assert_dangerous_workspace(requested_workspace);
    IF approval !~ '^[A-Za-z0-9_-]{1,128}$' OR requester_id !~ '^[A-Za-z0-9_-]{1,128}$'
       OR subject_id_value<>requester_id OR parameters_digest !~ '^[a-f0-9]{64}$'
       OR jsonb_typeof(parameters)<>'object' OR octet_length(parameters::text)>8192
       OR char_length(reason_text) NOT BETWEEN 1 AND 1000
       OR expiry_time<at_time+interval '1 minute' OR expiry_time>at_time+interval '30 minutes'
       OR (amount_value IS NULL)<>(currency_value IS NULL)
       OR (amount_value IS NOT NULL AND (amount_value<0 OR currency_value !~ '^[A-Z]{3}$'))
       OR NOT governance.dangerous_requester_authorized(requested_workspace,action_name,requester_id) THEN
        RAISE EXCEPTION 'invalid dangerous operation request' USING ERRCODE='42501';
    END IF;
    IF action_name='release.emergency_disable' THEN
        IF subject_kind_value<>'workspace_member' OR target_kind_value<>'release_plan'
           OR parameters<>'{"mode":"emergency_disable"}'::jsonb THEN
            RAISE EXCEPTION 'invalid release emergency binding' USING ERRCODE='22023';
        END IF;
        SELECT revision INTO release_rev FROM supply.release_plans
         WHERE workspace_id=requested_workspace AND id=target_id_value;
        IF release_rev IS NULL OR target_version_value<>release_rev::text THEN
            RAISE EXCEPTION 'release target version changed' USING ERRCODE='23514';
        END IF;
    ELSIF action_name='support.workspace_read' THEN
        IF subject_kind_value<>'platform_staff' OR target_kind_value<>'workspace' OR target_id_value<>requested_workspace
           OR target_version_value<>'' OR NOT governance.valid_support_parameters(parameters) THEN
            RAISE EXCEPTION 'invalid support JIT binding' USING ERRCODE='22023';
        END IF;
    ELSE
        RAISE EXCEPTION 'unsupported dangerous operation' USING ERRCODE='22023';
    END IF;
    INSERT INTO governance.dangerous_operation_approvals(
      workspace_id,id,requester_user_id,subject_kind,subject_id,action,target_kind,target_id,target_version,
      parameters_json,parameters_sha256,amount_micro,currency,reason,state,requested_at,expires_at)
    VALUES(requested_workspace,approval,requester_id,subject_kind_value,subject_id_value,action_name,target_kind_value,target_id_value,target_version_value,
      parameters,parameters_digest,amount_value,currency_value,reason_text,'pending',at_time,expiry_time);
    PERFORM governance.append_dangerous_operation_audit(requested_workspace,approval,'requested',requester_id,at_time,reason_text);
END $$;

CREATE FUNCTION governance.approve_dangerous_operation(
    requested_workspace text,approval text,reviewer_id text,at_time timestamptz,note_text text) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE a record;
BEGIN
    PERFORM governance.assert_dangerous_workspace(requested_workspace);
    SELECT * INTO a FROM governance.dangerous_operation_approvals
     WHERE workspace_id=requested_workspace AND id=approval FOR UPDATE;
    IF NOT FOUND OR a.state<>'pending' THEN RAISE EXCEPTION 'approval is not pending' USING ERRCODE='23514'; END IF;
    IF a.expires_at<=at_time THEN
        UPDATE governance.dangerous_operation_approvals SET state='expired' WHERE workspace_id=requested_workspace AND id=approval AND state='pending';
        PERFORM governance.append_dangerous_operation_audit(requested_workspace,approval,'expired',NULL,at_time,'');
        RETURN;
    END IF;
    IF reviewer_id=a.requester_user_id OR NOT governance.dangerous_reviewer_authorized(requested_workspace,a.action,reviewer_id)
       OR char_length(coalesce(note_text,''))>1000 THEN
        RAISE EXCEPTION 'dangerous operation reviewer forbidden' USING ERRCODE='42501';
    END IF;
    UPDATE governance.dangerous_operation_approvals
       SET state='approved',reviewer_user_id=reviewer_id,reviewed_at=at_time,decision_note=coalesce(note_text,'')
     WHERE workspace_id=requested_workspace AND id=approval AND state='pending';
    PERFORM governance.append_dangerous_operation_audit(requested_workspace,approval,'approved',reviewer_id,at_time,coalesce(note_text,''));
END $$;

CREATE FUNCTION governance.reject_dangerous_operation(
    requested_workspace text,approval text,reviewer_id text,at_time timestamptz,note_text text) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE a record;
BEGIN
    PERFORM governance.assert_dangerous_workspace(requested_workspace);
    SELECT * INTO a FROM governance.dangerous_operation_approvals
     WHERE workspace_id=requested_workspace AND id=approval FOR UPDATE;
    IF NOT FOUND OR a.state<>'pending' OR reviewer_id=a.requester_user_id
       OR NOT governance.dangerous_reviewer_authorized(requested_workspace,a.action,reviewer_id)
       OR char_length(coalesce(note_text,''))>1000 THEN
        RAISE EXCEPTION 'dangerous operation rejection forbidden' USING ERRCODE='42501';
    END IF;
    IF a.expires_at<=at_time THEN
        UPDATE governance.dangerous_operation_approvals SET state='expired' WHERE workspace_id=requested_workspace AND id=approval AND state='pending';
        PERFORM governance.append_dangerous_operation_audit(requested_workspace,approval,'expired',NULL,at_time,'');
        RETURN;
    END IF;
    UPDATE governance.dangerous_operation_approvals
       SET state='rejected',reviewer_user_id=reviewer_id,reviewed_at=at_time,decision_note=coalesce(note_text,'')
     WHERE workspace_id=requested_workspace AND id=approval AND state='pending';
    PERFORM governance.append_dangerous_operation_audit(requested_workspace,approval,'rejected',reviewer_id,at_time,coalesce(note_text,''));
END $$;

CREATE FUNCTION governance.consume_release_emergency_approval(
    requested_workspace text,approval text,consumer_id text,plan_id text,plan_revision bigint,at_time timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE a record;
BEGIN
    PERFORM governance.assert_dangerous_workspace(requested_workspace);
    SELECT * INTO a FROM governance.dangerous_operation_approvals
     WHERE workspace_id=requested_workspace AND id=approval FOR UPDATE;
    IF NOT FOUND OR a.state<>'approved' OR a.expires_at<=at_time OR a.requester_user_id<>consumer_id
       OR a.subject_kind<>'workspace_member' OR a.subject_id<>consumer_id OR a.action<>'release.emergency_disable'
       OR a.target_kind<>'release_plan' OR a.target_id<>plan_id OR a.target_version<>plan_revision::text
       OR a.parameters_json<>'{"mode":"emergency_disable"}'::jsonb OR a.amount_micro IS NOT NULL OR a.currency IS NOT NULL THEN
        RAISE EXCEPTION 'exact emergency approval unavailable' USING ERRCODE='42501';
    END IF;
    UPDATE governance.dangerous_operation_approvals SET state='consumed',consumed_at=at_time
     WHERE workspace_id=requested_workspace AND id=approval AND state='approved';
    PERFORM governance.append_dangerous_operation_audit(requested_workspace,approval,'consumed',consumer_id,at_time,'release.emergency_disable');
END $$;

CREATE FUNCTION governance.activate_jit_support(
    requested_workspace text,approval text,grant_id text,consumer_id text,at_time timestamptz) RETURNS timestamptz
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE a record; ttl integer; granted_scopes text[]; grant_expiry timestamptz;
BEGIN
    PERFORM governance.assert_dangerous_workspace(requested_workspace);
    SELECT * INTO a FROM governance.dangerous_operation_approvals
     WHERE workspace_id=requested_workspace AND id=approval FOR UPDATE;
    IF NOT FOUND OR a.state<>'approved' OR a.expires_at<=at_time OR a.requester_user_id<>consumer_id
       OR a.subject_kind<>'platform_staff' OR a.subject_id<>consumer_id OR a.action<>'support.workspace_read'
       OR a.target_kind<>'workspace' OR a.target_id<>requested_workspace OR a.target_version<>''
       OR a.amount_micro IS NOT NULL OR a.currency IS NOT NULL OR NOT governance.valid_support_parameters(a.parameters_json)
       OR NOT governance.dangerous_requester_authorized(requested_workspace,a.action,consumer_id)
       OR grant_id !~ '^[A-Za-z0-9_-]{1,128}$' THEN
        RAISE EXCEPTION 'exact JIT support approval unavailable' USING ERRCODE='42501';
    END IF;
    ttl:=(a.parameters_json->>'ttl_seconds')::integer;
    SELECT array_agg(value ORDER BY value) INTO granted_scopes FROM jsonb_array_elements_text(a.parameters_json->'scopes') s(value);
    grant_expiry:=at_time+(ttl*interval '1 second');
    UPDATE governance.dangerous_operation_approvals SET state='consumed',consumed_at=at_time
     WHERE workspace_id=requested_workspace AND id=approval AND state='approved';
    INSERT INTO governance.jit_support_grants(workspace_id,id,approval_id,user_id,scopes,reason,created_at,expires_at)
    VALUES(requested_workspace,grant_id,approval,consumer_id,granted_scopes,a.reason,at_time,grant_expiry);
    PERFORM governance.append_dangerous_operation_audit(requested_workspace,approval,'consumed',consumer_id,at_time,'support.workspace_read');
    PERFORM governance.append_dangerous_operation_audit(requested_workspace,approval,'jit_granted',consumer_id,at_time,grant_id);
    RETURN grant_expiry;
END $$;

CREATE FUNCTION governance.revoke_jit_support(
    requested_workspace text,grant_id text,actor_id text,at_time timestamptz,reason_text text) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE g record;
BEGIN
    PERFORM governance.assert_dangerous_workspace(requested_workspace);
    IF NOT governance.dangerous_reviewer_authorized(requested_workspace,'support.workspace_read',actor_id)
       OR char_length(reason_text) NOT BETWEEN 1 AND 1000 THEN
        RAISE EXCEPTION 'JIT revoke forbidden' USING ERRCODE='42501';
    END IF;
    SELECT * INTO g FROM governance.jit_support_grants WHERE workspace_id=requested_workspace AND id=grant_id FOR UPDATE;
    IF NOT FOUND OR g.revoked_at IS NOT NULL OR at_time<g.created_at THEN
        RAISE EXCEPTION 'JIT grant unavailable' USING ERRCODE='23514';
    END IF;
    UPDATE governance.jit_support_grants SET revoked_at=at_time WHERE workspace_id=requested_workspace AND id=grant_id AND revoked_at IS NULL;
    PERFORM governance.append_dangerous_operation_audit(requested_workspace,g.approval_id,'jit_revoked',actor_id,at_time,reason_text);
END $$;

CREATE FUNCTION governance.authorize_jit_support(
    requested_workspace text,user_id_value text,scope_value text,at_time timestamptz) RETURNS text
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE grant_id text;
BEGIN
    PERFORM governance.assert_dangerous_workspace(requested_workspace);
    IF scope_value NOT IN ('workspace:read','run:read','usage:read')
       OR NOT EXISTS(SELECT 1 FROM identity.platform_staff s JOIN identity.users u ON u.id=s.user_id
                     WHERE s.user_id=user_id_value AND NOT s.disabled AND NOT u.disabled AND s.role IN ('support','operator')) THEN
        RETURN NULL;
    END IF;
    SELECT g.id INTO grant_id FROM governance.jit_support_grants g
     WHERE g.workspace_id=requested_workspace AND g.user_id=user_id_value
       AND g.revoked_at IS NULL AND g.created_at<=at_time AND g.expires_at>at_time
       AND scope_value=ANY(g.scopes)
     ORDER BY g.created_at DESC,g.id DESC LIMIT 1;
    RETURN grant_id;
END $$;

REVOKE ALL ON governance.dangerous_operation_approvals,governance.dangerous_operation_audit_events,governance.jit_support_grants FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.guard_dangerous_operation_approval() FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.guard_jit_support_grant() FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.guard_dangerous_operation_audit() FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.assert_dangerous_workspace(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.append_dangerous_operation_audit(text,text,text,text,timestamptz,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.dangerous_requester_authorized(text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.dangerous_reviewer_authorized(text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.valid_support_parameters(jsonb) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.submit_dangerous_operation(text,text,text,text,text,text,text,text,text,jsonb,text,bigint,text,text,timestamptz,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.approve_dangerous_operation(text,text,text,timestamptz,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.reject_dangerous_operation(text,text,text,timestamptz,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.consume_release_emergency_approval(text,text,text,text,bigint,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.activate_jit_support(text,text,text,text,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.revoke_jit_support(text,text,text,timestamptz,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.authorize_jit_support(text,text,text,timestamptz) FROM PUBLIC;
