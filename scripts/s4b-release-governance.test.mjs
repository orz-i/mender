import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { validateS4BEvidence } from './check-s4b-release-governance.mjs';

const evidencePath = fileURLToPath(new URL('../docs/engineering/s4b-release-governance-evidence.json', import.meta.url));
const fresh = () => JSON.parse(readFileSync(evidencePath, 'utf8'));

test('S4-B evidence preserves T23 route pinning and emergency-disable semantics', () => {
  const evidence = fresh();
  assert.doesNotThrow(() => validateS4BEvidence(evidence));
  assert.equal(evidence.routing.run_admission_deployment_revision_is_pin, true);
  assert.equal(evidence.release_model.historical_run_repinning_allowed, false);
  assert.equal(evidence.emergency_disable.distinct_from_rollback, true);
  assert.equal(evidence.emergency_disable.rollback_reenables_candidate, false);
});

test('S4-B evidence rejects route and authority inflation', () => {
  for (const mutate of [
    (e) => { e.release_model.published_tool_version_mutated = true; },
    (e) => { e.release_model.historical_run_repinning_allowed = true; },
    (e) => { e.routing.route_cas_revisioned = false; },
    (e) => { e.emergency_disable.distinct_from_rollback = false; },
    (e) => { e.emergency_disable.rollback_reenables_candidate = true; },
    (e) => { e.security.release_manager_direct_route_dml_allowed = true; },
    (e) => { e.security.developer_release_manage_allowed = true; },
  ]) {
    const evidence = fresh();
    mutate(evidence);
    assert.throws(() => validateS4BEvidence(evidence));
  }
});

test('S4-B evidence keeps percentage canary, UI, finance and production deferred', () => {
  for (const mutate of [
    (e) => { e.release_model.percentage_random_split = true; },
    (e) => { e.surfaces.admin_release_frontend = 'implemented'; },
    (e) => { e.deferred = ['production-deploy']; },
    (e) => { e.next_work_package = 'production'; },
    (e) => { e.status = 'production-certified'; },
  ]) {
    const evidence = fresh();
    mutate(evidence);
    assert.throws(() => validateS4BEvidence(evidence));
  }
});

