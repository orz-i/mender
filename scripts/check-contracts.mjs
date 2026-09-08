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
