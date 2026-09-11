-- Owner: supply. Provider status/cancel endpoints are immutable deployment
-- descriptor facts. Provider handles never become URL fragments: control
-- adapters POST a bounded JSON body to these fixed endpoints.
ALTER TABLE supply.deployments
    ADD COLUMN status_endpoint_url text,
    ADD COLUMN status_http_method text,
    ADD COLUMN cancel_endpoint_url text,
    ADD COLUMN cancel_http_method text;

ALTER TABLE supply.deployments
    ADD CONSTRAINT supply_status_control_pair CHECK (
        (status_endpoint_url IS NULL AND status_http_method IS NULL)
        OR (
            status_endpoint_url IS NOT NULL
            AND char_length(status_endpoint_url) BETWEEN 1 AND 2048
            AND status_endpoint_url !~ '[[:cntrl:]]'
            AND status_http_method = 'POST'
        )
    ),
    ADD CONSTRAINT supply_cancel_control_pair CHECK (
        (cancel_endpoint_url IS NULL AND cancel_http_method IS NULL)
        OR (
            cancel_endpoint_url IS NOT NULL
            AND char_length(cancel_endpoint_url) BETWEEN 1 AND 2048
            AND cancel_endpoint_url !~ '[[:cntrl:]]'
            AND cancel_http_method = 'POST'
        )
    );

COMMENT ON COLUMN supply.deployments.status_endpoint_url IS
    'Fixed reviewed HTTP status endpoint. Provider request/task handles are sent in the request body, never interpolated into this URL.';
COMMENT ON COLUMN supply.deployments.cancel_endpoint_url IS
    'Fixed reviewed HTTP cancel endpoint. Provider request/task handles are sent in the request body, never interpolated into this URL.';
