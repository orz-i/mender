import assert from 'node:assert/strict';
import { readFileSync, statSync } from 'node:fs';
import { isAbsolute, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const projectRoot = fileURLToPath(new URL('../', import.meta.url));
const manifestPath = resolve(projectRoot, 'docs/engineering/alpha-operations-evidence.json');

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
  assert.ok(!within.startsWith('..') && !isAbsolute(within) && statSync(path).isFile(), `Invalid Alpha operations evidence path ${name}`);
  return path;
}

export function loadAlphaOperations(path = manifestPath) {
  return JSON.parse(readFileSync(path, 'utf8'));
}

export function validateAlphaOperations(manifest) {
  exactKeys(manifest, ['schema_version', 'gate', 'as_of', 'status', 's3_16', 's3_17', 'evidence', 'limits', 'document'], 'Alpha operations manifest');
  assert.equal(manifest.schema_version, 1);
  assert.equal(manifest.gate, 'G3');
  assert.equal(manifest.as_of, '2026-09-15');
  assert.equal(manifest.status, 'alpha-bounded');

  exactKeys(manifest.s3_16, [
    'status', 'mcp_transport', 'application_run_events', 'egress_policy', 'request_timeout_min_ms', 'request_timeout_max_ms',
    'http_default', 'loopback_default', 'production_streaming_proxy_certified', 'standalone_sse_certified', 'application_sse_certified',
  ], 'S3-16 evidence');
  assert.equal(manifest.s3_16.status, 'covered-alpha-production-proxy-deferred');
  assert.equal(manifest.s3_16.mcp_transport, 'stateless-streamable-http-json-response');
  assert.equal(manifest.s3_16.application_run_events, 'finite-json-pagination');
  assert.equal(manifest.s3_16.egress_policy, 'exact-host-allowlist');
  assert.equal(manifest.s3_16.request_timeout_min_ms, 100);
  assert.equal(manifest.s3_16.request_timeout_max_ms, 300000);
  for (const key of ['http_default', 'loopback_default', 'production_streaming_proxy_certified', 'standalone_sse_certified', 'application_sse_certified']) {
    assert.equal(manifest.s3_16[key], false, `S3-16 boundary ${key} must remain false`);
  }

  exactKeys(manifest.s3_17, [
    'status', 'artifact_retention_default_hours', 'artifact_retention_min_hours', 'artifact_retention_max_hours',
    'artifact_expiry_order', 'callback_failure_observability', 'callback_alert_transport_certified',
  ], 'S3-17 evidence');
  assert.equal(manifest.s3_17.status, 'covered-alpha-alert-transport-deferred');
  assert.equal(manifest.s3_17.artifact_retention_default_hours, 24);
  assert.equal(manifest.s3_17.artifact_retention_min_hours, 1);
  assert.equal(manifest.s3_17.artifact_retention_max_hours, 2160);
  assert.equal(manifest.s3_17.artifact_expiry_order, 'physical-delete-before-expired');
  assert.equal(manifest.s3_17.callback_failure_observability, 'durable-inbox-admin-safe-projection');
  assert.equal(manifest.s3_17.callback_alert_transport_certified, false);

  assert.ok(Array.isArray(manifest.evidence) && manifest.evidence.length === 4, 'Alpha operations evidence is incomplete');
  for (const item of manifest.evidence) {
    exactKeys(item, ['file', 'symbol'], `Alpha operations evidence ${item.symbol ?? '?'}`);
    assert.match(item.file, /^backend\/[A-Za-z0-9._/-]+$/);
    assert.match(item.symbol, /^[A-Za-z_][A-Za-z0-9_]*$/);
  }
  exactSet(manifest.limits, [
    'no-production-streaming-proxy-certification',
    'no-standalone-sse-certification',
    'no-application-sse-certification',
    'no-external-callback-alert-transport-certification',
    'no-cloud-object-storage-certification',
  ], 'Alpha operations limits');
  assert.equal(manifest.document, 'docs/engineering/2026-09-15-alpha-operations-boundary.md');
}

export function validateAlphaOperationFiles(manifest, root = projectRoot) {
  const mcp = readFileSync(localFile(root, 'backend/internal/processes/mcpbridge/adapters/inbound/httpapi/handler.go'), 'utf8');
  const fixed = readFileSync(localFile(root, 'backend/internal/processes/mcpbridge/adapters/inbound/httpapi/fixed.go'), 'utf8');
  for (const content of [mcp, fixed]) {
    assert.match(content, /Stateless:\s*true/);
    assert.match(content, /JSONResponse:\s*true/);
    assert.match(content, /PropagateRequestCancellation:\s*true/);
  }
  const query = readFileSync(localFile(root, 'backend/internal/contexts/execution/application/query.go'), 'utf8');
  assert.ok(query.includes('finite JSON pagination, not SSE or an Outbox consumer'), 'Run events drifted from finite JSON Alpha contract');
  const deployment = readFileSync(localFile(root, 'backend/internal/contexts/supply/domain/deployment.go'), 'utf8');
  assert.match(deployment, /RequestTimeout < 100\*time\.Millisecond/);
  assert.match(deployment, /RequestTimeout > 5\*time\.Minute/);
  const host = readFileSync(localFile(root, 'backend/internal/bootstrap/reviewed_worker_host.go'), 'utf8');
  assert.match(host, /ArtifactObjectRetention = 24 \* time\.Hour/);
  assert.match(host, /retention < time\.Hour \|\| retention > 90\*24\*time\.Hour/);
  const env = readFileSync(localFile(root, 'backend/.env.example'), 'utf8');
  assert.ok(env.includes('MENDER_REVIEWED_EGRESS_ALLOW_HTTP=false'));
  assert.ok(env.includes('MENDER_REVIEWED_EGRESS_ALLOW_LOOPBACK=false'));
  assert.ok(env.includes('MENDER_REVIEWED_ARTIFACT_OBJECTS_ENABLED=false'));

  for (const ref of manifest.evidence) {
    const content = readFileSync(localFile(root, ref.file), 'utf8');
    assert.ok(content.includes(`func ${ref.symbol}(`), `Missing Alpha operations evidence ${ref.symbol}`);
  }
  const doc = readFileSync(localFile(root, manifest.document), 'utf8');
  for (const phrase of ['finite JSON pagination', '100 ms–5 min', '1h–2160h', 'physical delete', 'durable Inbox', 'production streaming proxy', 'not certified']) {
    assert.ok(doc.includes(phrase), `Alpha operations document missing boundary text: ${phrase}`);
  }
}

export function checkAlphaOperations(root = projectRoot) {
  const manifest = loadAlphaOperations(resolve(root, 'docs/engineering/alpha-operations-evidence.json'));
  validateAlphaOperations(manifest);
  validateAlphaOperationFiles(manifest, root);
  return manifest;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const manifest = checkAlphaOperations();
  console.log(`PASS: ${manifest.gate} Alpha operations evidence closes S3-16/S3-17 for the bounded local runtime.`);
  console.log('LIMIT: production streaming proxy, standalone/application SSE, external callback alert transport and cloud object storage remain explicitly uncertified.');
}
