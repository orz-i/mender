-- Owner: execution. Object materialization is a secondary, replaceable copy of
-- the immutable inline Artifact. The object key is internal routing metadata,
-- never an authorization token and never a browser-facing path.

ALTER TABLE execution.artifacts
  ADD CONSTRAINT artifacts_workspace_run_id_key UNIQUE(workspace_id,run_id,id);

CREATE TABLE execution.artifact_objects (
    workspace_id text NOT NULL,
    run_id text NOT NULL,
    artifact_id text NOT NULL CHECK (artifact_id ~ '^[A-Za-z0-9._:-]{1,160}$'),
    object_key text NOT NULL CHECK (object_key ~ '^objects/[0-9a-f]{64}/[0-9a-f]{64}\.json$'),
    content_sha256 text NOT NULL CHECK (content_sha256 ~ '^[0-9a-f]{64}$'),
    size_bytes bigint NOT NULL CHECK (size_bytes>262144 AND size_bytes<=1048576),
    state text NOT NULL DEFAULT 'available' CHECK (state IN ('available','expired')),
    materialized_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    deleted_at timestamptz,
    PRIMARY KEY(workspace_id,artifact_id),
    UNIQUE(object_key),
    FOREIGN KEY(workspace_id,run_id,artifact_id)
      REFERENCES execution.artifacts(workspace_id,run_id,id),
    CHECK (expires_at>materialized_at),
    CHECK (
      (state='available' AND deleted_at IS NULL)
      OR (state='expired' AND deleted_at IS NOT NULL AND deleted_at>=expires_at)
    )
);
ALTER TABLE execution.artifact_objects ENABLE ROW LEVEL SECURITY;
ALTER TABLE execution.artifact_objects FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON execution.artifact_objects
 USING(workspace_id=nullif(current_setting('mender.workspace_id',true),''))
 WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));
REVOKE ALL ON execution.artifact_objects FROM PUBLIC;

-- Object identity, digest, size and retention are immutable. Physical cleanup
-- is represented only by the one-way available -> expired transition.
CREATE FUNCTION execution.guard_artifact_object_lifecycle() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        RAISE EXCEPTION 'Artifact object metadata is append-only' USING ERRCODE='23514';
    END IF;
    IF NEW.workspace_id IS DISTINCT FROM OLD.workspace_id
       OR NEW.run_id IS DISTINCT FROM OLD.run_id
       OR NEW.artifact_id IS DISTINCT FROM OLD.artifact_id
       OR NEW.object_key IS DISTINCT FROM OLD.object_key
       OR NEW.content_sha256 IS DISTINCT FROM OLD.content_sha256
       OR NEW.size_bytes IS DISTINCT FROM OLD.size_bytes
       OR NEW.materialized_at IS DISTINCT FROM OLD.materialized_at
       OR NEW.expires_at IS DISTINCT FROM OLD.expires_at THEN
        RAISE EXCEPTION 'Artifact object identity is immutable' USING ERRCODE='23514';
    END IF;
    IF OLD.state='available' AND NEW.state='expired' AND OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL AND NEW.deleted_at>=NEW.expires_at THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'Artifact object lifecycle cannot regress' USING ERRCODE='23514';
END $$;
CREATE TRIGGER artifact_object_lifecycle_guard
 BEFORE UPDATE OR DELETE ON execution.artifact_objects
 FOR EACH ROW EXECUTE FUNCTION execution.guard_artifact_object_lifecycle();
REVOKE ALL ON FUNCTION execution.guard_artifact_object_lifecycle() FROM PUBLIC;

CREATE INDEX artifact_objects_expiry
 ON execution.artifact_objects(workspace_id,expires_at,artifact_id)
 WHERE state='available';
