-- Owner: execution. Persist the provider identity that actually accepted a
-- submission so result reconciliation never guesses routing from request IDs.

ALTER TABLE execution.run_attempts ADD COLUMN provider_id text;
ALTER TABLE execution.run_attempts ADD CONSTRAINT run_attempts_provider_id_check
    CHECK (provider_id IS NULL OR provider_id ~ '^[A-Za-z0-9_-]{1,128}$');
ALTER TABLE execution.provider_observations ADD COLUMN provider_id text;
ALTER TABLE execution.provider_observations ADD CONSTRAINT provider_observations_provider_id_check
    CHECK (provider_id IS NULL OR provider_id ~ '^[A-Za-z0-9_-]{1,128}$');

ALTER TABLE execution.run_attempts DROP CONSTRAINT run_attempts_submission_check;
ALTER TABLE execution.run_attempts ADD CONSTRAINT run_attempts_submission_check CHECK (
    (state='leased' AND finished_at IS NULL AND submission_key IS NULL AND submission_intent_at IS NULL AND provider_id IS NULL AND provider_request_id IS NULL AND external_task_id IS NULL AND submitted_at IS NULL AND unknown_at IS NULL AND unknown_reason IS NULL)
    OR (state IN ('released','expired') AND finished_at IS NOT NULL AND finished_at>=leased_at AND submission_key IS NULL AND submission_intent_at IS NULL AND provider_id IS NULL AND provider_request_id IS NULL AND external_task_id IS NULL AND submitted_at IS NULL AND unknown_at IS NULL AND unknown_reason IS NULL)
    OR (state='submitting' AND finished_at IS NULL AND submission_key ~ '^[A-Za-z0-9._:-]{8,200}$' AND submission_intent_at>=leased_at AND submission_intent_at<lease_until AND provider_id IS NULL AND provider_request_id IS NULL AND external_task_id IS NULL AND submitted_at IS NULL AND unknown_at IS NULL AND unknown_reason IS NULL)
    -- provider_id is nullable only to keep pre-0011 submitted rows readable. A
    -- trigger below requires it for every new submitted transition.
    OR (state='submitted' AND finished_at IS NULL AND submission_key ~ '^[A-Za-z0-9._:-]{8,200}$' AND submission_intent_at>=leased_at AND submission_intent_at<lease_until AND (provider_id IS NULL OR provider_id ~ '^[A-Za-z0-9_-]{1,128}$') AND provider_request_id IS NOT NULL AND char_length(provider_request_id) BETWEEN 1 AND 512 AND (external_task_id IS NULL OR char_length(external_task_id) BETWEEN 1 AND 512) AND submitted_at>=submission_intent_at AND submitted_at<lease_until AND unknown_at IS NULL AND unknown_reason IS NULL)
    OR (state='unknown' AND finished_at=unknown_at AND submission_key ~ '^[A-Za-z0-9._:-]{8,200}$' AND submission_intent_at>=leased_at AND submission_intent_at<lease_until AND (provider_id IS NULL OR provider_id ~ '^[A-Za-z0-9_-]{1,128}$') AND (provider_request_id IS NULL OR char_length(provider_request_id) BETWEEN 1 AND 512) AND (external_task_id IS NULL OR char_length(external_task_id) BETWEEN 1 AND 512) AND (submitted_at IS NULL OR (submitted_at>=submission_intent_at AND submitted_at<lease_until)) AND unknown_at>=submission_intent_at AND char_length(unknown_reason) BETWEEN 1 AND 500)
);

CREATE FUNCTION execution.require_provider_identity() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
    IF NEW.state='submitted' AND NEW.provider_id IS NULL THEN
        IF TG_OP='INSERT' OR OLD.state IS DISTINCT FROM 'submitted' THEN
            RAISE EXCEPTION 'submitted attempt requires provider identity' USING ERRCODE='23514';
        END IF;
    END IF;
    IF NEW.state='unknown' AND NEW.provider_request_id IS NOT NULL AND NEW.provider_id IS NULL THEN
        -- Preserve recovery of legacy pre-0011 submitted rows. New submitting
        -- attempts that know a provider request ID must also persist provider_id.
        IF TG_OP='INSERT' OR OLD.state IS DISTINCT FROM 'submitted' OR OLD.provider_id IS NOT NULL OR OLD.provider_request_id IS DISTINCT FROM NEW.provider_request_id THEN
            RAISE EXCEPTION 'known provider request requires provider identity' USING ERRCODE='23514';
        END IF;
    END IF;
    IF NEW.provider_request_id IS NULL AND (NEW.provider_id IS NOT NULL OR NEW.external_task_id IS NOT NULL) THEN
        RAISE EXCEPTION 'provider identity/task requires provider request id' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER run_attempt_provider_identity BEFORE INSERT OR UPDATE OF state,provider_id,provider_request_id,external_task_id ON execution.run_attempts FOR EACH ROW EXECUTE FUNCTION execution.require_provider_identity();
