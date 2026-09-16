import assert from 'node:assert/strict';
import { readFileSync, statSync } from 'node:fs';
import { isAbsolute, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const projectRoot = fileURLToPath(new URL('../', import.meta.url));
const evidencePath = resolve(projectRoot, 'docs/engineering/s403-commerce-billing-evidence.json');

function exactKeys(value, keys, label) {
  assert.equal(typeof value, 'object', `${label} must be an object`);
  assert.ok(value !== null && !Array.isArray(value), `${label} must be an object`);
  assert.deepEqual(Object.keys(value).sort(), [...keys].sort(), `${label} keys drifted`);
}

function exactSet(values, expected, label) {
  assert.ok(Array.isArray(values), `${label} must be an array`);
  assert.equal(new Set(values).size, values.length, `${label} contains duplicates`);
  assert.deepEqual([...values].sort(), [...expected].sort(), `${label} drifted`);
}

function localFile(root, name) {
  const path = resolve(root, name);
  const within = relative(root, path);
  assert.ok(!within.startsWith('..') && !isAbsolute(within) && statSync(path).isFile(), `Invalid S4-03 evidence path ${name}`);
  return path;
}

export function loadS403Evidence(path = evidencePath) {
  return JSON.parse(readFileSync(path, 'utf8'));
}

export function validateS403Evidence(evidence) {
  exactKeys(evidence, ['schema_version', 'as_of', 'work_package', 'status', 'roadmap', 'ledger', 'approval', 'reconciliation', 'security', 'surfaces', 'verification', 'deferred', 'document'], 'S4-03 evidence');
  assert.equal(evidence.schema_version, 1);
  assert.equal(evidence.as_of, '2026-09-16');
  assert.equal(evidence.work_package, 'S4-03');
  assert.equal(evidence.status, 'complete');

  exactKeys(evidence.roadmap, ['canonical_id', 'canonical_s4_range', 'invented_stage_id'], 'S4-03 roadmap');
  assert.equal(evidence.roadmap.canonical_id, 'S4-03');
  assert.equal(evidence.roadmap.canonical_s4_range, 'S4-01..S4-18');
  assert.equal(evidence.roadmap.invented_stage_id, false);

  exactKeys(evidence.ledger, ['operational_billing_ledger', 'usage_settlements_remain_usage_facts', 'double_entry', 'entries_per_journal', 'zero_sum_per_currency', 'amount_unit', 'implicit_fx', 'immutable_journals', 'immutable_entries', 'immutable_usage_settlements', 'unique_business_key', 'one_charge_per_usage_settlement', 'automatic_charge_mirror', 'exact_replay_idempotent', 'corrections_append_only', 'provider_cost_event_implemented', 'statutory_accounting_gl_claim'], 'S4-03 ledger');
  for (const key of ['operational_billing_ledger', 'usage_settlements_remain_usage_facts', 'double_entry', 'zero_sum_per_currency', 'immutable_journals', 'immutable_entries', 'immutable_usage_settlements', 'unique_business_key', 'one_charge_per_usage_settlement', 'automatic_charge_mirror', 'exact_replay_idempotent', 'corrections_append_only']) assert.equal(evidence.ledger[key], true, `${key} must remain true`);
  assert.equal(evidence.ledger.entries_per_journal, 2);
  assert.equal(evidence.ledger.amount_unit, 'integer_micro_units');
  for (const key of ['implicit_fx', 'provider_cost_event_implemented', 'statutory_accounting_gl_claim']) assert.equal(evidence.ledger[key], false, `${key} must remain false`);

  exactKeys(evidence.approval, ['actions', 'requester_roles', 'reviewer_roles', 'maker_checker', 'self_approval_allowed', 'exact_amount_currency_binding', 'business_key_bound', 'business_basis_bound', 'direction_bound', 'reason_required', 'one_time_consumption', 'failed_business_mutation_consumes_approval', 'refund_cumulative_cap_enforced', 'refund_basis', 'adjustment_bases', 'adjustment_directions'], 'S4-03 approval');
  exactSet(evidence.approval.actions, ['commerce.refund', 'commerce.adjustment'], 'Commerce dangerous actions');
  exactSet(evidence.approval.requester_roles, ['operator'], 'Commerce requester roles');
  exactSet(evidence.approval.reviewer_roles, ['reviewer', 'operator'], 'Commerce reviewer roles');
  for (const key of ['maker_checker', 'exact_amount_currency_binding', 'business_key_bound', 'business_basis_bound', 'direction_bound', 'reason_required', 'one_time_consumption', 'refund_cumulative_cap_enforced']) assert.equal(evidence.approval[key], true, `${key} must remain true`);
  assert.equal(evidence.approval.self_approval_allowed, false);
  assert.equal(evidence.approval.failed_business_mutation_consumes_approval, false);
  assert.equal(evidence.approval.refund_basis, 'usage_settlement');
  exactSet(evidence.approval.adjustment_bases, ['run', 'incident', 'reconciliation'], 'Adjustment bases');
  exactSet(evidence.approval.adjustment_directions, ['debit', 'credit'], 'Adjustment directions');

  exactKeys(evidence.reconciliation, ['unknown_provider_state', 'unknown_outcome_auto_release', 'unknown_outcome_auto_charge', 'held_reservation_preserved', 'late_provider_terminal_evidence_required', 'late_result_artifact_required', 'late_settlement_appends_one_charge', 'billing_summary_rebuilt_from_entries', 'usage_vs_ledger_reconciliation'], 'S4-03 reconciliation');
  assert.equal(evidence.reconciliation.unknown_provider_state, 'reconciling');
  for (const key of ['held_reservation_preserved', 'late_provider_terminal_evidence_required', 'late_result_artifact_required', 'late_settlement_appends_one_charge', 'billing_summary_rebuilt_from_entries', 'usage_vs_ledger_reconciliation']) assert.equal(evidence.reconciliation[key], true, `${key} must remain true`);
  assert.equal(evidence.reconciliation.unknown_outcome_auto_release, false);
  assert.equal(evidence.reconciliation.unknown_outcome_auto_charge, false);

  exactKeys(evidence.security, ['billing_database_role', 'approval_database_role', 'roles_separate', 'billing_direct_ledger_table_access', 'billing_direct_usage_table_access', 'billing_direct_approval_table_access', 'billing_internal_charge_mirror_execute', 'platform_staff_separate_from_workspace_membership', 'browser_session_csrf', 'browser_actor_fields_authoritative', 'raw_credentials_or_secrets_exposed', 'unrestricted_impersonation'], 'S4-03 security');
  assert.equal(evidence.security.billing_database_role, 'billing-manager');
  assert.equal(evidence.security.approval_database_role, 'dangerous-operation-manager');
  for (const key of ['roles_separate', 'platform_staff_separate_from_workspace_membership', 'browser_session_csrf']) assert.equal(evidence.security[key], true, `${key} must remain true`);
  for (const key of ['billing_direct_ledger_table_access', 'billing_direct_usage_table_access', 'billing_direct_approval_table_access', 'billing_internal_charge_mirror_execute', 'browser_actor_fields_authoritative', 'raw_credentials_or_secrets_exposed', 'unrestricted_impersonation']) assert.equal(evidence.security[key], false, `${key} must remain false`);

  exactKeys(evidence.surfaces, ['billing_summary_api', 'billing_reconciliation_api', 'refund_api', 'adjustment_api', 'commerce_approval_request_api', 'billing_frontend', 'payment_provider_adapter'], 'S4-03 surfaces');
  for (const key of ['billing_summary_api', 'billing_reconciliation_api', 'refund_api', 'adjustment_api', 'commerce_approval_request_api']) assert.equal(evidence.surfaces[key], 'implemented');
  assert.equal(evidence.surfaces.billing_frontend, 'deferred');
  assert.equal(evidence.surfaces.payment_provider_adapter, 'deferred');

  exactKeys(evidence.verification, ['real_postgres_test', 'ledger_migration', 'hardening_migration', 'billing_role', 'billing_role_validator', 'billing_api', 'approval_api', 'settlement_boundary_test'], 'S4-03 verification');
  exactSet(evidence.deferred, ['provider-cost-event-accounting', 'payment-provider-integration', 'real-money-wallet-funding', 'automatic-fx-conversion', 'tax-calculation', 'statutory-accounting-general-ledger', 'billing-frontend', 'production-deploy'], 'S4-03 deferred scope');
  assert.equal(evidence.document, 'docs/engineering/2026-09-16-s403-commerce-billing-boundary.md');
}

export function checkS403CommerceBilling(root = projectRoot) {
  const evidence = loadS403Evidence(resolve(root, 'docs/engineering/s403-commerce-billing-evidence.json'));
  validateS403Evidence(evidence);
  const ledger = readFileSync(localFile(root, evidence.verification.ledger_migration), 'utf8');
  const hardening = readFileSync(localFile(root, evidence.verification.hardening_migration), 'utf8');
  const role = readFileSync(localFile(root, evidence.verification.billing_role), 'utf8');
  const roleValidator = readFileSync(localFile(root, evidence.verification.billing_role_validator), 'utf8');
  const billingAPI = readFileSync(localFile(root, evidence.verification.billing_api), 'utf8');
  const approvalAPI = readFileSync(localFile(root, evidence.verification.approval_api), 'utf8');
  const settlementTest = readFileSync(localFile(root, evidence.verification.settlement_boundary_test), 'utf8');
  const integration = readFileSync(localFile(root, evidence.verification.real_postgres_test), 'utf8');
  const bootstrap = readFileSync(localFile(root, 'backend/internal/bootstrap/persistence.go'), 'utf8');
  const operator = readFileSync(localFile(root, 'backend/internal/bootstrap/operator.go'), 'utf8');

  for (const phrase of ['billing_journals', 'billing_entries', 'one_usage_charge_journal', 'DEFERRABLE INITIALLY DEFERRED', 'append_usage_charge_journal', 'request_commerce_approval', 'consume_commerce_approval', 'post_billing_refund', 'post_billing_adjustment', 'billing_summary', 'billing_reconciliation']) assert.ok(ledger.includes(phrase), `Billing migration missing ${phrase}`);
  assert.ok(ledger.includes("RAISE EXCEPTION 'billing ledger is immutable'") && ledger.includes('prior_refunds+amount_value>charged'), 'Ledger immutability or cumulative refund cap drifted');
  assert.ok(hardening.includes('immutable_usage_settlements') && hardening.includes('append a billing correction') && hardening.includes('char_length(target_version) <= 200'), 'S4-03 hardening migration drifted');

  assert.ok(role.includes('GRANT EXECUTE ON FUNCTION commerce.billing_summary') && role.includes('GRANT EXECUTE ON FUNCTION commerce.post_billing_refund') && !role.includes('GRANT SELECT ON commerce.billing_'), 'Billing manager grants drifted to direct ledger access');
  for (const phrase of ["has_schema_privilege(current_user,'governance','USAGE')", "has_schema_privilege(current_user,'identity','USAGE')", "has_schema_privilege(current_user,'execution','USAGE')", 'append_usage_charge_journal']) assert.ok(roleValidator.includes(phrase), `Billing manager isolation validator missing ${phrase}`);

  assert.ok(billingAPI.includes('DisallowUnknownFields') && billingAPI.includes('X-Mender-CSRF') && billingAPI.includes('/summary') && billingAPI.includes('/reconciliation') && billingAPI.includes('/refunds') && billingAPI.includes('/adjustments'), 'Billing HTTP boundary incomplete');
  assert.ok(approvalAPI.includes('/commerce-requests') && approvalAPI.includes('amount_micro') && approvalAPI.includes('ttl_seconds'), 'Commerce approval HTTP boundary incomplete');
  assert.ok(settlementTest.includes('TestSettlementProcessDoesNotChargeUnknownReconcilingOutcome') && settlementTest.includes('scope.settleCalls != 0'), 'Unknown outcome settlement boundary missing');

  for (const phrase of ['Platform finance staff unexpectedly became tenant members', 'billing manager obtained direct table authority', 'unbalanced journal committed', 'failed exact refund binding consumed approval', 'over-refund rollback consumed approval', 'unknown Provider outcome auto-released quota', 'unknown Provider outcome produced billing charge', 'late settlement charge did not converge exactly once', 'T20/T21 billing ledger verified']) assert.ok(integration.includes(phrase), `T20/T21 PostgreSQL drill missing ${phrase}`);
  assert.ok(integration.includes('execution.provider_observations') && integration.includes('execution.artifacts') && integration.includes("'reconciling',2") && integration.includes('submission_outcome_unknown'), 'T21 late-result evidence path incomplete');
  for (const phrase of ['MENDER_ADMIN_BILLING_ENABLED', 'MENDER_BILLING_MANAGER_DATABASE_URL', 'BillingManagerRole']) assert.ok(bootstrap.includes(phrase), `Billing bootstrap boundary missing ${phrase}`);
  assert.ok(operator.includes('grant-billing-manager'), 'Billing operator grant command missing');

  const document = readFileSync(localFile(root, evidence.document), 'utf8');
  for (const phrase of ['S4-03 Commerce Billing / Adjustments / Refunds｜T20／T21：COMPLETE', 'S4-01..S4-18', '不能回写原 settlement', '累计值不能超过原 `usage_settlement.charged_micro`', '不因 TTL 自动 release', '没有实现独立 ProviderCostEvent accounting', '不自动发明“下一 S4-x”']) assert.ok(document.includes(phrase), `S4-03 boundary document missing ${phrase}`);
  return evidence;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const evidence = checkS403CommerceBilling();
  console.log(`PASS: Mender canonical ${evidence.work_package} Commerce Billing / Adjustments / Refunds is ${evidence.status.toUpperCase()}.`);
  console.log('LIMIT: ProviderCostEvent accounting, payment providers, wallet funding, FX, tax/statutory GL, billing frontend and production deploy remain deferred.');
}
