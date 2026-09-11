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

const artifactAPI = parse(readFileSync(join(root, 'artifact-read.openapi.yaml'), 'utf8'));
assert.equal(artifactAPI.openapi, '3.1.0');
assert.equal(artifactAPI.info.version, '0.1.0');
assert.equal(Object.keys(artifactAPI.paths).length, 2);
for (const [path, item] of Object.entries(artifactAPI.paths)) {
  const expected = [...path.matchAll(/\{([^}]+)\}/g)].map((match) => match[1]).sort();
  assert.deepEqual(item.parameters.filter((p) => p.in === 'path' && p.required).map((p) => p.name).sort(), expected);
  assert.ok(item.get.operationId);
  assert.equal(item.get.parameters, undefined, 'Artifact endpoints intentionally accept no query parameters');
  assert.ok(item.get.responses['200'].content['application/json'].schema.$ref.startsWith('#/components/schemas/'));
}
validate(artifactAPI.components.schemas.ArtifactListResponse, {
  data: [{ artifact_id: 'art_run_a', kind: 'provider_result', media_type: 'application/json', size_bytes: 11, created_at: '2026-09-11T09:30:00Z' }],
  meta: { request_id: 'example' },
}, 'Artifact list response');
validate(artifactAPI.components.schemas.ArtifactDetailResponse, {
  data: { artifact_id: 'art_run_a', kind: 'provider_result', media_type: 'application/json', size_bytes: 11, created_at: '2026-09-11T09:30:00Z', content: { ok: true } },
  meta: { request_id: 'example' },
}, 'Artifact detail response');
console.log('PASS: authenticated inline Artifact metadata/content contract; no provider-control identifiers or object-storage claims.');

const mcpTools = json('mcp-meta-tools.json');
assert.equal(mcpTools.contract_version, '0.1.0');
assert.equal(mcpTools.sdk, 'github.com/modelcontextprotocol/go-sdk@v1.7.0');
assert.equal(mcpTools.protocol_version, '2026-07-28');
assert.equal(mcpTools.transport, 'streamable-http-stateless');
assert.equal(mcpTools.endpoint_template, '/mcp/v1/workspaces/{workspace_id}');
const mcpNames = mcpTools.tools.map((tool) => tool.name);
assert.deepEqual([...mcpNames].sort(), ['mender_artifact_get', 'mender_run_cancel', 'mender_run_get', 'mender_run_start']);
assert.equal(new Set(mcpNames).size, mcpNames.length);
const startMetaTool = mcpTools.tools.find((tool) => tool.name === 'mender_run_start');
assert.deepEqual(startMetaTool.required_input, ['idempotency_key', 'tool_id', 'tool_version', 'toolset_id', 'connection_id', 'arguments', 'currency', 'max_charge_micro']);
const forbiddenMCP = new Set(mcpTools.forbidden_output_fields);
for (const tool of mcpTools.tools) {
  assert.ok(Array.isArray(tool.required_input) && Array.isArray(tool.output));
  for (const field of tool.output) assert.ok(!forbiddenMCP.has(field), `${tool.name} exposes forbidden field ${field}`);
}
for (const name of ['fixed-toolset-direct-tools', 'upstream-mcp-client', 'oauth', 'resources', 'prompts', 'mrtr', 'agent-as-tool']) assert.ok(mcpTools.not_claimed.includes(name));
console.log('PASS: MCP 2026-07-28 stateless meta-tool contract, stable four-tool surface and forbidden-output policy.');

