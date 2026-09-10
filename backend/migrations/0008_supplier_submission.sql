-- Owner: execution. Durable supplier-submission protocol only; no network call is performed here.
ALTER TABLE execution.jobs DROP CONSTRAINT job_worker_state;
ALTER TABLE execution.jobs DROP CONSTRAINT IF EXISTS jobs_blocked_reason_check;
ALTER TABLE execution.jobs ADD CONSTRAINT job_worker_state CHECK (
    state IN ('blocked','queued','leased','reconciling','canceled')
    AND available_at>=created_at AND updated_at>=created_at
    AND lease_generation>=0 AND attempt_count>=0 AND max_attempts BETWEEN 1 AND 100
    AND lease_generation=attempt_count AND attempt_count<=max_attempts
    AND (
        (state='blocked' AND blocked_reason IN ('executor_not_configured','attempt_limit_reached') AND stopped_at IS NULL AND lease_owner IS NULL AND lease_until IS NULL)
        OR (state='queued' AND blocked_reason IS NULL AND stopped_at IS NULL AND lease_owner IS NULL AND lease_until IS NULL)
        OR (state='leased' AND blocked_reason IS NULL AND stopped_at IS NULL AND lease_owner ~ '^[A-Za-z0-9_-]{1,128}$' AND lease_until>updated_at)
        OR (state='reconciling' AND blocked_reason='submission_outcome_unknown' AND stopped_at IS NULL AND lease_owner IS NULL AND lease_until IS NULL AND lease_generation>0 AND attempt_count>0)
        OR (state='canceled' AND (blocked_reason IS NULL OR blocked_reason IN ('executor_not_configured','attempt_limit_reached')) AND stopped_at IS NOT NULL AND stopped_at>=created_at AND lease_owner IS NULL AND lease_until IS NULL)
    )
);
CREATE INDEX execution_job_reconciling ON execution.jobs(workspace_id,updated_at,run_id) WHERE state='reconciling';

-- 0007 intentionally used inline/unnamed CHECK clauses. PostgreSQL-generated
-- names for multi-column CHECKs are not a stable migration contract, so replace
-- the entire CHECK set with explicit names before adding submission states.
DO $$
DECLARE c record;
BEGIN
    FOR c IN
        SELECT conname FROM pg_constraint
        WHERE conrelid='execution.run_attempts'::regclass AND contype='c'
    LOOP
        EXECUTE format('ALTER TABLE execution.run_attempts DROP CONSTRAINT %I', c.conname);
    END LOOP;
END $$;
ALTER TABLE execution.run_attempts
    ADD COLUMN submission_key text,
    ADD COLUMN submission_intent_at timestamptz,
    ADD COLUMN provider_request_id text,
    ADD COLUMN external_task_id text,
    ADD COLUMN submitted_at timestamptz,
    ADD COLUMN unknown_at timestamptz,
    ADD COLUMN unknown_reason text;
ALTER TABLE execution.run_attempts ADD CONSTRAINT run_attempts_attempt_no_check CHECK (attempt_no BETWEEN 1 AND 100);
ALTER TABLE execution.run_attempts ADD CONSTRAINT run_attempts_generation_check CHECK (lease_generation=attempt_no);
ALTER TABLE execution.run_attempts ADD CONSTRAINT run_attempts_owner_check CHECK (lease_owner ~ '^[A-Za-z0-9_-]{1,128}$');
ALTER TABLE execution.run_attempts ADD CONSTRAINT run_attempts_lease_window_check CHECK (lease_until>leased_at);
ALTER TABLE execution.run_attempts ADD CONSTRAINT run_attempts_state_check CHECK (state IN ('leased','released','expired','submitting','submitted','unknown'));
ALTER TABLE execution.run_attempts ADD CONSTRAINT run_attempts_submission_check CHECK (
    (state='leased' AND finished_at IS NULL AND submission_key IS NULL AND submission_intent_at IS NULL AND provider_request_id IS NULL AND external_task_id IS NULL AND submitted_at IS NULL AND unknown_at IS NULL AND unknown_reason IS NULL)
    OR (state IN ('released','expired') AND finished_at IS NOT NULL AND finished_at>=leased_at AND submission_key IS NULL AND submission_intent_at IS NULL AND provider_request_id IS NULL AND external_task_id IS NULL AND submitted_at IS NULL AND unknown_at IS NULL AND unknown_reason IS NULL)
    OR (state='submitting' AND finished_at IS NULL AND submission_key ~ '^[A-Za-z0-9._:-]{8,200}$' AND submission_intent_at>=leased_at AND submission_intent_at<lease_until AND provider_request_id IS NULL AND external_task_id IS NULL AND submitted_at IS NULL AND unknown_at IS NULL AND unknown_reason IS NULL)
    OR (state='submitted' AND finished_at IS NULL AND submission_key ~ '^[A-Za-z0-9._:-]{8,200}$' AND submission_intent_at>=leased_at AND submission_intent_at<lease_until AND provider_request_id IS NOT NULL AND char_length(provider_request_id) BETWEEN 1 AND 512 AND (external_task_id IS NULL OR char_length(external_task_id) BETWEEN 1 AND 512) AND submitted_at>=submission_intent_at AND submitted_at<lease_until AND unknown_at IS NULL AND unknown_reason IS NULL)
    OR (state='unknown' AND finished_at=unknown_at AND submission_key ~ '^[A-Za-z0-9._:-]{8,200}$' AND submission_intent_at>=leased_at AND submission_intent_at<lease_until AND (provider_request_id IS NULL OR char_length(provider_request_id) BETWEEN 1 AND 512) AND (external_task_id IS NULL OR char_length(external_task_id) BETWEEN 1 AND 512) AND (submitted_at IS NULL OR (submitted_at>=submission_intent_at AND submitted_at<lease_until)) AND unknown_at>=submission_intent_at AND char_length(unknown_reason) BETWEEN 1 AND 500)
);
CREATE UNIQUE INDEX run_attempt_submission_key ON execution.run_attempts(workspace_id,submission_key) WHERE submission_key IS NOT NULL;

