-- Owners: execution owns durable terminal settlement jobs; commerce still owns
-- all pricing/quota/settlement facts. The later process UoW coordinates both.
CREATE TABLE execution.settlement_jobs (
    workspace_id text NOT NULL,
    run_id text NOT NULL,
    observation_id text NOT NULL CHECK (observation_id ~ '^[A-Za-z0-9._:-]{1,200}$'),
    state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','finished')),
    created_at timestamptz NOT NULL,
    finished_at timestamptz,
    PRIMARY KEY(workspace_id,run_id),
    UNIQUE(workspace_id,run_id,observation_id),
    FOREIGN KEY(workspace_id,run_id) REFERENCES execution.run_admissions(workspace_id,run_id),
    FOREIGN KEY(workspace_id,run_id,observation_id) REFERENCES execution.provider_observations(workspace_id,run_id,observation_id),
    CHECK ((state='pending' AND finished_at IS NULL) OR (state='finished' AND finished_at IS NOT NULL AND finished_at>=created_at AND finished_at<'10000-01-01 UTC'))
);

-- Existing terminal provider facts are safe to backfill: ProviderResults already
-- guarantees at most one accepted terminal convergence for a Run.
INSERT INTO execution.settlement_jobs(workspace_id,run_id,observation_id,created_at)
SELECT p.workspace_id,p.run_id,p.observation_id,p.observed_at
FROM execution.provider_observations p
JOIN execution.runs r ON (r.workspace_id,r.id)=(p.workspace_id,p.run_id)
WHERE p.state IN ('succeeded','failed','canceled') AND r.state=p.state
ON CONFLICT (workspace_id,run_id) DO NOTHING;

ALTER TABLE execution.settlement_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE execution.settlement_jobs FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON execution.settlement_jobs
    USING(workspace_id=nullif(current_setting('mender.workspace_id',true),''))
    WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));
CREATE INDEX settlement_job_ready ON execution.settlement_jobs(workspace_id,state,created_at,run_id) WHERE state='pending';

CREATE FUNCTION execution.check_terminal_settlement_job() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
    IF NOT EXISTS(
        SELECT 1
        FROM execution.provider_observations p
        JOIN execution.runs r ON (r.workspace_id,r.id)=(p.workspace_id,p.run_id)
        JOIN execution.jobs j ON (j.workspace_id,j.run_id)=(p.workspace_id,p.run_id)
        WHERE p.workspace_id=NEW.workspace_id AND p.run_id=NEW.run_id AND p.observation_id=NEW.observation_id
          AND p.state IN ('succeeded','failed','canceled')
          AND r.state=p.state AND j.state='finished'
          AND NEW.created_at=p.observed_at
    ) THEN RAISE EXCEPTION 'settlement job lacks terminal execution proof' USING ERRCODE='23514'; END IF;
    RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER terminal_settlement_job_bundle
    AFTER INSERT ON execution.settlement_jobs DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION execution.check_terminal_settlement_job();

REVOKE ALL ON execution.settlement_jobs FROM PUBLIC;
REVOKE ALL ON FUNCTION execution.check_terminal_settlement_job() FROM PUBLIC;
