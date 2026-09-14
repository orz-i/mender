-- Owner: execution. Verified provider callback delivery metadata only.
-- Raw request bodies, signatures, secrets and result/error payloads are never stored here.

CREATE TABLE execution.provider_callback_inbox (
    receipt_id text PRIMARY KEY CHECK (receipt_id ~ '^[a-f0-9]{64}$'),
    workspace_id text NOT NULL CHECK (workspace_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    provider_id text NOT NULL CHECK (provider_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    event_id text NOT NULL CHECK (event_id ~ '^[A-Za-z0-9_.:-]{1,200}$'),
    body_sha256 text NOT NULL CHECK (body_sha256 ~ '^[a-f0-9]{64}$'),
    key_id text NOT NULL CHECK (key_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    signed_at timestamptz NOT NULL,
    received_at timestamptz NOT NULL,
    last_received_at timestamptz NOT NULL,
    delivery_count integer NOT NULL DEFAULT 1 CHECK (delivery_count BETWEEN 1 AND 1000000),
    run_id text NOT NULL CHECK (run_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    attempt_no integer NOT NULL CHECK (attempt_no BETWEEN 1 AND 100),
    provider_request_id text NOT NULL CHECK (char_length(provider_request_id) BETWEEN 1 AND 512 AND provider_request_id !~ '[[:cntrl:]]'),
    external_task_id text CHECK (external_task_id IS NULL OR (char_length(external_task_id) BETWEEN 1 AND 512 AND external_task_id !~ '[[:cntrl:]]')),
    observation_id text NOT NULL CHECK (observation_id ~ '^[A-Za-z0-9_.:-]{1,200}$'),
    observation_state text NOT NULL CHECK (observation_state IN ('pending','succeeded','failed','canceled')),
    observed_at timestamptz NOT NULL,
    disposition text NOT NULL CHECK (disposition IN ('pending','accepted','quarantined')),
    reason_code text CHECK (reason_code IS NULL OR reason_code ~ '^[A-Za-z0-9_.:-]{1,128}$'),
    processed_at timestamptz,
    UNIQUE(provider_id,event_id,body_sha256),
    CHECK (last_received_at>=received_at),
    CHECK ((disposition='pending' AND processed_at IS NULL AND reason_code IS NULL)
        OR (disposition='accepted' AND processed_at IS NOT NULL AND reason_code IS NULL)
        OR (disposition='quarantined' AND processed_at IS NOT NULL AND reason_code IS NOT NULL))
);

CREATE INDEX provider_callback_event_lookup
    ON execution.provider_callback_inbox(provider_id,event_id,received_at,receipt_id);
CREATE INDEX provider_callback_admin_timeline
    ON execution.provider_callback_inbox(workspace_id,received_at DESC,receipt_id DESC);

ALTER TABLE execution.provider_callback_inbox ENABLE ROW LEVEL SECURITY;
ALTER TABLE execution.provider_callback_inbox FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON execution.provider_callback_inbox
    USING(workspace_id=nullif(current_setting('mender.workspace_id',true),''))
    WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));

REVOKE ALL ON execution.provider_callback_inbox FROM PUBLIC;

COMMENT ON TABLE execution.provider_callback_inbox IS
    'Verified callback delivery metadata. Raw callback bodies, HMAC signatures, secrets and provider result/error payloads are intentionally excluded.';
