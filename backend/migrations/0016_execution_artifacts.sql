-- Owner: execution. Artifacts are stable user-facing result records, distinct
-- from provider control evidence. v1 stores bounded JSON inline; object storage
-- and signed URLs are deliberately deferred.
CREATE TABLE execution.artifacts (
    workspace_id text NOT NULL,
    run_id text NOT NULL,
    id text NOT NULL CHECK (id ~ '^[A-Za-z0-9._:-]{1,160}$'),
    kind text NOT NULL CHECK (kind='provider_result'),
    media_type text NOT NULL CHECK (media_type='application/json'),
    source_observation_id text NOT NULL CHECK (source_observation_id ~ '^[A-Za-z0-9._:-]{1,200}$'),
    content_json jsonb NOT NULL CHECK (pg_column_size(content_json)<=1048576),
    created_at timestamptz NOT NULL,
    PRIMARY KEY(workspace_id,id),
    UNIQUE(workspace_id,run_id,kind),
    FOREIGN KEY(workspace_id,run_id) REFERENCES execution.runs(workspace_id,id),
    FOREIGN KEY(workspace_id,run_id,source_observation_id)
      REFERENCES execution.provider_observations(workspace_id,run_id,observation_id)
);
ALTER TABLE execution.artifacts ENABLE ROW LEVEL SECURITY;
ALTER TABLE execution.artifacts FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON execution.artifacts
 USING(workspace_id=nullif(current_setting('mender.workspace_id',true),''))
 WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));
REVOKE ALL ON execution.artifacts FROM PUBLIC;

-- Upgrade safety: every previously persisted succeeded provider observation is
-- materialized exactly once. Failed/canceled/pending evidence has no result artifact.
INSERT INTO execution.artifacts(workspace_id,run_id,id,kind,media_type,source_observation_id,content_json,created_at)
SELECT p.workspace_id,p.run_id,'art_'||p.run_id,'provider_result','application/json',p.observation_id,p.result_json,p.observed_at
FROM execution.provider_observations p
WHERE p.state='succeeded';

-- Both directions are checked at commit so terminal convergence can insert the
-- provider evidence and Artifact in either statement order inside one transaction.
CREATE FUNCTION execution.check_provider_artifact_bundle() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$
DECLARE w text; r text; observation text;
BEGIN
    IF TG_TABLE_NAME='provider_observations' THEN
        IF NEW.state<>'succeeded' THEN RETURN NULL; END IF;
        w:=NEW.workspace_id; r:=NEW.run_id; observation:=NEW.observation_id;
    ELSE
        w:=NEW.workspace_id; r:=NEW.run_id; observation:=NEW.source_observation_id;
    END IF;
    IF NOT EXISTS(
        SELECT 1 FROM execution.provider_observations p
        JOIN execution.artifacts a
          ON (a.workspace_id,a.run_id,a.source_observation_id)=(p.workspace_id,p.run_id,p.observation_id)
        WHERE p.workspace_id=w AND p.run_id=r AND p.observation_id=observation
          AND p.state='succeeded' AND p.error_code IS NULL AND p.result_json IS NOT NULL
          AND a.id='art_'||r AND a.kind='provider_result' AND a.media_type='application/json'
          AND a.content_json=p.result_json AND a.created_at=p.observed_at
    ) THEN RAISE EXCEPTION 'incomplete provider result artifact bundle' USING ERRCODE='23514'; END IF;
    RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER provider_result_artifact_observation
 AFTER INSERT ON execution.provider_observations DEFERRABLE INITIALLY DEFERRED
 FOR EACH ROW EXECUTE FUNCTION execution.check_provider_artifact_bundle();
CREATE CONSTRAINT TRIGGER provider_result_artifact_record
 AFTER INSERT OR UPDATE ON execution.artifacts DEFERRABLE INITIALLY DEFERRED
 FOR EACH ROW EXECUTE FUNCTION execution.check_provider_artifact_bundle();
REVOKE ALL ON FUNCTION execution.check_provider_artifact_bundle() FROM PUBLIC;
