-- Owner: execution. No admission/insert grant is given to the query/cancel API.
CREATE SCHEMA execution;
CREATE TABLE execution.runs (
    workspace_id text NOT NULL CHECK (workspace_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    id text NOT NULL CHECK (id ~ '^[A-Za-z0-9_-]{1,128}$'),
    state text NOT NULL CHECK (state IN ('queued','running','waiting_input','cancel_requested','reconciling','succeeded','failed','canceled','timed_out')),
    version bigint NOT NULL CHECK (version > 0),
    created_at timestamptz NOT NULL CHECK (created_at >= '0001-01-01 UTC'),
    updated_at timestamptz NOT NULL CHECK (updated_at >= created_at AND updated_at < '10000-01-01 UTC'),
    PRIMARY KEY (workspace_id,id),
    CHECK ((state = 'queued') = (version = 1))
);
CREATE TABLE execution.run_events (
    workspace_id text NOT NULL,
    run_id text NOT NULL,
    version bigint NOT NULL CHECK (version > 1),
    state text NOT NULL,
    subject_id text NOT NULL CHECK (subject_id <> ''),
    credential_id text NOT NULL CHECK (credential_id <> ''),
    occurred_at timestamptz NOT NULL,
    reason text NOT NULL DEFAULT '' CHECK (char_length(reason) <= 500),
    PRIMARY KEY (workspace_id,run_id,version),
    FOREIGN KEY (workspace_id,run_id) REFERENCES execution.runs(workspace_id,id)
);
ALTER TABLE execution.runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE execution.runs FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON execution.runs
    USING (workspace_id = nullif(current_setting('mender.workspace_id',true),''))
    WITH CHECK (workspace_id = nullif(current_setting('mender.workspace_id',true),''));
ALTER TABLE execution.run_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE execution.run_events FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON execution.run_events
    USING (workspace_id = nullif(current_setting('mender.workspace_id',true),''))
    WITH CHECK (workspace_id = nullif(current_setting('mender.workspace_id',true),''));
REVOKE ALL ON SCHEMA execution FROM PUBLIC;
REVOKE ALL ON ALL TABLES IN SCHEMA execution FROM PUBLIC;
