import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { readFileSync, statSync } from 'node:fs';
import { isAbsolute, join, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import Ajv2020 from 'ajv/dist/2020.js';
import addFormats from 'ajv-formats';
import { parse } from 'yaml';
import { readJSON } from './lib/files.mjs';

const root = fileURLToPath(new URL('../contracts/', import.meta.url));
const ajv = new Ajv2020({ strict: false, allErrors: true });
addFormats(ajv);
const json = (name) => readJSON(join(root, name));
function validate(schema, value, name) {
  assert.ok(ajv.validateSchema(schema), `${name}: ${ajv.errorsText()}`);
  const validator = ajv.compile(schema);
  assert.ok(validator(value), `${name}: ${ajv.errorsText(validator.errors)}`);
}
function localFile(name) {
  const path = resolve(root, name);
  const within = relative(root, path);
  assert.ok(!within.startsWith('..') && !isAbsolute(within) && statSync(path).isFile(), `Invalid artifact ${name}`);
  return path;
}
for (const stem of ['plugin-manifest', 'event-envelope']) {
  validate(json(`${stem}.schema.json`), json(stem === 'event-envelope' ? 'event.example.json' : `${stem}.example.json`), stem);
}
validate(json('connection-config.schema.json'), { credential_ref: 'conn_example' }, 'connection-config');
const manifest = json('plugin-manifest.example.json');
const digest = createHash('sha256').update(readFileSync(localFile(manifest.artifact.file))).digest('hex');
assert.equal(digest, manifest.artifact.sha256, 'Artifact digest mismatch');
for (const name of [manifest.config_schema_file, manifest.ui.schema_file, ...manifest.tool_files]) localFile(name);
const tool = json('tool.example.json');
validate(tool.input_schema, { query: 'example', limit: 10 }, 'tool input');
validate(tool.output_schema, { items: [{ name: 'Example', domain: 'example.test' }] }, 'tool output');

const api = parse(readFileSync(join(root, 'public-api.openapi.yaml'), 'utf8'));
assert.equal(api.openapi, '3.1.0');
function walk(value) {
  if (value === null || typeof value !== 'object') return;
  if ('$ref' in value) {
    assert.ok(value.$ref.startsWith('#/'), `Non-local OpenAPI ref: ${value.$ref}`);
    let target = api;
    for (const part of value.$ref.slice(2).split('/')) {
      target = target[part.replaceAll('~1', '/').replaceAll('~0', '~')];
      assert.notEqual(target, undefined, `Unresolved reference: ${value.$ref}`);
    }
  }
  Object.values(value).forEach(walk);
}
walk(api);
const ids = new Set();
for (const [path, item] of Object.entries(api.paths)) {
  const expected = [...path.matchAll(/\{([^}]+)\}/g)].map((match) => match[1]).sort();
  for (const [method, operation] of Object.entries(item)) {
    if (!['get', 'post', 'put', 'patch', 'delete', 'head', 'options', 'trace'].includes(method)) continue;
    assert.ok(operation.operationId && !ids.has(operation.operationId), `Invalid operation ID on ${path}`);
    ids.add(operation.operationId);
    const parameters = [...(item.parameters ?? []), ...(operation.parameters ?? [])];
    const actual = parameters.filter((p) => p.in === 'path' && p.required).map((p) => p.name).sort();
    assert.deepEqual(actual, expected, `Path parameters on ${path}`);
    assert.ok(Object.keys(operation.responses ?? {}).length, `Missing responses on ${path}`);
  }
}
for (const [name, schema] of Object.entries(api.components.schemas)) assert.ok(ajv.validateSchema(schema), `${name}: ${ajv.errorsText()}`);
console.log(`PASS: JSON Schemas / examples, artifact SHA-256, local refs and OpenAPI structure (${Object.keys(api.paths).length} paths / ${ids.size} operations)`);
console.log('LIMIT: this validates design examples, not full OpenAPI conformance or implemented business endpoints.');

