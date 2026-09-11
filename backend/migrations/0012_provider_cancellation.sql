-- Owner: execution. Provider cancellation is intent-first and never blindly
-- retried after the cancellation call may have crossed the network boundary.

CREATE TABLE execution.provider_cancel_intents (
    workspace_id text NOT NULL,
    run_id text NOT NULL,
    attempt_no integer NOT NULL CHECK (attempt_no BETWEEN 1 AND 100),
    cancel_key text NOT NULL CHECK (cancel_key ~ '^[A-Za-z0-9._:-]{8,200}$'),
    provider_id text NOT NULL CHECK (provider_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    provider_request_id text NOT NULL CHECK (char_length(provider_request_id) BETWEEN 1 AND 512),
    external_task_id text CHECK (external_task_id IS NULL OR char_length(external_task_id) BETWEEN 1 AND 512),
    requested_by_subject text NOT NULL CHECK (char_length(requested_by_subject) BETWEEN 1 AND 512),
    requested_by_credential text NOT NULL CHECK (char_length(requested_by_credential) BETWEEN 1 AND 512),
    reason text NOT NULL DEFAULT '' CHECK (char_length(reason)<=500),
    state text NOT NULL CHECK (state IN ('requested','sending','unknown','fulfilled','superseded')),
    requested_at timestamptz NOT NULL,
    sending_at timestamptz,
    resolved_at timestamptz,
    outcome_observation_id text CHECK (outcome_observation_id IS NULL OR outcome_observation_id ~ '^[A-Za-z0-9._:-]{1,200}$'),
    unknown_reason text CHECK (unknown_reason IS NULL OR char_length(unknown_reason) BETWEEN 1 AND 500),
    PRIMARY KEY(workspace_id,run_id),
    UNIQUE(workspace_id,cancel_key),
    FOREIGN KEY(workspace_id,run_id) REFERENCES execution.runs(workspace_id,id),
    FOREIGN KEY(workspace_id,run_id,attempt_no) REFERENCES execution.run_attempts(workspace_id,run_id,attempt_no),
    CHECK (
        (state='requested' AND sending_at IS NULL AND resolved_at IS NULL AND outcome_observation_id IS NULL AND unknown_reason IS NULL)
        OR (state='sending' AND sending_at IS NOT NULL AND sending_at>=requested_at AND resolved_at IS NULL AND outcome_observation_id IS NULL AND unknown_reason IS NULL)
        OR (state='unknown' AND sending_at IS NOT NULL AND sending_at>=requested_at AND resolved_at IS NOT NULL AND resolved_at>=sending_at AND outcome_observation_id IS NULL AND unknown_reason IS NOT NULL)
        OR (state IN ('fulfilled','superseded') AND resolved_at IS NOT NULL AND resolved_at>=requested_at AND outcome_observation_id IS NOT NULL AND unknown_reason IS NULL)
    )
);
ALTER TABLE execution.provider_cancel_intents ENABLE ROW LEVEL SECURITY;
ALTER TABLE execution.provider_cancel_intents FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON execution.provider_cancel_intents
    USING(workspace_id=nullif(current_setting('mender.workspace_id',true),''))
    WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));
REVOKE ALL ON execution.provider_cancel_intents FROM PUBLIC;
CREATE INDEX provider_cancel_requested ON execution.provider_cancel_intents(workspace_id,requested_at,run_id) WHERE state='requested';