const fixedToolset = json('fixed-toolset-contract.json');
assert.equal(fixedToolset.contract_version, '0.1.0');
assert.equal(fixedToolset.implementation_status, 'implemented');
assert.equal(fixedToolset.sdk, 'github.com/modelcontextprotocol/go-sdk@v1.7.0');
assert.equal(fixedToolset.protocol_version, '2026-07-28');
assert.equal(fixedToolset.transport, 'streamable-http-stateless');
assert.equal(fixedToolset.endpoint_template, '/mcp/v1/workspaces/{workspace_id}/toolsets/{toolset_version_id}');
assert.deepEqual(fixedToolset.catalog_tool_version.side_effect_values, ['read_only', 'write']);
assert.deepEqual(fixedToolset.catalog_tool_version.idempotency_values, ['safe_read', 'idempotent', 'unsafe']);
assert.equal(fixedToolset.catalog_tool_version.input_schema_top_level_type, 'object');
assert.equal(fixedToolset.catalog_tool_version.default_mcp_publishable, false);
assert.ok(fixedToolset.catalog_tool_version.immutable_fields.includes('mcp_publishable'));
assert.equal(fixedToolset.toolset_binding.default_mcp_exposed, false);
assert.equal(fixedToolset.toolset_binding.mcp_name_pattern, '^[a-z][a-z0-9_]{0,63}$');
assert.deepEqual(fixedToolset.toolset_binding.unique_scope, ['workspace_id', 'toolset_version_id', 'mcp_name']);
for (const field of ['connection_id', 'mcp_name', 'mcp_exposed']) assert.ok(fixedToolset.toolset_binding.direct_mcp_requires.includes(field));
for (const field of ['tool_id', 'tool_version', 'toolset_id', 'connection_id', 'price_version_id', 'budget_id', 'deployment_revision']) assert.ok(fixedToolset.security.client_cannot_override.includes(field));
assert.equal(fixedToolset.call_control.field, '_mender');
assert.deepEqual(fixedToolset.call_control.required, ['idempotency_key', 'currency', 'max_charge_micro']);
assert.equal(fixedToolset.call_control.stripped_before_business_arguments, true);
assert.equal(fixedToolset.call_control.business_arguments_validated_against_published_input_schema, true);
assert.equal(fixedToolset.call_result.mode, 'async_run_artifact');
assert.equal(fixedToolset.call_result.submission_state, 'accepted');
for (const field of ['run_id', 'submission_state', 'replayed', 'currency', 'reserved_micro']) assert.ok(fixedToolset.call_result.fields.includes(field));
console.log('PASS: Fixed Toolset publication contract keeps immutable Tool schemas, stable MCP aliases and server-owned routing controls.');

const upstreamMCP = json('upstream-mcp-contract.json');
assert.equal(upstreamMCP.contract_version, '0.1.0');
assert.equal(upstreamMCP.implementation_status, 'contracts_only');
assert.equal(upstreamMCP.sdk, 'github.com/modelcontextprotocol/go-sdk@v1.7.0');
assert.equal(upstreamMCP.protocol_version, '2026-07-28');
assert.equal(upstreamMCP.transport, 'streamable-http-stateless');
assert.equal(upstreamMCP.scope, 'tools-only');
assert.equal(upstreamMCP.deployment.endpoint_is_reviewed_and_fixed, true);
assert.equal(upstreamMCP.deployment.client_arguments_cannot_override_endpoint, true);
assert.equal(upstreamMCP.deployment.stateless_required, true);
assert.equal(upstreamMCP.deployment.generic_idempotency_header, false);
assert.equal(upstreamMCP.deployment.machine_token_passthrough, false);
assert.deepEqual(upstreamMCP.deployment.supported_auth_modes, ['none', 'bearer', 'header']);
assert.equal(upstreamMCP.discovery_snapshot.append_only, true);
assert.equal(upstreamMCP.discovery_snapshot.untrusted, true);
assert.equal(upstreamMCP.discovery_snapshot.auto_publish_to_catalog, false);
assert.equal(upstreamMCP.discovery_snapshot.schema_drift_creates_new_snapshot, true);
for (const capability of ['network-client-runtime', 'upstream-tools-call', 'oauth', 'resources', 'prompts', 'tasks', 'mrtr', 'agent-as-tool']) assert.ok(upstreamMCP.not_claimed.includes(capability));
console.log('PASS: upstream MCP contracts pin stateless Tools discovery, reviewed endpoints and append-only untrusted snapshots without claiming network execution.');
