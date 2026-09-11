-- Owner: execution. Provider observations are append-only evidence used to
-- resolve asynchronous submitted/reconciling Runs; no provider I/O occurs here.

ALTER TABLE execution.jobs DROP CONSTRAINT job_worker_state;
ALTER TABLE execution.jobs ADD CONSTRAINT job_worker_state CHECK (
    state IN ('blocked','queued','leased','provider_waiting','reconciling','finished','canceled')
    AND available_at>=created_at AND updated_at>=created_at
    AND lease_generation>=0 AND attempt_count>=0 AND max_attempts BETWEEN 1 AND 100
    AND lease_generation=attempt_count AND attempt_count<=max_attempts
    AND (
        (state='blocked' AND blocked_reason IN ('executor_not_configured','attempt_limit_reached') AND stopped_at IS NULL AND lease_owner IS NULL AND lease_until IS NULL)
        OR (state='queued' AND blocked_reason IS NULL AND stopped_at IS NULL AND lease_owner IS NULL AND lease_until IS NULL)
        OR (state='leased' AND blocked_reason IS NULL AND stopped_at IS NULL AND lease_owner ~ '^[A-Za-z0-9_-]{1,128}$' AND lease_until>updated_at)
        OR (state='provider_waiting' AND blocked_reason IS NULL AND stopped_at IS NULL AND lease_owner IS NULL AND lease_until IS NULL AND lease_generation>0 AND attempt_count>0)
        OR (state='reconciling' AND blocked_reason='submission_outcome_unknown' AND stopped_at IS NULL AND lease_owner IS NULL AND lease_until IS NULL AND lease_generation>0 AND attempt_count>0)
        OR (state='finished' AND blocked_reason IS NULL AND stopped_at IS NOT NULL AND stopped_at>=created_at AND lease_owner IS NULL AND lease_until IS NULL AND lease_generation>0 AND attempt_count>0)
        OR (state='canceled' AND (blocked_reason IS NULL OR blocked_reason IN ('executor_not_configured','attempt_limit_reached')) AND stopped_at IS NOT NULL AND stopped_at>=created_at AND lease_owner IS NULL AND lease_until IS NULL)
    )
);
CREATE INDEX execution_job_provider_waiting ON execution.jobs(workspace_id,updated_at,run_id) WHERE state='provider_waiting';

-- A submitted Attempt is historical submission ownership. Once acceptance is
-- durably recorded the Job is provider_waiting and no Worker lease remains.
CREATE OR REPLACE FUNCTION execution.check_worker_lease_bundle() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$
DECLARE w text; r text; g bigint; owner text; until_at timestamptz;
BEGIN
    w:=NEW.workspace_id; r:=NEW.run_id;
    IF TG_TABLE_NAME='jobs' THEN
        IF NEW.state<>'leased' THEN RETURN NULL; END IF;
        g:=NEW.lease_generation; owner:=NEW.lease_owner; until_at:=NEW.lease_until;
    ELSE
        IF NEW.state NOT IN ('leased','submitting') THEN RETURN NULL; END IF;
        g:=NEW.lease_generation; owner:=NEW.lease_owner; until_at:=NEW.lease_until;
    END IF;
    IF NOT EXISTS(
        SELECT 1 FROM execution.jobs j
        JOIN execution.run_attempts a ON (a.workspace_id,a.run_id,a.lease_generation)=(j.workspace_id,j.run_id,j.lease_generation)
        WHERE j.workspace_id=w AND j.run_id=r AND j.state='leased' AND a.state IN ('leased','submitting')
          AND j.lease_generation=g AND j.lease_owner=owner AND a.lease_owner=owner
          AND j.lease_until=until_at AND a.lease_until=until_at
    ) THEN RAISE EXCEPTION 'incomplete worker lease bundle' USING ERRCODE='23514'; END IF;
    RETURN NULL;
END $$;

