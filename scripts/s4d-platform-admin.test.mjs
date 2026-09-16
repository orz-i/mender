import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { validateS4DEvidence } from './check-s4d-platform-admin.mjs';

const evidencePath = fileURLToPath(new URL('../docs/engineering/s4d-platform-admin-evidence.json', import.meta.url));
const fresh = () => JSON.parse(readFileSync(evidencePath, 'utf8'));

test('S4-D evidence rejects Platform Staff authority inflation', () => {
  for (const mutate of [
    (e) => { e.platform_staff.creates_workspace_membership = true; },
    (e) => { e.platform_staff.operate_roles.push('reviewer'); },
    (e) => { e.security.direct_business_table_read = true; },
    (e) => { e.security.dangerous_jit_authority_widened = true; },
    (e) => { e.platform_admin.browser_actor_fields_authoritative = true; },
  ]) {
    const evidence = fresh();
    mutate(evidence);
    assert.throws(() => validateS4DEvidence(evidence));
  }
});

test('S4-D evidence rejects provider quarantine semantic drift', () => {
  for (const mutate of [
    (e) => { e.provider_quarantine.existing_run_control_allowed = false; },
    (e) => { e.provider_quarantine.deployment_state_mutated = true; },
    (e) => { e.provider_quarantine.historical_admission_rewritten = true; },
    (e) => { e.provider_quarantine.unknown_legacy_provider_default_allowed = false; },
    (e) => { e.provider_quarantine.function_only_runtime_gate = false; },
    (e) => { e.workspace_freeze.historical_facts_deleted = true; },
  ]) {
    const evidence = fresh();
    mutate(evidence);
    assert.throws(() => validateS4DEvidence(evidence));
  }
});

test('S4-D evidence keeps finance UI impersonation and production deferred', () => {
  for (const mutate of [
    (e) => { e.security.financial_mutation = true; },
    (e) => { e.security.unrestricted_impersonation = true; },
    (e) => { e.surfaces.platform_admin_frontend = 'implemented'; },
    (e) => { e.deferred = ['production-deploy']; },
    (e) => { e.next_work_package = 'production'; },
    (e) => { e.status = 'production-certified'; },
  ]) {
    const evidence = fresh();
    mutate(evidence);
    assert.throws(() => validateS4DEvidence(evidence));
  }
});
