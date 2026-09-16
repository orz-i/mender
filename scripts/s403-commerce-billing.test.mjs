import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { validateS403Evidence } from './check-s403-commerce-billing.mjs';

const evidencePath = fileURLToPath(new URL('../docs/engineering/s403-commerce-billing-evidence.json', import.meta.url));
const fresh = () => JSON.parse(readFileSync(evidencePath, 'utf8'));

test('S4-03 evidence rejects mutable or unbalanced ledger drift', () => {
  for (const mutate of [
    (e) => { e.ledger.double_entry = false; },
    (e) => { e.ledger.entries_per_journal = 3; },
    (e) => { e.ledger.zero_sum_per_currency = false; },
    (e) => { e.ledger.immutable_usage_settlements = false; },
    (e) => { e.ledger.implicit_fx = true; },
    (e) => { e.ledger.corrections_append_only = false; },
  ]) {
    const evidence = fresh();
    mutate(evidence);
    assert.throws(() => validateS403Evidence(evidence));
  }
});

test('S4-03 evidence rejects financial approval inflation', () => {
  for (const mutate of [
    (e) => { e.approval.self_approval_allowed = true; },
    (e) => { e.approval.exact_amount_currency_binding = false; },
    (e) => { e.approval.one_time_consumption = false; },
    (e) => { e.approval.failed_business_mutation_consumes_approval = true; },
    (e) => { e.approval.refund_cumulative_cap_enforced = false; },
    (e) => { e.security.billing_direct_approval_table_access = true; },
  ]) {
    const evidence = fresh();
    mutate(evidence);
    assert.throws(() => validateS403Evidence(evidence));
  }
});

test('S4-03 evidence rejects roadmap and deferred-scope inflation', () => {
  for (const mutate of [
    (e) => { e.roadmap.invented_stage_id = true; },
    (e) => { e.roadmap.canonical_s4_range = 'S4-01..S4-100'; },
    (e) => { e.reconciliation.unknown_outcome_auto_release = true; },
    (e) => { e.reconciliation.unknown_outcome_auto_charge = true; },
    (e) => { e.surfaces.payment_provider_adapter = 'implemented'; },
    (e) => { e.surfaces.billing_frontend = 'implemented'; },
    (e) => { e.ledger.provider_cost_event_implemented = true; },
    (e) => { e.status = 'production-certified'; },
  ]) {
    const evidence = fresh();
    mutate(evidence);
    assert.throws(() => validateS403Evidence(evidence));
  }
});