CREATE OR REPLACE FUNCTION execution.check_submission_bundle() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$
DECLARE w text; r text; run_state text; run_updated timestamptz; job_state text; job_generation bigint;
BEGIN
    IF TG_TABLE_NAME='runs' THEN
        IF NOT ((OLD.state='queued' AND NEW.state='running') OR (OLD.state IN ('queued','running') AND NEW.state='reconciling')) THEN RETURN NULL; END IF;
        w:=NEW.workspace_id; r:=NEW.id;
        IF NOT EXISTS(SELECT 1 FROM execution.run_admissions x WHERE x.workspace_id=w AND x.run_id=r) THEN RETURN NULL; END IF;
    ELSE
        w:=NEW.workspace_id; r:=NEW.run_id;
        IF TG_TABLE_NAME='run_attempts' AND NEW.state NOT IN ('submitted','unknown') THEN RETURN NULL; END IF;
        IF TG_TABLE_NAME='jobs' AND NEW.state NOT IN ('provider_waiting','reconciling') THEN RETURN NULL; END IF;
    END IF;
    SELECT x.state,x.updated_at INTO run_state,run_updated FROM execution.runs x WHERE x.workspace_id=w AND x.id=r;
    SELECT j.state,j.lease_generation INTO job_state,job_generation FROM execution.jobs j WHERE j.workspace_id=w AND j.run_id=r;
    IF TG_TABLE_NAME='runs' AND OLD.state='queued' AND NEW.state='running' THEN
        IF NOT EXISTS(SELECT 1 FROM execution.run_attempts a WHERE a.workspace_id=w AND a.run_id=r AND a.lease_generation=job_generation AND a.state='submitted' AND a.submitted_at=NEW.updated_at) OR job_state<>'provider_waiting' THEN
            RAISE EXCEPTION 'incomplete submitted Run bundle' USING ERRCODE='23514';
        END IF;
        RETURN NULL;
    END IF;
    IF TG_TABLE_NAME='runs' THEN
        IF NOT EXISTS(SELECT 1 FROM execution.run_attempts a WHERE a.workspace_id=w AND a.run_id=r AND a.lease_generation=job_generation AND a.state='unknown' AND a.unknown_at=NEW.updated_at) OR job_state<>'reconciling' THEN
            RAISE EXCEPTION 'incomplete reconciliation Run bundle' USING ERRCODE='23514';
        END IF;
        RETURN NULL;
    END IF;
    IF TG_TABLE_NAME='run_attempts' AND NEW.state='submitted' THEN
        IF run_state<>'running' OR run_updated<>NEW.submitted_at OR job_state<>'provider_waiting' OR job_generation<>NEW.lease_generation THEN
            RAISE EXCEPTION 'incomplete submitted attempt bundle' USING ERRCODE='23514';
        END IF;
        RETURN NULL;
    END IF;
    IF TG_TABLE_NAME='run_attempts' THEN
        IF run_state<>'reconciling' OR run_updated<>NEW.unknown_at OR job_state<>'reconciling' OR job_generation<>NEW.lease_generation THEN
            RAISE EXCEPTION 'incomplete unknown submission bundle' USING ERRCODE='23514';
        END IF;
        RETURN NULL;
    END IF;
    IF NEW.state='provider_waiting' THEN
        IF run_state<>'running' OR NOT EXISTS(SELECT 1 FROM execution.run_attempts a WHERE a.workspace_id=w AND a.run_id=r AND a.lease_generation=NEW.lease_generation AND a.state='submitted' AND a.submitted_at=run_updated) THEN
            RAISE EXCEPTION 'incomplete provider waiting bundle' USING ERRCODE='23514';
        END IF;
        RETURN NULL;
    END IF;
    IF NOT EXISTS(SELECT 1 FROM execution.run_attempts a WHERE a.workspace_id=w AND a.run_id=r AND a.lease_generation=NEW.lease_generation AND a.state='unknown' AND a.unknown_at=run_updated) OR run_state<>'reconciling' THEN
        RAISE EXCEPTION 'incomplete reconciliation job bundle' USING ERRCODE='23514';
    END IF;
    RETURN NULL;
END $$;

CREATE TABLE execution.provider_observations (
    workspace_id text NOT NULL,
    run_id text NOT NULL,
    observation_id text NOT NULL CHECK (observation_id ~ '^[A-Za-z0-9._:-]{1,200}$'),
    attempt_no integer NOT NULL CHECK (attempt_no BETWEEN 1 AND 100),
    provider_request_id text NOT NULL CHECK (char_length(provider_request_id) BETWEEN 1 AND 512),
    external_task_id text CHECK (external_task_id IS NULL OR char_length(external_task_id) BETWEEN 1 AND 512),
    state text NOT NULL CHECK (state IN ('pending','succeeded','failed','canceled')),
    result_json jsonb,
    error_code text,
    observed_at timestamptz NOT NULL,
    PRIMARY KEY(workspace_id,run_id,observation_id),
    FOREIGN KEY(workspace_id,run_id) REFERENCES execution.runs(workspace_id,id),
    FOREIGN KEY(workspace_id,run_id,attempt_no) REFERENCES execution.run_attempts(workspace_id,run_id,attempt_no),
    CHECK (
        (state='pending' AND result_json IS NULL AND error_code IS NULL)
        OR (state='succeeded' AND result_json IS NOT NULL AND pg_column_size(result_json)<=1048576 AND error_code IS NULL)
        OR (state='failed' AND result_json IS NULL AND error_code ~ '^[A-Za-z0-9._:-]{1,128}$')
        OR (state='canceled' AND result_json IS NULL AND error_code IS NULL)
    )
);
ALTER TABLE execution.provider_observations ENABLE ROW LEVEL SECURITY;
ALTER TABLE execution.provider_observations FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON execution.provider_observations
    USING(workspace_id=nullif(current_setting('mender.workspace_id',true),''))
    WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));
REVOKE ALL ON execution.provider_observations FROM PUBLIC;
CREATE INDEX provider_observation_pending ON execution.provider_observations(workspace_id,run_id,observed_at DESC) WHERE state='pending';

CREATE FUNCTION execution.check_provider_result_bundle() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$
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
        IF NOT EXISTS(SELECT 1 FROM execution.run_attempts a WHERE a.workspace_id=w AND a.run_id=r AND a.attempt_no=NEW.attempt_no AND a.state IN ('submitted','unknown') AND a.provider_request_id=NEW.provider_request_id AND (NEW.external_task_id IS NULL OR a.external_task_id=NEW.external_task_id)) THEN
            RAISE EXCEPTION 'provider observation does not match submitted attempt' USING ERRCODE='23514';
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
CREATE CONSTRAINT TRIGGER provider_result_observation_bundle AFTER INSERT ON execution.provider_observations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION execution.check_provider_result_bundle();
CREATE CONSTRAINT TRIGGER provider_result_run_bundle AFTER UPDATE ON execution.runs DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION execution.check_provider_result_bundle();
CREATE CONSTRAINT TRIGGER provider_result_job_bundle AFTER UPDATE ON execution.jobs DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION execution.check_provider_result_bundle();
REVOKE ALL ON FUNCTION execution.check_provider_result_bundle() FROM PUBLIC;
