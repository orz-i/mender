-- Owners: commerce owns quota/reservations; execution owns admission, jobs and Outbox.
-- This is an internal admission substrate, not payment balances or runnable jobs.
CREATE SCHEMA commerce;
CREATE TABLE commerce.budget_periods (
    workspace_id text NOT NULL CHECK (workspace_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    budget_id text NOT NULL CHECK (budget_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    period_id text NOT NULL CHECK (period_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    currency text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    starts_at timestamptz NOT NULL,
    ends_at timestamptz NOT NULL CHECK (ends_at > starts_at),
    active boolean NOT NULL DEFAULT true,
    limit_micro bigint NOT NULL CHECK (limit_micro >= 0),
    consumed_micro bigint NOT NULL DEFAULT 0 CHECK (consumed_micro >= 0),
    reserved_micro bigint NOT NULL DEFAULT 0 CHECK (reserved_micro >= 0),
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    PRIMARY KEY (workspace_id,budget_id,period_id),
    UNIQUE (workspace_id,budget_id,period_id,currency),
    CHECK (consumed_micro <= limit_micro AND reserved_micro <= limit_micro-consumed_micro)
);
CREATE TABLE commerce.reservations (
    workspace_id text NOT NULL,
    id text NOT NULL CHECK (id ~ '^[A-Za-z0-9_-]{1,128}$'),
    run_id text NOT NULL CHECK (run_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    budget_id text NOT NULL,
    period_id text NOT NULL,
    currency text NOT NULL,
    amount_micro bigint NOT NULL CHECK (amount_micro >= 0),
    state text NOT NULL DEFAULT 'held' CHECK (state='held'),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (workspace_id,id),
    UNIQUE (workspace_id,run_id),
    FOREIGN KEY (workspace_id,budget_id,period_id,currency) REFERENCES commerce.budget_periods(workspace_id,budget_id,period_id,currency)
);
CREATE INDEX reservation_period ON commerce.reservations(workspace_id,budget_id,period_id);
-- Deferred equality validates the multi-write quota invariant at commit, not between writes.
CREATE FUNCTION commerce.check_reserved_total() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$
DECLARE w text; b text; p text; expected bigint; actual numeric;
BEGIN
    IF TG_OP='DELETE' THEN w:=OLD.workspace_id; b:=OLD.budget_id; p:=OLD.period_id;
    ELSE w:=NEW.workspace_id; b:=NEW.budget_id; p:=NEW.period_id; END IF;
    SELECT reserved_micro INTO expected FROM commerce.budget_periods WHERE workspace_id=w AND budget_id=b AND period_id=p;
    -- Do not silently bypass the invariant by changing the RLS context before commit.
    IF NOT FOUND THEN RAISE EXCEPTION 'reservation period is not visible' USING ERRCODE='23514'; END IF;
    SELECT COALESCE(sum(amount_micro),0) INTO actual FROM commerce.reservations WHERE workspace_id=w AND budget_id=b AND period_id=p AND state='held';
    IF expected<>actual THEN RAISE EXCEPTION 'reservation total mismatch' USING ERRCODE='23514'; END IF;
    RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER quota_total AFTER INSERT OR UPDATE ON commerce.budget_periods DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION commerce.check_reserved_total();
CREATE CONSTRAINT TRIGGER reservation_total AFTER INSERT OR UPDATE OR DELETE ON commerce.reservations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION commerce.check_reserved_total();

CREATE TABLE execution.run_admissions (
    workspace_id text NOT NULL,
    run_id text NOT NULL,
    subject_id text NOT NULL CHECK (subject_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    credential_id text NOT NULL CHECK (credential_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    idempotency_key text NOT NULL CHECK (idempotency_key ~ '^[!-~]{8,128}$'),
    request_hash text NOT NULL CHECK (request_hash ~ '^[a-f0-9]{64}$'),
    reservation_id text NOT NULL,
    tool_version_id text NOT NULL,
    toolset_version_id text NOT NULL,
    connection_id text NOT NULL,
    price_version_id text NOT NULL,
    deployment_revision text NOT NULL,
    budget_id text NOT NULL,
    period_id text NOT NULL,
    currency text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    reserved_micro bigint NOT NULL CHECK (reserved_micro>=0),
    canonical_arguments text NOT NULL CHECK (octet_length(canonical_arguments)<=65536 AND jsonb_typeof(canonical_arguments::jsonb)='object'),
    created_at timestamptz NOT NULL,
    PRIMARY KEY(workspace_id,run_id),
    UNIQUE(workspace_id,subject_id,idempotency_key),
    FOREIGN KEY(workspace_id,run_id) REFERENCES execution.runs(workspace_id,id)
);
CREATE TABLE execution.jobs (
    workspace_id text NOT NULL,
    run_id text NOT NULL,
    state text NOT NULL CHECK (state='blocked'),
    blocked_reason text NOT NULL CHECK (blocked_reason='executor_not_configured'),
    created_at timestamptz NOT NULL,
    PRIMARY KEY(workspace_id,run_id),
    FOREIGN KEY(workspace_id,run_id) REFERENCES execution.run_admissions(workspace_id,run_id)
);
CREATE TABLE execution.outbox (
    workspace_id text NOT NULL,
    event_id text NOT NULL,
    run_id text NOT NULL,
    event_type text NOT NULL CHECK (event_type='run.admitted'),
    schema_version integer NOT NULL CHECK (schema_version=1),
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload)='object'),
    occurred_at timestamptz NOT NULL,
    delivery_state text NOT NULL DEFAULT 'pending' CHECK (delivery_state='pending'),
    PRIMARY KEY(workspace_id,event_id),
    UNIQUE(workspace_id,run_id,event_type),
    FOREIGN KEY(workspace_id,run_id) REFERENCES execution.run_admissions(workspace_id,run_id)
);
ALTER TABLE commerce.budget_periods ENABLE ROW LEVEL SECURITY;
ALTER TABLE commerce.budget_periods FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON commerce.budget_periods USING(workspace_id=nullif(current_setting('mender.workspace_id',true),'')) WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));
ALTER TABLE commerce.reservations ENABLE ROW LEVEL SECURITY;
ALTER TABLE commerce.reservations FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON commerce.reservations USING(workspace_id=nullif(current_setting('mender.workspace_id',true),'')) WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));
ALTER TABLE execution.run_admissions ENABLE ROW LEVEL SECURITY;
ALTER TABLE execution.run_admissions FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON execution.run_admissions USING(workspace_id=nullif(current_setting('mender.workspace_id',true),'')) WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));
ALTER TABLE execution.jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE execution.jobs FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON execution.jobs USING(workspace_id=nullif(current_setting('mender.workspace_id',true),'')) WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));
ALTER TABLE execution.outbox ENABLE ROW LEVEL SECURITY;
ALTER TABLE execution.outbox FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON execution.outbox USING(workspace_id=nullif(current_setting('mender.workspace_id',true),'')) WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));
REVOKE ALL ON SCHEMA commerce FROM PUBLIC;
REVOKE ALL ON ALL TABLES IN SCHEMA commerce FROM PUBLIC;
REVOKE ALL ON FUNCTION commerce.check_reserved_total() FROM PUBLIC;
REVOKE ALL ON execution.run_admissions,execution.jobs,execution.outbox FROM PUBLIC;
