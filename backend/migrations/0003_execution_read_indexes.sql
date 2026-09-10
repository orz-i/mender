-- Owner: execution. Preserve existing migrations; keyset order is locale-independent.
CREATE INDEX runs_workspace_created_id ON execution.runs
    (workspace_id, created_at DESC, id COLLATE "C" DESC);
CREATE INDEX runs_workspace_state_created_id ON execution.runs
    (workspace_id, state, created_at DESC, id COLLATE "C" DESC);
-- run_events already has (workspace_id,run_id,version) as its primary key.
