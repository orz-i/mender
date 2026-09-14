-- Owner: identity. Existing short-lived StartRun delegations predate exact
-- argument binding. They are backfilled with an impossible sentinel hash and
-- therefore fail the new Admission equality check instead of gaining broader
-- authority after migration.

ALTER TABLE identity.run_start_delegations
    ADD COLUMN arguments_hash text;

UPDATE identity.run_start_delegations
   SET arguments_hash=repeat('0',64)
 WHERE arguments_hash IS NULL;

ALTER TABLE identity.run_start_delegations
    ALTER COLUMN arguments_hash SET NOT NULL,
    ADD CONSTRAINT run_start_delegations_arguments_hash_check
        CHECK (arguments_hash ~ '^[a-f0-9]{64}$');
