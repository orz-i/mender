-- Owners: commerce releases quota; execution preserves cancellation facts.
-- Only blocked, never-executed jobs can use this path. No worker or refund is introduced.
ALTER TABLE commerce.reservations DROP CONSTRAINT reservations_state_check;
ALTER TABLE commerce.reservations ADD COLUMN released_at timestamptz;
ALTER TABLE commerce.reservations ADD CONSTRAINT reservation_release_state CHECK (
    (state='held' AND released_at IS NULL) OR
    (state='released' AND released_at IS NOT NULL AND released_at>=created_at AND released_at<'10000-01-01 UTC')
);
ALTER TABLE execution.jobs DROP CONSTRAINT jobs_state_check;
ALTER TABLE execution.jobs ADD COLUMN stopped_at timestamptz;
ALTER TABLE execution.jobs ADD CONSTRAINT job_stop_state CHECK (
    (state='blocked' AND stopped_at IS NULL) OR
    (state='canceled' AND stopped_at IS NOT NULL AND stopped_at>=created_at AND stopped_at<'10000-01-01 UTC')
);
ALTER TABLE execution.outbox DROP CONSTRAINT outbox_event_type_check;
ALTER TABLE execution.outbox DROP CONSTRAINT outbox_delivery_state_check;
ALTER TABLE execution.outbox ADD CONSTRAINT outbox_cancel_events CHECK (event_type IN ('run.admitted','run.canceled'));
ALTER TABLE execution.outbox ADD CONSTRAINT outbox_cancel_delivery CHECK (
    delivery_state='pending' OR (event_type='run.admitted' AND delivery_state='suppressed')
);

CREATE TABLE execution.run_cancellations (
    workspace_id text NOT NULL,
    run_id text NOT NULL,
    reservation_id text NOT NULL,
    budget_id text NOT NULL,
    period_id text NOT NULL,
    currency text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    released_micro bigint NOT NULL CHECK (released_micro>=0),
    version bigint NOT NULL CHECK (version=2),
    subject_id text NOT NULL CHECK (subject_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    credential_id text NOT NULL CHECK (credential_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    reason text NOT NULL CHECK (char_length(reason)<=500),
    occurred_at timestamptz NOT NULL CHECK (occurred_at>='0001-01-01 UTC' AND occurred_at<'10000-01-01 UTC'),
    PRIMARY KEY(workspace_id,run_id),
    FOREIGN KEY(workspace_id,run_id) REFERENCES execution.run_admissions(workspace_id,run_id)
);
ALTER TABLE execution.run_cancellations ENABLE ROW LEVEL SECURITY;
ALTER TABLE execution.run_cancellations FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON execution.run_cancellations
 USING(workspace_id=nullif(current_setting('mender.workspace_id',true),''))
 WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));
REVOKE ALL ON execution.run_cancellations FROM PUBLIC;

-- Explicit same-database integration contract: released quota and execution's
-- cancellation receipt mutually prove the exact Run/reservation/amount/time.
-- Held rows have released_at=NULL and therefore do not need a cancellation.
-- These deferred FKs are intentional coupling of ADR-020, not cross-domain SQL
-- from an application repository; both owners still write only their own tables.
ALTER TABLE commerce.reservations ADD CONSTRAINT reservation_release_proof UNIQUE(workspace_id,run_id,id,amount_micro,released_at);
ALTER TABLE execution.run_cancellations ADD CONSTRAINT cancellation_release_proof UNIQUE(workspace_id,run_id,reservation_id,released_micro,occurred_at);
ALTER TABLE commerce.reservations ADD CONSTRAINT released_requires_cancellation
 FOREIGN KEY(workspace_id,run_id,id,amount_micro,released_at)
 REFERENCES execution.run_cancellations(workspace_id,run_id,reservation_id,released_micro,occurred_at)
 DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE execution.run_cancellations ADD CONSTRAINT canceled_requires_release
 FOREIGN KEY(workspace_id,run_id,reservation_id,released_micro,occurred_at)
 REFERENCES commerce.reservations(workspace_id,run_id,id,amount_micro,released_at)
 DEFERRABLE INITIALLY DEFERRED;

-- Execution's own cancellation bundle is checked at commit, not between writes.
-- Commerce keeps its own deferred held-reservation total; the application UoW
-- coordinates both owners. This function never reads/writes commerce tables.
CREATE FUNCTION execution.check_cancellation_bundle() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$
DECLARE w text; r text;
BEGIN
    w:=NEW.workspace_id;
    IF TG_TABLE_NAME='runs' THEN
        r:=NEW.id;
        IF NEW.state<>'canceled' THEN RETURN NULL; END IF;
        IF NOT EXISTS(SELECT 1 FROM execution.run_admissions WHERE workspace_id=w AND run_id=r) THEN RETURN NULL; END IF;
    ELSE
        r:=NEW.run_id;
        IF TG_TABLE_NAME='jobs' THEN
            IF NEW.state<>'canceled' THEN RETURN NULL; END IF;
        ELSIF TG_TABLE_NAME='outbox' THEN
            IF NEW.event_type<>'run.canceled' AND NEW.delivery_state<>'suppressed' THEN RETURN NULL; END IF;
        END IF;
    END IF;
    IF NOT EXISTS(
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
    ) THEN RAISE EXCEPTION 'incomplete cancellation bundle' USING ERRCODE='23514'; END IF;
    RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER cancellation_receipt_bundle AFTER INSERT ON execution.run_cancellations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION execution.check_cancellation_bundle();
CREATE CONSTRAINT TRIGGER cancellation_run_bundle AFTER UPDATE ON execution.runs DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION execution.check_cancellation_bundle();
CREATE CONSTRAINT TRIGGER cancellation_job_bundle AFTER UPDATE ON execution.jobs DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION execution.check_cancellation_bundle();
CREATE CONSTRAINT TRIGGER cancellation_outbox_bundle AFTER INSERT OR UPDATE ON execution.outbox DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION execution.check_cancellation_bundle();
REVOKE ALL ON FUNCTION execution.check_cancellation_bundle() FROM PUBLIC;
