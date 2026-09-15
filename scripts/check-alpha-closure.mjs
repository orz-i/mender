import assert from 'node:assert/strict';
import { readFileSync, statSync } from 'node:fs';
import { isAbsolute, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { checkG3Evidence } from './check-g3-evidence.mjs';
import { checkAlphaOperations } from './check-alpha-operations.mjs';

const projectRoot = fileURLToPath(new URL('../', import.meta.url));
const manifestPath = resolve(projectRoot, 'docs/engineering/alpha-closure.json');

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
  assert.ok(!within.startsWith('..') && !isAbsolute(within) && statSync(path).isFile(), `Invalid Alpha closure path ${name}`);
  return path;
}

export function loadAlphaClosure(path = manifestPath) {
  return JSON.parse(readFileSync(path, 'utf8'));
}

export function validateAlphaClosure(manifest) {
  exactKeys(manifest, [
    'schema_version', 'as_of', 'release_stage', 'decision', 'certified_client_matrix', 'third_party_clients_certified',
    'user_feedback_status', 'entry_sources', 'required_tests', 'evidence_packages', 'deferred', 'next_phase', 'document',
  ], 'Alpha closure');
  assert.equal(manifest.schema_version, 1);
  assert.equal(manifest.as_of, '2026-09-15');
  assert.equal(manifest.release_stage, 'core-alpha');
  exactKeys(manifest.decision, ['core_alpha', 'g3_internal_integration', 'g3_gate', 'external_client_interoperability', 'production_operational_certification'], 'Alpha closure decision');
  assert.deepEqual(manifest.decision, {
    core_alpha: 'complete',
    g3_internal_integration: 'complete',
    g3_gate: 'scoped-go',
    external_client_interoperability: 'deferred',
    production_operational_certification: 'deferred',
  });

  assert.ok(Array.isArray(manifest.certified_client_matrix) && manifest.certified_client_matrix.length === 1, 'Alpha closure certifies exactly one automated client harness');
  const client = manifest.certified_client_matrix[0];
  exactKeys(client, ['id', 'kind', 'protocol_version', 'distributions', 'status'], 'Certified client harness');
  assert.equal(client.id, 'go-sdk-v1.7.0-automated-harness');
  assert.equal(client.kind, 'automated-harness');
  assert.equal(client.protocol_version, '2026-07-28');
  exactSet(client.distributions, ['meta_tools', 'fixed_toolset'], 'Certified client distributions');
  assert.equal(client.status, 'certified');
  assert.deepEqual(manifest.third_party_clients_certified, [], 'No third-party MCP client may be claimed by Core Alpha closure');
  assert.equal(manifest.user_feedback_status, 'not-collected-external-acceptance-deferred');
  exactSet(manifest.entry_sources, ['http', 'mcp_streamable_http', 'agent_http'], 'Alpha entry sources');
  exactSet(manifest.required_tests, ['T07', 'T08', 'T13', 'T14', 'T15', 'T16', 'T17', 'T28', 'T30', 'T40'], 'Alpha G3 tests');
  exactSet(manifest.evidence_packages, ['docs/engineering/g3-evidence.json', 'docs/engineering/alpha-operations-evidence.json'], 'Alpha evidence packages');
  exactSet(manifest.deferred, [
    'third-party-mcp-client-interoperability',
    'external-user-acceptance-feedback',
    'production-streaming-proxy',
    'standalone-mcp-sse',
    'application-run-event-sse',
    'external-callback-alert-transport',
    'cloud-object-storage-certification',
    'a2a-protocol',
    'multi-turn-agent',
    'payment-accounting',
  ], 'Alpha deferred scope');
  assert.equal(manifest.next_phase, 's4-governed-commercialization-prep');
  assert.equal(manifest.document, 'docs/engineering/2026-09-15-core-alpha-g3-decision.md');
}

export function checkAlphaClosure(root = projectRoot) {
  const closure = loadAlphaClosure(resolve(root, 'docs/engineering/alpha-closure.json'));
  validateAlphaClosure(closure);
  const g3 = checkG3Evidence(root);
  const operations = checkAlphaOperations(root);

  exactSet(g3.entry_matrix.sources, closure.entry_sources, 'Closure/G3 entry source binding');
  for (const required of closure.required_tests) assert.ok(g3.requirements.includes(required), `G3 evidence is missing ${required}`);
  assert.deepEqual(g3.limits.third_party_clients_certified, closure.third_party_clients_certified, 'Closure inflated third-party client certification');
  assert.equal(g3.mcp.sdk_harness.version, 'v1.7.0');
  assert.equal(g3.mcp.certified_protocol_version, closure.certified_client_matrix[0].protocol_version);
  assert.equal(g3.mcp.sdk_harness.certification_scope, 'automated-harness-only');
  assert.equal(operations.s3_16.production_streaming_proxy_certified, false);
  assert.equal(operations.s3_16.standalone_sse_certified, false);
  assert.equal(operations.s3_16.application_sse_certified, false);
  assert.equal(operations.s3_17.callback_alert_transport_certified, false);

  for (const path of closure.evidence_packages) localFile(root, path);
  const document = readFileSync(localFile(root, closure.document), 'utf8');
  for (const phrase of ['Core Alpha：COMPLETE', 'G3 内部集成 Gate：SCOPED GO', 'third_party_clients_certified = []', 'not-collected-external-acceptance-deferred', '不是 production-ready']) {
    assert.ok(document.includes(phrase), `Alpha closure document missing boundary text: ${phrase}`);
  }
  return closure;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const closure = checkAlphaClosure();
  console.log(`PASS: Mender ${closure.release_stage} is COMPLETE; G3 internal integration decision is ${closure.decision.g3_gate.toUpperCase()}.`);
  console.log('LIMIT: third-party client interoperability, external user acceptance, production proxy/SSE/alerts, cloud object storage, A2A, multi-turn Agent and payment accounting remain deferred.');
}