CREATE FUNCTION execution.check_provider_cancel_request_bundle() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$
DECLARE w text; r text; run_state text; run_updated timestamptz;
BEGIN
    IF TG_TABLE_NAME='runs' THEN
        IF NEW.state<>'cancel_requested' OR OLD.state NOT IN ('running','reconciling') THEN RETURN NULL; END IF;
        IF NOT EXISTS(SELECT 1 FROM execution.run_admissions a WHERE a.workspace_id=NEW.workspace_id AND a.run_id=NEW.id) THEN RETURN NULL; END IF;
        w:=NEW.workspace_id; r:=NEW.id;
    ELSE
        w:=NEW.workspace_id; r:=NEW.run_id;
    END IF;
    SELECT x.state,x.updated_at INTO run_state,run_updated FROM execution.runs x WHERE x.workspace_id=w AND x.id=r;
    IF NOT EXISTS(
        SELECT 1 FROM execution.provider_cancel_intents c
        JOIN execution.jobs j ON (j.workspace_id,j.run_id)=(c.workspace_id,c.run_id)
        JOIN execution.run_attempts a ON (a.workspace_id,a.run_id,a.attempt_no)=(c.workspace_id,c.run_id,c.attempt_no)
        WHERE c.workspace_id=w AND c.run_id=r AND c.state IN ('requested','sending','unknown')
          AND run_state='cancel_requested' AND run_updated=c.requested_at
          AND j.state IN ('provider_waiting','reconciling')
          AND a.state IN ('submitted','unknown') AND a.provider_id=c.provider_id
          AND a.provider_request_id=c.provider_request_id
          AND (c.external_task_id IS NULL OR a.external_task_id=c.external_task_id)
    ) THEN RAISE EXCEPTION 'incomplete provider cancel request bundle' USING ERRCODE='23514'; END IF;
    RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER provider_cancel_request_intent_bundle AFTER INSERT ON execution.provider_cancel_intents DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION execution.check_provider_cancel_request_bundle();
CREATE CONSTRAINT TRIGGER provider_cancel_request_run_bundle AFTER UPDATE ON execution.runs DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION execution.check_provider_cancel_request_bundle();
REVOKE ALL ON FUNCTION execution.check_provider_cancel_request_bundle() FROM PUBLIC;

CREATE FUNCTION execution.check_provider_cancel_terminal_bundle() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$
DECLARE expected_cancel_state text; terminal_state text; terminal_at timestamptz; observation_id text;
BEGIN
    IF TG_TABLE_NAME='provider_cancel_intents' THEN
        IF NEW.state NOT IN ('fulfilled','superseded') THEN RETURN NULL; END IF;
        IF NEW.state='fulfilled' THEN expected_cancel_state:='canceled'; END IF;
        SELECT p.state,p.observed_at,p.observation_id INTO terminal_state,terminal_at,observation_id
          FROM execution.provider_observations p
         WHERE p.workspace_id=NEW.workspace_id AND p.run_id=NEW.run_id AND p.state IN ('succeeded','failed','canceled')
         ORDER BY p.observed_at DESC LIMIT 1;
        IF terminal_state IS NULL OR terminal_at<>NEW.resolved_at OR observation_id<>NEW.outcome_observation_id
           OR (NEW.state='fulfilled' AND terminal_state<>expected_cancel_state)
           OR (NEW.state='superseded' AND terminal_state NOT IN ('succeeded','failed')) THEN
            RAISE EXCEPTION 'provider cancel terminal state lacks matching provider result' USING ERRCODE='23514';
        END IF;
        RETURN NULL;
    END IF;
    IF NEW.state NOT IN ('succeeded','failed','canceled') THEN RETURN NULL; END IF;
    IF EXISTS(SELECT 1 FROM execution.provider_cancel_intents c WHERE c.workspace_id=NEW.workspace_id AND c.run_id=NEW.run_id) THEN
        IF NOT EXISTS(
            SELECT 1 FROM execution.provider_cancel_intents c
             WHERE c.workspace_id=NEW.workspace_id AND c.run_id=NEW.run_id
               AND c.resolved_at=NEW.observed_at AND c.outcome_observation_id=NEW.observation_id
               AND ((NEW.state='canceled' AND c.state='fulfilled') OR (NEW.state IN ('succeeded','failed') AND c.state='superseded'))
        ) THEN RAISE EXCEPTION 'provider result did not resolve cancel intent' USING ERRCODE='23514'; END IF;
    END IF;
    RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER provider_cancel_terminal_intent_bundle AFTER UPDATE ON execution.provider_cancel_intents DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION execution.check_provider_cancel_terminal_bundle();
CREATE CONSTRAINT TRIGGER provider_cancel_terminal_observation_bundle AFTER INSERT ON execution.provider_observations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION execution.check_provider_cancel_terminal_bundle();
REVOKE ALL ON FUNCTION execution.check_provider_cancel_terminal_bundle() FROM PUBLIC;

