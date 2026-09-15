import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { validateAlphaOperations } from './check-alpha-operations.mjs';

const manifestPath = fileURLToPath(new URL('../docs/engineering/alpha-operations-evidence.json', import.meta.url));
const fresh = () => JSON.parse(readFileSync(manifestPath, 'utf8'));

test('Alpha operations evidence preserves bounded S3-16/S3-17 scope', () => {
  const manifest = fresh();
  assert.doesNotThrow(() => validateAlphaOperations(manifest));
  assert.equal(manifest.s3_16.production_streaming_proxy_certified, false);
  assert.equal(manifest.s3_16.standalone_sse_certified, false);
  assert.equal(manifest.s3_17.callback_alert_transport_certified, false);
});

test('Alpha operations evidence rejects production certification inflation', () => {
  for (const mutate of [
    (m) => { m.s3_16.production_streaming_proxy_certified = true; },
    (m) => { m.s3_16.standalone_sse_certified = true; },
    (m) => { m.s3_16.application_sse_certified = true; },
    (m) => { m.s3_17.callback_alert_transport_certified = true; },
    (m) => { m.s3_16.mcp_transport = 'standalone-sse'; },
  ]) {
    const manifest = fresh();
    mutate(manifest);
    assert.throws(() => validateAlphaOperations(manifest));
  }
});

test('Alpha operations evidence rejects timeout and retention drift', () => {
  for (const mutate of [
    (m) => { m.s3_16.request_timeout_min_ms = 0; },
    (m) => { m.s3_16.request_timeout_max_ms = 600000; },
    (m) => { m.s3_17.artifact_retention_default_hours = 0; },
    (m) => { m.s3_17.artifact_retention_max_hours = 8760; },
    (m) => { m.s3_17.artifact_expiry_order = 'metadata-first'; },
  ]) {
    const manifest = fresh();
    mutate(manifest);
    assert.throws(() => validateAlphaOperations(manifest));
  }
});
