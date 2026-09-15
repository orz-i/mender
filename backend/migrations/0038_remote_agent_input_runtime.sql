-- Alpha intentionally supports at most one supplemental-input request for a
-- Run. This prevents the control feature from becoming an implicit chat log.
CREATE UNIQUE INDEX agent_input_single_request_per_run
    ON execution.agent_input_requests(workspace_id,run_id);
