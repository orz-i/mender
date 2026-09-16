import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { test } from 'node:test';
import { validateS406Evidence } from './check-s406-sandbox-payment.mjs';

const path = fileURLToPath(new URL('../docs/engineering/s406-sandbox-payment-evidence.json', import.meta.url));
const fresh = () => JSON.parse(readFileSync(path, 'utf8'));

test('S4-06 evidence rejects live payment scope inflation', () => {
  for (const mutate of [
    (e) => { e.scope.mode = 'live'; },
    (e) => { e.scope.live_payments_enabled = true; },
    (e) => { e.scope.real_money_funding_enabled = true; },
    (e) => { e.scope.browser_redirect_is_payment_truth = true; },
    (e) => { e.invented_stage_id = true; },
  ]) {
    const e = fresh(); mutate(e); assert.throws(() => validateS406Evidence(e));
  }
});

test('S4-06 evidence rejects callback and ledger authority inflation', () => {
  for (const mutate of [
    (e) => { e.callbacks.verification_before_persistence = false; },
    (e) => { e.callbacks.raw_body_persisted = true; },
    (e) => { e.callbacks.fact_drift_quarantined = false; },
    (e) => { e.reconciliation.refund_callback_mutates_original_journal = true; },
    (e) => { e.reconciliation.callback_creates_billing_journal = true; },
    (e) => { e.security.direct_business_table_access = true; },
  ]) {
    const e = fresh(); mutate(e); assert.throws(() => validateS406Evidence(e));
  }
});

test('S4-06 evidence keeps deferred production capabilities exact', () => {
  const e = fresh();
  e.deferred = ['production-deploy'];
  assert.throws(() => validateS406Evidence(e));
});
