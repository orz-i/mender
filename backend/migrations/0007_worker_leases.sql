-- Owner: execution. Durable local worker-control facts only; no supplier call or billing mutation.
ALTER TABLE execution.jobs DROP CONSTRAINT job_stop_state;
ALTER TABLE execution.jobs ALTER COLUMN blocked_reason DROP NOT NULL;
ALTER TABLE execution.jobs
    ADD COLUMN available_at timestamptz,
    ADD COLUMN priority integer NOT NULL DEFAULT 0,
    ADD COLUMN lease_owner text,
    ADD COLUMN lease_until timestamptz,
    ADD COLUMN lease_generation bigint NOT NULL DEFAULT 0,
    ADD COLUMN attempt_count integer NOT NULL DEFAULT 0,
    ADD COLUMN max_attempts integer NOT NULL DEFAULT 3,
    ADD COLUMN updated_at timestamptz;
UPDATE execution.jobs SET available_at=created_at,updated_at=created_at;
ALTER TABLE execution.jobs ALTER COLUMN available_at SET NOT NULL;
ALTER TABLE execution.jobs ALTER COLUMN updated_at SET NOT NULL;
ALTER TABLE execution.jobs ADD CONSTRAINT job_worker_state CHECK (
    state IN ('blocked','queued','leased','canceled')
    AND available_at>=created_at AND updated_at>=created_at
    AND lease_generation>=0 AND attempt_count>=0 AND max_attempts BETWEEN 1 AND 100
    AND lease_generation=attempt_count AND attempt_count<=max_attempts
    AND (
        (state='blocked' AND blocked_reason IN ('executor_not_configured','attempt_limit_reached') AND stopped_at IS NULL AND lease_owner IS NULL AND lease_until IS NULL)
        OR (state='queued' AND blocked_reason IS NULL AND stopped_at IS NULL AND lease_owner IS NULL AND lease_until IS NULL)
        OR (state='leased' AND blocked_reason IS NULL AND stopped_at IS NULL AND lease_owner ~ '^[A-Za-z0-9_-]{1,128}$' AND lease_until>updated_at)
        OR (state='canceled' AND (blocked_reason IS NULL OR blocked_reason IN ('executor_not_configured','attempt_limit_reached')) AND stopped_at IS NOT NULL AND stopped_at>=created_at AND lease_owner IS NULL AND lease_until IS NULL)
    )
);
CREATE INDEX execution_job_ready ON execution.jobs(workspace_id,priority DESC,available_at,created_at,run_id) WHERE state='queued';
CREATE INDEX execution_job_expired_lease ON execution.jobs(workspace_id,lease_until,run_id) WHERE state='leased';

CREATE TABLE execution.run_attempts (
    workspace_id text NOT NULL,
    run_id text NOT NULL,
    attempt_no integer NOT NULL CHECK (attempt_no BETWEEN 1 AND 100),
    lease_generation bigint NOT NULL CHECK (lease_generation=attempt_no),
    lease_owner text NOT NULL CHECK (lease_owner ~ '^[A-Za-z0-9_-]{1,128}$'),
    state text NOT NULL CHECK (state IN ('leased','released','expired')),
    leased_at timestamptz NOT NULL,
    lease_until timestamptz NOT NULL CHECK (lease_until>leased_at),
    finished_at timestamptz,
    PRIMARY KEY(workspace_id,run_id,attempt_no),
    UNIQUE(workspace_id,run_id,lease_generation),
    FOREIGN KEY(workspace_id,run_id) REFERENCES execution.jobs(workspace_id,run_id),
    CHECK ((state='leased' AND finished_at IS NULL) OR (state IN ('released','expired') AND finished_at IS NOT NULL AND finished_at>=leased_at))
);
ALTER TABLE execution.run_attempts ENABLE ROW LEVEL SECURITY;
ALTER TABLE execution.run_attempts FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON execution.run_attempts
 USING(workspace_id=nullif(current_setting('mender.workspace_id',true),''))
 WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));
REVOKE ALL ON execution.run_attempts FROM PUBLIC;

-- Deferred checks protect the final same-domain lease/attempt bundle. Admission only INSERTs
-- blocked jobs, and cancellation only writes canceled jobs, so they do not need Attempt access.
CREATE FUNCTION execution.check_worker_lease_bundle() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$
DECLARE w text; r text; g bigint; owner text; until_at timestamptz;
BEGIN
    w:=NEW.workspace_id; r:=NEW.run_id;
    IF TG_TABLE_NAME='jobs' THEN
        IF NEW.state<>'leased' THEN RETURN NULL; END IF;
        g:=NEW.lease_generation; owner:=NEW.lease_owner; until_at:=NEW.lease_until;
    ELSE
        IF NEW.state<>'leased' THEN RETURN NULL; END IF;
        g:=NEW.lease_generation; owner:=NEW.lease_owner; until_at:=NEW.lease_until;
    END IF;
    IF NOT EXISTS(
        SELECT 1 FROM execution.jobs j
        JOIN execution.run_attempts a ON (a.workspace_id,a.run_id,a.lease_generation)=(j.workspace_id,j.run_id,j.lease_generation)
        WHERE j.workspace_id=w AND j.run_id=r AND j.state='leased' AND a.state='leased'
          AND j.lease_generation=g AND j.lease_owner=owner AND a.lease_owner=owner
          AND j.lease_until=until_at AND a.lease_until=until_at
    ) THEN RAISE EXCEPTION 'incomplete worker lease bundle' USING ERRCODE='23514'; END IF;
    RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER worker_job_lease_bundle AFTER UPDATE ON execution.jobs DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION execution.check_worker_lease_bundle();
CREATE CONSTRAINT TRIGGER worker_attempt_lease_bundle AFTER INSERT OR UPDATE ON execution.run_attempts DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION execution.check_worker_lease_bundle();
REVOKE ALL ON FUNCTION execution.check_worker_lease_bundle() FROM PUBLIC;
