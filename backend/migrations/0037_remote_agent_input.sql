-- Owners: execution / supply / identity. Remote Agent supplemental input is a
-- bounded control-plane capability mapped into the existing Run/Attempt model.
-- It is not a chat transcript and stores no Provider credential or raw answer.

ALTER TABLE supply.deployments
    ADD COLUMN input_endpoint_url text,
    ADD COLUMN input_http_method text;

ALTER TABLE supply.deployments
    ADD CONSTRAINT supply_input_control_pair CHECK (
        (input_endpoint_url IS NULL AND input_http_method IS NULL)
        OR (
            transport_kind='agent_http'
            AND input_endpoint_url IS NOT NULL
            AND char_length(input_endpoint_url) BETWEEN 1 AND 2048
            AND input_endpoint_url !~ '[[:cntrl:]]'
            AND input_http_method='POST'
        )
    );

COMMENT ON COLUMN supply.deployments.input_endpoint_url IS
    'Optional reviewed Remote Agent supplemental-input endpoint. Provider/task/request handles are sent in the bounded request body, never interpolated into this URL.';

CREATE TABLE execution.agent_input_requests (
    workspace_id text NOT NULL CHECK (workspace_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    run_id text NOT NULL CHECK (run_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    attempt_no integer NOT NULL CHECK (attempt_no BETWEEN 1 AND 100),
    input_request_id text NOT NULL CHECK (input_request_id ~ '^[A-Za-z0-9_.:-]{1,200}$'),
    provider_id text NOT NULL CHECK (provider_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    provider_request_id text NOT NULL CHECK (char_length(provider_request_id) BETWEEN 1 AND 512 AND provider_request_id !~ '[[:cntrl:]]'),
    external_task_id text NOT NULL CHECK (char_length(external_task_id) BETWEEN 1 AND 512 AND external_task_id !~ '[[:cntrl:]]'),
    prompt text NOT NULL CHECK (char_length(prompt) BETWEEN 1 AND 2000 AND prompt !~ '[\x00-\x08\x0B\x0C\x0E-\x1F\x7F]'),
    input_schema_json jsonb NOT NULL CHECK (jsonb_typeof(input_schema_json)='object' AND octet_length(input_schema_json::text) <= 65536),
    state text NOT NULL CHECK (state IN ('pending','sending','unknown','submitted','superseded')),
    answer_sha256 text CHECK (answer_sha256 IS NULL OR answer_sha256 ~ '^[a-f0-9]{64}$'),
    submission_id text CHECK (submission_id IS NULL OR submission_id ~ '^[A-Za-z0-9_.:-]{1,200}$'),
    requested_at timestamptz NOT NULL,
    sending_at timestamptz,
    submitted_at timestamptz,
    updated_at timestamptz NOT NULL CHECK (updated_at>=requested_at),
    PRIMARY KEY(workspace_id,run_id,input_request_id),
    FOREIGN KEY(workspace_id,run_id,attempt_no) REFERENCES execution.run_attempts(workspace_id,run_id,attempt_no),
    CHECK (
        (state='pending' AND answer_sha256 IS NULL AND submission_id IS NULL AND sending_at IS NULL AND submitted_at IS NULL)
        OR (state='sending' AND answer_sha256 IS NOT NULL AND submission_id IS NOT NULL AND sending_at IS NOT NULL AND submitted_at IS NULL)
        OR (state='unknown' AND answer_sha256 IS NOT NULL AND submission_id IS NOT NULL AND sending_at IS NOT NULL AND submitted_at IS NULL)
        OR (state='submitted' AND answer_sha256 IS NOT NULL AND submission_id IS NOT NULL AND sending_at IS NOT NULL AND submitted_at IS NOT NULL)
        OR (state='superseded')
    )
);

CREATE UNIQUE INDEX agent_input_single_outstanding
    ON execution.agent_input_requests(workspace_id,run_id)
    WHERE state IN ('pending','sending','unknown');
CREATE INDEX agent_input_pending_timeline
    ON execution.agent_input_requests(workspace_id,requested_at,run_id,input_request_id)
    WHERE state IN ('pending','sending','unknown');

ALTER TABLE execution.agent_input_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE execution.agent_input_requests FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON execution.agent_input_requests
    USING(workspace_id=nullif(current_setting('mender.workspace_id',true),''))
    WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));
REVOKE ALL ON execution.agent_input_requests FROM PUBLIC;

ALTER TABLE execution.provider_callback_inbox
    DROP CONSTRAINT provider_callback_inbox_observation_state_check,
    ADD CONSTRAINT provider_callback_inbox_observation_state_check
        CHECK (observation_state IN ('pending','succeeded','failed','canceled','input_required'));

ALTER TABLE identity.run_delegations
    DROP CONSTRAINT run_delegations_scopes_check,
    DROP CONSTRAINT run_delegations_scopes_check1,
    ADD CONSTRAINT run_delegations_scopes_check CHECK (cardinality(scopes) BETWEEN 1 AND 3),
    ADD CONSTRAINT run_delegations_scopes_check1 CHECK (scopes <@ ARRAY['run:read','run:cancel','run:input']::text[]);

ALTER TABLE identity.api_keys
    DROP CONSTRAINT api_keys_scopes_check,
    ADD CONSTRAINT api_keys_scopes_check CHECK (
        cardinality(scopes) > 0
        AND scopes <@ ARRAY['run:read','run:cancel','run:create','run:input']::text[]
        AND array_position(scopes,NULL) IS NULL
    );

COMMENT ON TABLE execution.agent_input_requests IS
    'Execution-owned, single-outstanding Remote Agent input request metadata. Raw submitted answer bytes and Provider credentials are intentionally excluded.';
