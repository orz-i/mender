import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { validateAlphaClosure } from './check-alpha-closure.mjs';

const manifestPath = fileURLToPath(new URL('../docs/engineering/alpha-closure.json', import.meta.url));
const fresh = () => JSON.parse(readFileSync(manifestPath, 'utf8'));

test('Core Alpha closure preserves scoped GO and exact certified client matrix', () => {
  const manifest = fresh();
  assert.doesNotThrow(() => validateAlphaClosure(manifest));
  assert.equal(manifest.decision.core_alpha, 'complete');
  assert.equal(manifest.decision.g3_gate, 'scoped-go');
  assert.deepEqual(manifest.third_party_clients_certified, []);
});

test('Core Alpha closure rejects external/production certification inflation', () => {
  for (const mutate of [
    (m) => { m.decision.g3_gate = 'unbounded-go'; },
    (m) => { m.decision.external_client_interoperability = 'complete'; },
    (m) => { m.decision.production_operational_certification = 'complete'; },
    (m) => { m.third_party_clients_certified = ['claude-desktop']; },
    (m) => { m.certified_client_matrix.push({ id: 'cursor', kind: 'third-party', protocol_version: '2026-07-28', distributions: ['meta_tools'], status: 'certified' }); },
    (m) => { m.user_feedback_status = 'accepted'; },
  ]) {
    const manifest = fresh();
    mutate(manifest);
    assert.throws(() => validateAlphaClosure(manifest));
  }
});

test('Core Alpha closure rejects missing G3 tests or deferred boundaries', () => {
  for (const mutate of [
    (m) => { m.required_tests = ['T40']; },
    (m) => { m.entry_sources = ['http', 'agent_http']; },
    (m) => { m.deferred = ['payment-accounting']; },
    (m) => { m.evidence_packages.pop(); },
    (m) => { m.next_phase = 'production'; },
  ]) {
    const manifest = fresh();
    mutate(manifest);
    assert.throws(() => validateAlphaClosure(manifest));
  }
});
