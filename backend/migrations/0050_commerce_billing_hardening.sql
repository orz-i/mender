-- S4-03 evidence hardening. Commerce business keys are bound through the
-- dangerous-operation target version, so the stored binding must support the
-- same reviewed 200-character limit. Usage settlements are immutable business
-- evidence for charge journals: corrections append billing facts instead.

ALTER TABLE governance.dangerous_operation_approvals
  DROP CONSTRAINT dangerous_operation_approvals_target_version_check;
ALTER TABLE governance.dangerous_operation_approvals
  ADD CONSTRAINT dangerous_operation_approvals_target_version_check
  CHECK (char_length(target_version) <= 200);

CREATE FUNCTION commerce.guard_usage_settlement_immutable() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
    RAISE EXCEPTION 'usage settlement is immutable; append a billing correction' USING ERRCODE='42501';
END $$;
CREATE TRIGGER immutable_usage_settlements
  BEFORE UPDATE OR DELETE ON commerce.usage_settlements
  FOR EACH ROW EXECUTE FUNCTION commerce.guard_usage_settlement_immutable();

REVOKE ALL ON FUNCTION commerce.guard_usage_settlement_immutable() FROM PUBLIC;