-- 0005 originally treated every admitted Run=canceled as a pre-submit safe
-- cancellation with quota release and Job=canceled. After a supplier has
-- accepted work, cancellation is instead proven by a canceled provider
-- observation, fulfilled cancel intent, and Job=finished. Preserve the 0005
-- branch for run_cancellations/Job/outbox events; only the Run trigger may use
-- the provider-confirmed alternative.
CREATE OR REPLACE FUNCTION execution.check_cancellation_bundle() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$
DECLARE w text; r text; can_read_provider_cancel boolean:=false;
BEGIN
    w:=NEW.workspace_id;
    IF TG_TABLE_NAME='runs' THEN
        r:=NEW.id;
        IF NEW.state<>'canceled' THEN RETURN NULL; END IF;
        can_read_provider_cancel:=has_table_privilege(current_user,'execution.provider_cancel_intents','SELECT');
        IF can_read_provider_cancel AND EXISTS(SELECT 1 FROM execution.provider_cancel_intents c WHERE c.workspace_id=w AND c.run_id=r) THEN
            IF EXISTS(
                SELECT 1 FROM execution.provider_cancel_intents c
                JOIN execution.provider_observations p ON (p.workspace_id,p.run_id,p.observation_id)=(c.workspace_id,c.run_id,c.outcome_observation_id)
                JOIN execution.runs x ON (x.workspace_id,x.id)=(c.workspace_id,c.run_id)
                JOIN execution.jobs j ON (j.workspace_id,j.run_id)=(c.workspace_id,c.run_id)
                WHERE c.workspace_id=w AND c.run_id=r AND c.state='fulfilled'
                  AND p.state='canceled' AND p.observed_at=c.resolved_at
                  AND x.state='canceled' AND x.updated_at=p.observed_at
                  AND j.state='finished' AND j.stopped_at=p.observed_at
            ) THEN RETURN NULL; END IF;
        END IF;
        IF NOT EXISTS(SELECT 1 FROM execution.run_admissions WHERE workspace_id=w AND run_id=r) THEN RETURN NULL; END IF;
    ELSE
        r:=NEW.run_id;
        IF TG_TABLE_NAME='jobs' THEN
            IF NEW.state<>'canceled' THEN RETURN NULL; END IF;
        ELSIF TG_TABLE_NAME='outbox' THEN
            IF NEW.event_type<>'run.canceled' AND NEW.delivery_state<>'suppressed' THEN RETURN NULL; END IF;
        END IF;
    END IF;
    IF EXISTS(
        SELECT 1 FROM execution.run_cancellations c
        JOIN execution.run_admissions a ON (a.workspace_id,a.run_id)=(c.workspace_id,c.run_id)
        JOIN execution.runs x ON (x.workspace_id,x.id)=(c.workspace_id,c.run_id)
        JOIN execution.jobs j ON (j.workspace_id,j.run_id)=(c.workspace_id,c.run_id)
        JOIN execution.run_events e ON (e.workspace_id,e.run_id,e.version)=(c.workspace_id,c.run_id,c.version)
        JOIN execution.outbox admitted ON (admitted.workspace_id,admitted.run_id)=(c.workspace_id,c.run_id) AND admitted.event_type='run.admitted'
        JOIN execution.outbox canceled ON (canceled.workspace_id,canceled.run_id)=(c.workspace_id,c.run_id) AND canceled.event_type='run.canceled'
        WHERE c.workspace_id=w AND c.run_id=r
          AND (c.reservation_id,c.budget_id,c.period_id,c.currency,c.released_micro)=(a.reservation_id,a.budget_id,a.period_id,a.currency,a.reserved_micro)
          AND x.state='canceled' AND x.version=c.version AND x.updated_at=c.occurred_at
          AND j.state='canceled' AND j.stopped_at=c.occurred_at
          AND e.state='canceled' AND e.occurred_at=c.occurred_at
          AND (e.subject_id,e.credential_id,e.reason)=(c.subject_id,c.credential_id,c.reason)
          AND admitted.delivery_state='suppressed' AND canceled.occurred_at=c.occurred_at
    ) THEN RETURN NULL; END IF;
    RAISE EXCEPTION 'incomplete cancellation bundle' USING ERRCODE='23514';
END $$;

-- Status reconciliation remains active while cancellation is pending or the
-- cancellation network outcome is unknown. A remote success/failure may still
-- legitimately win before cancellation is confirmed.
