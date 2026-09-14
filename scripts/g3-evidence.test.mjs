import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { validateG3Manifest } from './check-g3-evidence.mjs';

const manifestPath = fileURLToPath(new URL('../docs/engineering/g3-evidence.json', import.meta.url));
const fresh = () => JSON.parse(readFileSync(manifestPath, 'utf8'));

test('G3 evidence manifest keeps the certified current protocol and exact three-source truth', () => {
  const manifest = fresh();
  assert.doesNotThrow(() => validateG3Manifest(manifest));
  assert.deepEqual(manifest.entry_matrix.sources.sort(), ['agent_http', 'http', 'mcp_streamable_http']);
  assert.deepEqual(manifest.limits.third_party_clients_certified, []);
});

test('G3 evidence rejects legacy/third-party/A2A support inflation', () => {
  for (const mutate of [
    (m) => { m.mcp.protocol_cases[1].status = 'certified'; },
    (m) => { m.limits.legacy_protocol_compatibility = true; },
    (m) => { m.limits.third_party_clients_certified = ['unverified-client']; },
    (m) => { m.limits.a2a_protocol = true; },
    (m) => { m.mcp.sdk_harness.version = 'v1.8.0'; },
  ]) {
    const manifest = fresh();
    mutate(manifest);
    assert.throws(() => validateG3Manifest(manifest));
  }
});

test('G3 evidence rejects missing entry sources and truth phases', () => {
  for (const mutate of [
    (m) => { m.entry_matrix.sources = ['http', 'agent_http']; },
    (m) => { m.entry_matrix.shared_truth = ['run', 'artifact']; },
    (m) => { m.mcp.distributions.pop(); },
    (m) => { m.requirements = ['S3-13']; },
  ]) {
    const manifest = fresh();
    mutate(manifest);
    assert.throws(() => validateG3Manifest(manifest));
  }
});