REVOKE ALL ON FUNCTION execution.require_provider_identity() FROM PUBLIC;

CREATE FUNCTION execution.require_provider_observation_identity() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
    IF NEW.provider_id IS NULL THEN
        RAISE EXCEPTION 'new provider observation requires provider identity' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER provider_observation_identity BEFORE INSERT ON execution.provider_observations FOR EACH ROW EXECUTE FUNCTION execution.require_provider_observation_identity();
REVOKE ALL ON FUNCTION execution.require_provider_observation_identity() FROM PUBLIC;

-- New observations are auto-reconcilable only when the Attempt has a durable
-- provider identity. Legacy observations remain readable but are not routed.
CREATE OR REPLACE FUNCTION execution.check_provider_result_bundle() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$
DECLARE w text; r text; run_state text; run_updated timestamptz; job_state text; job_stopped timestamptz; expected_state text;
BEGIN
    IF TG_TABLE_NAME='runs' THEN
        IF OLD.state NOT IN ('running','reconciling','cancel_requested') OR NEW.state NOT IN ('succeeded','failed','canceled') THEN RETURN NULL; END IF;
        w:=NEW.workspace_id; r:=NEW.id;
    ELSIF TG_TABLE_NAME='jobs' THEN
        IF NEW.state<>'finished' THEN RETURN NULL; END IF;
        w:=NEW.workspace_id; r:=NEW.run_id;
    ELSE
        w:=NEW.workspace_id; r:=NEW.run_id;
    END IF;
    SELECT x.state,x.updated_at INTO run_state,run_updated FROM execution.runs x WHERE x.workspace_id=w AND x.id=r;
    SELECT j.state,j.stopped_at INTO job_state,job_stopped FROM execution.jobs j WHERE j.workspace_id=w AND j.run_id=r;
    IF TG_TABLE_NAME='provider_observations' THEN
        IF EXISTS(SELECT 1 FROM execution.provider_observations p WHERE p.workspace_id=w AND p.run_id=r AND p.observation_id<>NEW.observation_id AND p.state IN ('succeeded','failed','canceled')) THEN
            RAISE EXCEPTION 'provider result already terminal' USING ERRCODE='23514';
        END IF;
        IF NOT EXISTS(SELECT 1 FROM execution.run_attempts a WHERE a.workspace_id=w AND a.run_id=r AND a.attempt_no=NEW.attempt_no AND a.state IN ('submitted','unknown') AND a.provider_id=NEW.provider_id AND a.provider_request_id=NEW.provider_request_id AND (NEW.external_task_id IS NULL OR a.external_task_id=NEW.external_task_id)) THEN
            RAISE EXCEPTION 'provider observation does not match routable submitted attempt' USING ERRCODE='23514';
        END IF;
        IF NEW.state='pending' THEN
            IF run_state NOT IN ('running','reconciling','cancel_requested') OR job_state NOT IN ('provider_waiting','reconciling') THEN
                RAISE EXCEPTION 'pending provider observation has invalid execution state' USING ERRCODE='23514';
            END IF;
            RETURN NULL;
        END IF;
        expected_state:=NEW.state;
        IF run_state<>expected_state OR run_updated<>NEW.observed_at OR job_state<>'finished' OR job_stopped<>NEW.observed_at THEN
            RAISE EXCEPTION 'incomplete terminal provider result bundle' USING ERRCODE='23514';
        END IF;
        RETURN NULL;
    END IF;
    IF TG_TABLE_NAME='runs' THEN
        IF NOT EXISTS(SELECT 1 FROM execution.provider_observations p WHERE p.workspace_id=w AND p.run_id=r AND p.state=NEW.state AND p.observed_at=NEW.updated_at) OR job_state<>'finished' OR job_stopped<>NEW.updated_at THEN
            RAISE EXCEPTION 'terminal Run lacks provider result evidence' USING ERRCODE='23514';
        END IF;
        RETURN NULL;
    END IF;
    IF NOT EXISTS(SELECT 1 FROM execution.provider_observations p WHERE p.workspace_id=w AND p.run_id=r AND p.state=run_state AND p.observed_at=NEW.stopped_at) OR run_state NOT IN ('succeeded','failed','canceled') OR run_updated<>NEW.stopped_at THEN
        RAISE EXCEPTION 'finished Job lacks provider result evidence' USING ERRCODE='23514';
    END IF;
    RETURN NULL;
END $$;
