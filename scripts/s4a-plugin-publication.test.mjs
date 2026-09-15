import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { validateS4AEvidence } from './check-s4a-plugin-publication.mjs';

const evidencePath = fileURLToPath(new URL('../docs/engineering/s4a-plugin-publication-evidence.json', import.meta.url));
const fresh = () => JSON.parse(readFileSync(evidencePath, 'utf8'));

test('S4-A evidence preserves declarative capability and maker-checker boundaries', () => {
  const evidence = fresh();
  assert.doesNotThrow(() => validateS4AEvidence(evidence));
  assert.deepEqual(evidence.manifest.allowed_capability_kinds.sort(), ['agent', 'api_tool', 'mcp_tool']);
  assert.equal(evidence.governance.self_approval_allowed, false);
  assert.equal(evidence.publication.capability_preflight_on_publish, true);
});

test('S4-A evidence rejects arbitrary code or authority inflation', () => {
  for (const mutate of [
    (e) => { e.manifest.arbitrary_scripts_allowed = true; },
    (e) => { e.manifest.allowed_capability_kinds.push('script'); },
    (e) => { e.governance.self_approval_allowed = true; },
    (e) => { e.governance.publisher_can_approve = true; },
    (e) => { e.governance.reviewer_can_publish = true; },
    (e) => { e.publication.capability_preflight_on_publish = false; },
    (e) => { e.security.force_rls = false; },
  ]) {
    const evidence = fresh();
    mutate(evidence);
    assert.throws(() => validateS4AEvidence(evidence));
  }
});

test('S4-A evidence does not inflate S4-B, UI, payment or deploy scope', () => {
  for (const mutate of [
    (e) => { e.surfaces.publisher_frontend_workbench = 'implemented'; },
    (e) => { e.deferred = ['production-deploy']; },
    (e) => { e.next_work_package = 'production'; },
    (e) => { e.status = 'production-certified'; },
  ]) {
    const evidence = fresh();
    mutate(evidence);
    assert.throws(() => validateS4AEvidence(evidence));
  }
});

