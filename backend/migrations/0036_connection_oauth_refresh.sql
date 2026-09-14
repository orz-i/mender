CREATE TABLE connections.oauth_refresh_sessions (
    workspace_id text NOT NULL CHECK (workspace_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    connection_id text NOT NULL CHECK (connection_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    provider_id text NOT NULL CHECK (provider_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    refresh_credential_ref text NOT NULL CHECK (refresh_credential_ref ~ '^[A-Za-z0-9_-]{1,128}$'),
    refresh_secret_revision bigint NOT NULL CHECK (refresh_secret_revision > 0),
    connection_revision bigint NOT NULL CHECK (connection_revision > 0),
    required_scopes text[] NOT NULL CHECK (cardinality(required_scopes) BETWEEN 1 AND 16 AND array_position(required_scopes,NULL) IS NULL),
    granted_scopes text[] NOT NULL CHECK (cardinality(granted_scopes) BETWEEN 1 AND 32 AND array_position(granted_scopes,NULL) IS NULL),
    state text NOT NULL CHECK (state IN ('active','error','revoked')),
    last_error_code text CHECK (last_error_code IS NULL OR last_error_code IN ('invalid_grant','scope_reduced','invalid_token')),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL CHECK (updated_at >= created_at),
    last_refreshed_at timestamptz,
    PRIMARY KEY(workspace_id,connection_id),
    FOREIGN KEY(workspace_id,connection_id) REFERENCES connections.connections(workspace_id,id),
    CHECK ((state='error' AND last_error_code IS NOT NULL) OR (state<>'error' AND last_error_code IS NULL)),
    CHECK (last_refreshed_at IS NULL OR last_refreshed_at >= created_at)
);

CREATE INDEX oauth_refresh_sessions_candidates
    ON connections.oauth_refresh_sessions(workspace_id,provider_id,state,connection_id)
    WHERE state='active';

ALTER TABLE connections.oauth_refresh_sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE connections.oauth_refresh_sessions FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON connections.oauth_refresh_sessions
    USING(workspace_id=nullif(current_setting('mender.workspace_id',true),''))
    WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));

CREATE FUNCTION connections.guard_oauth_refresh_session() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        RAISE EXCEPTION 'oauth refresh session metadata is retained' USING ERRCODE='42501';
    END IF;
    IF NEW.workspace_id IS DISTINCT FROM OLD.workspace_id
       OR NEW.connection_id IS DISTINCT FROM OLD.connection_id
       OR NEW.provider_id IS DISTINCT FROM OLD.provider_id
       OR NEW.required_scopes IS DISTINCT FROM OLD.required_scopes
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'oauth refresh binding is immutable' USING ERRCODE='42501';
    END IF;
    IF OLD.state='active' AND NEW.state='active' THEN
        IF NEW.connection_revision <> OLD.connection_revision + 1
           OR NEW.updated_at < OLD.updated_at
           OR NEW.last_refreshed_at IS NULL
           OR (OLD.last_refreshed_at IS NOT NULL AND NEW.last_refreshed_at < OLD.last_refreshed_at) THEN
            RAISE EXCEPTION 'invalid oauth refresh advance' USING ERRCODE='23514';
        END IF;
    ELSIF OLD.state IN ('active','error') AND NEW.state='revoked' THEN
        IF NEW.connection_revision <> OLD.connection_revision + 1 OR NEW.updated_at < OLD.updated_at THEN
            RAISE EXCEPTION 'invalid oauth refresh revoke' USING ERRCODE='23514';
        END IF;
    ELSIF OLD.state='active' AND NEW.state='error' THEN
        IF NEW.connection_revision <> OLD.connection_revision + 1 OR NEW.updated_at < OLD.updated_at THEN
            RAISE EXCEPTION 'invalid oauth refresh failure' USING ERRCODE='23514';
        END IF;
    ELSE
        RAISE EXCEPTION 'invalid oauth refresh lifecycle transition' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER guard_oauth_refresh_session
    BEFORE UPDATE OR DELETE ON connections.oauth_refresh_sessions
    FOR EACH ROW EXECUTE FUNCTION connections.guard_oauth_refresh_session();

CREATE FUNCTION connections.revoke_oauth_refresh_session(
    requested_workspace text,
    requested_connection text,
    previous_revision bigint,
    new_revision bigint,
    revoked_at timestamptz
) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
    IF requested_workspace IS DISTINCT FROM nullif(current_setting('mender.workspace_id',true),'')
       OR previous_revision < 1 OR new_revision <> previous_revision + 1 OR revoked_at IS NULL THEN
        RAISE EXCEPTION 'oauth refresh revoke binding mismatch' USING ERRCODE='42501';
    END IF;
    UPDATE connections.oauth_refresh_sessions
       SET state='revoked',connection_revision=new_revision,updated_at=revoked_at,last_error_code=NULL
     WHERE workspace_id=requested_workspace AND connection_id=requested_connection
       AND state IN ('active','error') AND connection_revision=previous_revision;
END $$;

REVOKE ALL ON connections.oauth_refresh_sessions FROM PUBLIC;
REVOKE ALL ON FUNCTION connections.guard_oauth_refresh_session() FROM PUBLIC;
REVOKE ALL ON FUNCTION connections.revoke_oauth_refresh_session(text,text,bigint,bigint,timestamptz) FROM PUBLIC;

COMMENT ON TABLE connections.oauth_refresh_sessions IS
    'Secret-free OAuth refresh metadata. Access/refresh token bytes remain only in the mounted credential vault.';
COMMENT ON COLUMN connections.oauth_refresh_sessions.refresh_credential_ref IS
    'Opaque immutable vault reference; never an OAuth token and never exposed to browser projections.';
