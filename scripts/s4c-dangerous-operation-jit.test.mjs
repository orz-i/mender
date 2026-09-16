import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { validateS4CEvidence } from './check-s4c-dangerous-operation-jit.mjs';

const evidencePath = fileURLToPath(new URL('../docs/engineering/s4c-dangerous-operation-jit-evidence.json', import.meta.url));
const fresh = () => JSON.parse(readFileSync(evidencePath, 'utf8'));

test('S4-C evidence preserves exact maker-checker and one-time consumption boundaries', () => {
  const evidence = fresh();
  assert.doesNotThrow(() => validateS4CEvidence(evidence));
  assert.equal(evidence.approval_contract.maker_checker, true);
  assert.equal(evidence.approval_contract.self_approval_allowed, false);
  assert.equal(evidence.approval_contract.one_time_consumption, true);
  assert.equal(evidence.approval_contract.release_revision_server_bound, true);
});

test('S4-C evidence rejects JIT authority inflation', () => {
  for (const mutate of [
    (e) => { e.jit_support.creates_workspace_membership = true; },
    (e) => { e.jit_support.mutation_scopes_allowed = true; },
    (e) => { e.jit_support.support_reader_direct_tenant_table_read_allowed = true; },
    (e) => { e.security.support_reader_can_read_identity_tables = true; },
    (e) => { e.security.unrestricted_impersonation = true; },
    (e) => { e.approval_contract.browser_approval_flags_authoritative = true; },
    (e) => { e.approval_contract.generic_submit_granted_to_restricted_manager = true; },
  ]) {
    const evidence = fresh();
    mutate(evidence);
    assert.throws(() => validateS4CEvidence(evidence));
  }
});

test('S4-C evidence keeps finance, generalized actions, UI and production deferred', () => {
  for (const mutate of [
    (e) => { e.approval_contract.financial_consumer_implemented = true; },
    (e) => { e.approval_contract.implemented_actions.push('payment.refund'); },
    (e) => { e.surfaces.support_jit_frontend = 'implemented'; },
    (e) => { e.deferred = ['production-deploy']; },
    (e) => { e.next_work_package = 'production'; },
    (e) => { e.status = 'production-certified'; },
  ]) {
    const evidence = fresh();
    mutate(evidence);
    assert.throws(() => validateS4CEvidence(evidence));
  }
});
