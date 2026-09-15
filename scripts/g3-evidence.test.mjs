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
  assert.equal(manifest.oauth_refresh.status, 'covered-alpha');
  assert.equal(manifest.remote_agent.status, 'covered-alpha');
  assert.equal(manifest.remote_agent.interaction_scope, 'one-shot-supplemental-input');
  assert.equal(manifest.mcp_cancellation.status, 'covered-alpha');
  assert.equal(manifest.mcp_cancellation.transport_scope, 'stateless-streamable-http-json-response');
  assert.equal(manifest.provider_callback.status, 'covered-alpha');
  assert.equal(manifest.provider_callback.ingress_scope, 'reviewed-signed-inbox');
  assert.equal(manifest.artifact_object.status, 'covered-alpha');
  assert.equal(manifest.artifact_object.storage_scope, 'reviewed-filesystem-sidecar');
  for (const requirement of ['T07', 'T08', 'T17', 'T28', 'T30']) assert.ok(manifest.requirements.includes(requirement));
  assert.deepEqual(manifest.limits.third_party_clients_certified, []);
});

test('G3 evidence rejects legacy/third-party/A2A support inflation', () => {
  for (const mutate of [
    (m) => { m.mcp.protocol_cases[1].status = 'certified'; },
    (m) => { m.limits.legacy_protocol_compatibility = true; },
    (m) => { m.limits.third_party_clients_certified = ['unverified-client']; },
    (m) => { m.limits.a2a_protocol = true; },
    (m) => { m.mcp.sdk_harness.version = 'v1.8.0'; },
    (m) => { m.oauth_refresh.status = 'certified-production'; },
    (m) => { m.remote_agent.interaction_scope = 'multi-turn-agent'; },
    (m) => { m.mcp_cancellation.transport_scope = 'production-sse-proxy'; },
    (m) => { m.provider_callback.ingress_scope = 'user-configurable-webhook'; },
    (m) => { m.artifact_object.storage_scope = 'public-cloud-bucket'; },
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
    (m) => { m.oauth_refresh.behaviors = ['cas_single_winner']; },
    (m) => { m.oauth_refresh.evidence.pop(); },
    (m) => { m.remote_agent.behaviors = ['reviewed_submit_status_cancel']; },
    (m) => { m.remote_agent.evidence.pop(); },
    (m) => { m.mcp_cancellation.behaviors = ['request_context_cancellation']; },
    (m) => { m.mcp_cancellation.evidence.pop(); },
    (m) => { m.provider_callback.behaviors = ['duplicate_delivery_dedup']; },
    (m) => { m.provider_callback.evidence.pop(); },
    (m) => { m.artifact_object.behaviors = ['short_lived_capability']; },
    (m) => { m.artifact_object.evidence.pop(); },
  ]) {
    const manifest = fresh();
    mutate(manifest);
    assert.throws(() => validateG3Manifest(manifest));
  }
});
