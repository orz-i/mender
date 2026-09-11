-- Owner: commerce. This migration closes quota reservation accounting for
-- confirmed Provider terminal outcomes. It is not a payment/revenue ledger.

ALTER TABLE commerce.price_versions ADD COLUMN charge_micro bigint;
ALTER TABLE commerce.price_versions ADD COLUMN billing_policy text;
UPDATE commerce.price_versions SET charge_micro=reserve_micro,billing_policy='fixed_success_only';
ALTER TABLE commerce.price_versions ALTER COLUMN charge_micro SET NOT NULL;
ALTER TABLE commerce.price_versions ALTER COLUMN billing_policy SET NOT NULL;
ALTER TABLE commerce.price_versions ADD CONSTRAINT price_fixed_success_only CHECK (
    charge_micro>=0 AND charge_micro<=reserve_micro AND billing_policy='fixed_success_only'
);

ALTER TABLE commerce.reservations DROP CONSTRAINT reservation_release_state;
ALTER TABLE commerce.reservations ADD COLUMN charged_micro bigint;
ALTER TABLE commerce.reservations ADD COLUMN settled_at timestamptz;
ALTER TABLE commerce.reservations ADD CONSTRAINT reservation_usage_state CHECK (
    (state='held' AND released_at IS NULL AND settled_at IS NULL AND charged_micro IS NULL) OR
    (state='released' AND released_at IS NOT NULL AND settled_at IS NULL AND charged_micro IS NULL
        AND released_at>=created_at AND released_at<'10000-01-01 UTC') OR
    (state='settled' AND released_at IS NULL AND settled_at IS NOT NULL AND charged_micro IS NOT NULL
        AND charged_micro>=0 AND charged_micro<=amount_micro
        AND settled_at>=created_at AND settled_at<'10000-01-01 UTC')
);

CREATE TABLE commerce.usage_settlements (
    workspace_id text NOT NULL CHECK (workspace_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    run_id text NOT NULL CHECK (run_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    reservation_id text NOT NULL CHECK (reservation_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    price_version_id text NOT NULL CHECK (price_version_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    budget_id text NOT NULL CHECK (budget_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    period_id text NOT NULL CHECK (period_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    currency text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    reserved_micro bigint NOT NULL CHECK (reserved_micro>=0),
    charged_micro bigint NOT NULL CHECK (charged_micro>=0 AND charged_micro<=reserved_micro),
    outcome text NOT NULL CHECK (outcome IN ('succeeded','failed','canceled')),
    observed_at timestamptz NOT NULL CHECK (observed_at>='0001-01-01 UTC' AND observed_at<'10000-01-01 UTC'),
    settled_at timestamptz NOT NULL CHECK (settled_at>=observed_at AND settled_at<'10000-01-01 UTC'),
    PRIMARY KEY(workspace_id,run_id),
    UNIQUE(workspace_id,reservation_id),
    FOREIGN KEY(price_version_id) REFERENCES commerce.price_versions(id),
    FOREIGN KEY(workspace_id,budget_id,period_id,currency) REFERENCES commerce.budget_periods(workspace_id,budget_id,period_id,currency)
);

ALTER TABLE commerce.reservations ADD CONSTRAINT reservation_settlement_proof
    UNIQUE(workspace_id,run_id,id,amount_micro,charged_micro,settled_at);
ALTER TABLE commerce.usage_settlements ADD CONSTRAINT usage_settlement_proof
    UNIQUE(workspace_id,run_id,reservation_id,reserved_micro,charged_micro,settled_at);
ALTER TABLE commerce.reservations ADD CONSTRAINT settled_requires_usage_receipt
    FOREIGN KEY(workspace_id,run_id,id,amount_micro,charged_micro,settled_at)
    REFERENCES commerce.usage_settlements(workspace_id,run_id,reservation_id,reserved_micro,charged_micro,settled_at)
    DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE commerce.usage_settlements ADD CONSTRAINT usage_receipt_requires_settled_reservation
    FOREIGN KEY(workspace_id,run_id,reservation_id,reserved_micro,charged_micro,settled_at)
    REFERENCES commerce.reservations(workspace_id,run_id,id,amount_micro,charged_micro,settled_at)
    DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE commerce.usage_settlements ENABLE ROW LEVEL SECURITY;
ALTER TABLE commerce.usage_settlements FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON commerce.usage_settlements
    USING(workspace_id=nullif(current_setting('mender.workspace_id',true),''))
    WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));

CREATE INDEX usage_settlement_period ON commerce.usage_settlements(workspace_id,budget_id,period_id,settled_at,run_id);
REVOKE ALL ON commerce.usage_settlements FROM PUBLIC;
