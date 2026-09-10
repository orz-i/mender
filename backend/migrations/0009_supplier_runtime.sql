-- Owner: supply. Provider deployment descriptors only; no credential secret is stored here.
CREATE SCHEMA supply;

CREATE TABLE supply.deployments (
    revision text PRIMARY KEY CHECK (revision ~ '^[A-Za-z0-9_-]{1,128}$'),
    provider_id text NOT NULL CHECK (provider_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    transport_kind text NOT NULL CHECK (transport_kind='http'),
    endpoint_url text NOT NULL CHECK (char_length(endpoint_url) BETWEEN 1 AND 2048 AND endpoint_url !~ '[[:cntrl:]]'),
    http_method text NOT NULL CHECK (http_method='POST'),
    auth_mode text NOT NULL CHECK (auth_mode IN ('none','bearer','header')),
    auth_header_name text,
    idempotency_header text NOT NULL CHECK (idempotency_header ~ '^[A-Za-z0-9-]{1,128}$'),
    request_timeout_ms integer NOT NULL CHECK (request_timeout_ms BETWEEN 100 AND 300000),
    max_request_bytes integer NOT NULL CHECK (max_request_bytes BETWEEN 1 AND 1048576),
    max_response_bytes integer NOT NULL CHECK (max_response_bytes BETWEEN 1 AND 8388608),
    state text NOT NULL CHECK (state IN ('active','disabled')),
    created_at timestamptz NOT NULL,
    CHECK (
        (auth_mode='header' AND auth_header_name ~ '^[A-Za-z0-9-]{1,128}$')
        OR (auth_mode IN ('none','bearer') AND auth_header_name IS NULL)
    )
);
CREATE INDEX supply_deployment_provider ON supply.deployments(provider_id,revision);

REVOKE ALL ON SCHEMA supply FROM PUBLIC;
REVOKE ALL ON ALL TABLES IN SCHEMA supply FROM PUBLIC;