const runAPI = parse(readFileSync(join(root, 'run-query-cancel.openapi.yaml'), 'utf8'));
assert.equal(runAPI.openapi, '3.1.0');
assert.equal(runAPI.info.version, '0.3.0');
assert.equal(Object.keys(runAPI.paths).length, 2);
validate(runAPI.components.schemas.RunResponse, {
  data: { run_id: 'run_example', workspace_id: 'ws_example', execution_state: 'queued', version: '1', created_at: '2026-09-09T00:00:00Z', updated_at: '2026-09-09T00:00:00Z' },
  meta: { request_id: '0123456789abcdef0123456789abcdef' },
}, 'implemented Run response subset');
for (const item of Object.values(runAPI.paths)) {
  assert.equal(item.parameters.length, 2);
  for (const operation of [item.get, item.post].filter(Boolean)) {
    assert.ok(operation.operationId);
    for (const response of Object.values(operation.responses)) {
      assert.equal(typeof response.description, 'string');
      for (const key of Object.keys(response)) {
        assert.ok(['description', 'headers', 'content', 'links', '$ref'].includes(key) || key.startsWith('x-'), `Unexpected Run response key ${key}; quote comma-bearing YAML descriptions.`);
      }
    }
  }
}
const cancelOperation = runAPI.paths['/workspaces/{workspace_id}/runs/{run_id}/cancel'].post;
assert.match(cancelOperation.responses['409'].description, /UNSAFE_CANCELLATION/);
assert.match(cancelOperation.responses['503'].description, /CANCELLATION_COMMIT_UNCONFIRMED/);
validate(runAPI.components.schemas.RunResponse, {
  data: { run_id: 'run_example', workspace_id: 'ws_example', execution_state: 'canceled', version: '2', created_at: '2026-09-09T00:00:00Z', updated_at: '2026-09-10T00:00:00Z' },
  meta: { request_id: '0123456789abcdef0123456789abcdef' },
}, 'coordinated cancellation response');
console.log('PASS: authenticated Run query/cancel v0.3.0 schemas, cancellation errors and response examples; real database checks are pnpm test:integration.');

const readAPI = parse(readFileSync(join(root, 'run-read.openapi.yaml'), 'utf8'));
assert.equal(readAPI.openapi, '3.1.0');
assert.equal(Object.keys(readAPI.paths).length, 2);
const readIDs = new Set();
for (const [path, item] of Object.entries(readAPI.paths)) {
  const expected = [...path.matchAll(/\{([^}]+)\}/g)].map((match) => match[1]).sort();
  assert.deepEqual(item.parameters.filter((p) => p.in === 'path' && p.required).map((p) => p.name).sort(), expected);
  assert.ok(item.get.operationId && !readIDs.has(item.get.operationId)); readIDs.add(item.get.operationId);
  const ref = item.get.responses['200'].content['application/json'].schema.$ref;
  assert.ok(readAPI.components.schemas[ref.split('/').at(-1)]);
}
validate(readAPI.components.schemas.RunListResponse, { data: [], meta: { request_id: 'example', next_cursor: null } }, 'Run list empty response');
validate(readAPI.components.schemas.RunListResponse, { data: [{ run_id: 'run_a', workspace_id: 'ws_a', execution_state: 'queued', version: '1', created_at: '2026-09-09T00:00:00Z', updated_at: '2026-09-09T00:00:00Z' }], meta: { request_id: 'example', next_cursor: 'opaque.example' } }, 'Run list response');
validate(readAPI.components.schemas.EventListResponse, { data: [{ version: '2', event_type: 'run.state_changed', execution_state: 'canceled', occurred_at: '2026-09-09T00:00:00Z', subject_id: 'subject_a', reason: 'example only' }], meta: { request_id: 'example', next_cursor: null, through_version: '2' } }, 'Run event response');
console.log('PASS: authorized Run list / finite event timeline schemas, local response refs and examples; no database execution claimed.');