-- The existing deferred lease bundle now accepts the in-flight submission states.
CREATE OR REPLACE FUNCTION execution.check_worker_lease_bundle() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$
DECLARE w text; r text; g bigint; owner text; until_at timestamptz;
BEGIN
    w:=NEW.workspace_id; r:=NEW.run_id;
    IF TG_TABLE_NAME='jobs' THEN
        IF NEW.state<>'leased' THEN RETURN NULL; END IF;
        g:=NEW.lease_generation; owner:=NEW.lease_owner; until_at:=NEW.lease_until;
    ELSE
        IF NEW.state NOT IN ('leased','submitting','submitted') THEN RETURN NULL; END IF;
        g:=NEW.lease_generation; owner:=NEW.lease_owner; until_at:=NEW.lease_until;
    END IF;
    IF NOT EXISTS(
        SELECT 1 FROM execution.jobs j
        JOIN execution.run_attempts a ON (a.workspace_id,a.run_id,a.lease_generation)=(j.workspace_id,j.run_id,j.lease_generation)
        WHERE j.workspace_id=w AND j.run_id=r AND j.state='leased' AND a.state IN ('leased','submitting','submitted')
          AND j.lease_generation=g AND j.lease_owner=owner AND a.lease_owner=owner
          AND j.lease_until=until_at AND a.lease_until=until_at
    ) THEN RAISE EXCEPTION 'incomplete worker lease bundle' USING ERRCODE='23514'; END IF;
    RETURN NULL;
END $$;

CREATE FUNCTION execution.check_submission_bundle() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$
DECLARE w text; r text; run_state text; run_updated timestamptz; job_state text; job_generation bigint;
BEGIN
    IF TG_TABLE_NAME='runs' THEN
        -- Submission proof only owns the first queued -> running transition and
        -- queued/running -> reconciling transitions caused by submission uncertainty.
        -- It must not constrain later waiting_input -> running resumes.
        IF NOT ((OLD.state='queued' AND NEW.state='running') OR (OLD.state IN ('queued','running') AND NEW.state='reconciling')) THEN RETURN NULL; END IF;
        w:=NEW.workspace_id; r:=NEW.id;
        IF NOT EXISTS(SELECT 1 FROM execution.run_admissions x WHERE x.workspace_id=w AND x.run_id=r) THEN RETURN NULL; END IF;
    ELSE
        w:=NEW.workspace_id; r:=NEW.run_id;
        IF TG_TABLE_NAME='run_attempts' AND NEW.state NOT IN ('submitted','unknown') THEN RETURN NULL; END IF;
        IF TG_TABLE_NAME='jobs' AND NEW.state<>'reconciling' THEN RETURN NULL; END IF;
    END IF;
    SELECT x.state,x.updated_at INTO run_state,run_updated FROM execution.runs x WHERE x.workspace_id=w AND x.id=r;
    SELECT j.state,j.lease_generation INTO job_state,job_generation FROM execution.jobs j WHERE j.workspace_id=w AND j.run_id=r;
    IF TG_TABLE_NAME='runs' AND OLD.state='queued' AND NEW.state='running' THEN
        IF NOT EXISTS(SELECT 1 FROM execution.run_attempts a WHERE a.workspace_id=w AND a.run_id=r AND a.lease_generation=job_generation AND a.state='submitted' AND a.submitted_at=NEW.updated_at) OR job_state<>'leased' THEN
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
        IF run_state<>'running' OR run_updated<>NEW.submitted_at OR job_state<>'leased' OR job_generation<>NEW.lease_generation THEN
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
    IF NOT EXISTS(SELECT 1 FROM execution.run_attempts a WHERE a.workspace_id=NEW.workspace_id AND a.run_id=NEW.run_id AND a.lease_generation=NEW.lease_generation AND a.state='unknown' AND a.unknown_at=run_updated) OR run_state<>'reconciling' THEN
        RAISE EXCEPTION 'incomplete reconciliation job bundle' USING ERRCODE='23514';
    END IF;
    RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER submission_attempt_bundle AFTER UPDATE ON execution.run_attempts DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION execution.check_submission_bundle();
CREATE CONSTRAINT TRIGGER submission_job_bundle AFTER UPDATE ON execution.jobs DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION execution.check_submission_bundle();
CREATE CONSTRAINT TRIGGER submission_run_bundle AFTER UPDATE ON execution.runs DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION execution.check_submission_bundle();
REVOKE ALL ON FUNCTION execution.check_submission_bundle() FROM PUBLIC;
