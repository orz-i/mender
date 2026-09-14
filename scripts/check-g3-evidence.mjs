import assert from 'node:assert/strict';
import { readFileSync, statSync } from 'node:fs';
import { isAbsolute, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const projectRoot = fileURLToPath(new URL('../', import.meta.url));
const manifestPath = resolve(projectRoot, 'docs/engineering/g3-evidence.json');

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

function evidenceRef(value, label) {
  exactKeys(value, value.subtest === undefined ? ['file', 'symbol'] : ['file', 'symbol', 'subtest'], label);
  assert.match(value.file, /^(backend|frontend|scripts)\/[A-Za-z0-9._/-]+$/, `${label} has unsafe file path`);
  assert.match(value.symbol, /^[A-Za-z_][A-Za-z0-9_]*$/, `${label} has invalid symbol`);
  if (value.subtest !== undefined) assert.ok(typeof value.subtest === 'string' && value.subtest.length > 0 && value.subtest.length <= 200, `${label} has invalid subtest`);
}

export function validateG3Manifest(manifest) {
  exactKeys(manifest, ['schema_version', 'gate', 'as_of', 'requirements', 'mcp', 'entry_matrix', 'oauth_refresh', 'limits', 'document'], 'G3 manifest');
  assert.equal(manifest.schema_version, 1);
  assert.equal(manifest.gate, 'G3');
  assert.equal(manifest.as_of, '2026-09-14');
  exactSet(manifest.requirements, ['S3-13', 'S3-14', 'S3-15', 'S3-18', 'T07', 'T08', 'T13', 'T14', 'T15', 'T16', 'T40'], 'G3 requirements');

  exactKeys(manifest.mcp, ['certified_protocol_version', 'selected_legacy_protocol_version', 'sdk_harness', 'distributions', 'protocol_cases', 'evidence'], 'MCP evidence');
  assert.equal(manifest.mcp.certified_protocol_version, '2026-07-28');
  assert.equal(manifest.mcp.selected_legacy_protocol_version, '2025-11-25');
  exactKeys(manifest.mcp.sdk_harness, ['module', 'version', 'certification_scope'], 'MCP SDK harness');
  assert.equal(manifest.mcp.sdk_harness.module, 'github.com/modelcontextprotocol/go-sdk');
  assert.equal(manifest.mcp.sdk_harness.version, 'v1.7.0');
  assert.equal(manifest.mcp.sdk_harness.certification_scope, 'automated-harness-only');

  assert.equal(manifest.mcp.distributions.length, 2, 'G3 certifies exactly two MCP distributions');
  const distributions = Object.fromEntries(manifest.mcp.distributions.map((item) => {
    exactKeys(item, ['id', 'route', 'status'], `MCP distribution ${item.id ?? '?'}`);
    return [item.id, item];
  }));
  assert.equal(distributions.meta_tools?.route, '/mcp/v1/workspaces/{workspace_id}');
  assert.equal(distributions.fixed_toolset?.route, '/mcp/v1/workspaces/{workspace_id}/toolsets/{toolset_version_id}');
  assert.equal(distributions.meta_tools?.status, 'certified');
  assert.equal(distributions.fixed_toolset?.status, 'certified');
  assert.equal(Object.keys(distributions).length, 2, 'Unknown MCP distribution in G3 evidence');

  assert.equal(manifest.mcp.protocol_cases.length, 2, 'G3 records one current and one selected legacy protocol');
  const protocolCases = Object.fromEntries(manifest.mcp.protocol_cases.map((item) => {
    exactKeys(item, ['id', 'version', 'status'], `MCP protocol case ${item.id ?? '?'}`);
    return [item.id, item];
  }));
  assert.deepEqual(protocolCases.current, { id: 'current', version: '2026-07-28', status: 'certified' });
  assert.deepEqual(protocolCases.selected_legacy, { id: 'selected_legacy', version: '2025-11-25', status: 'rejected' });
  assert.equal(Object.keys(protocolCases).length, 2, 'Unknown MCP protocol case in G3 evidence');
  assert.ok(Array.isArray(manifest.mcp.evidence) && manifest.mcp.evidence.length >= 4, 'MCP evidence is incomplete');
  manifest.mcp.evidence.forEach((item, index) => evidenceRef(item, `MCP evidence[${index}]`));

  exactKeys(manifest.entry_matrix, ['workspace', 'subject', 'toolset', 'sources', 'shared_truth', 'evidence'], 'entry matrix');
  assert.equal(manifest.entry_matrix.workspace, 'ws_g3_matrix');
  assert.equal(manifest.entry_matrix.subject, 'sa_g3_matrix');
  assert.equal(manifest.entry_matrix.toolset, 'set_g3_matrix_v1');
  exactSet(manifest.entry_matrix.sources, ['http', 'mcp_streamable_http', 'agent_http'], 'G3 entry sources');
  exactSet(manifest.entry_matrix.shared_truth, ['admission', 'governance', 'run', 'artifact', 'settlement'], 'G3 shared truth');
  assert.ok(Array.isArray(manifest.entry_matrix.evidence) && manifest.entry_matrix.evidence.length === 2, 'Entry matrix evidence is incomplete');
  manifest.entry_matrix.evidence.forEach((item, index) => evidenceRef(item, `entry evidence[${index}]`));

  exactKeys(manifest.oauth_refresh, ['status', 'provider_scope', 'behaviors', 'evidence'], 'OAuth refresh evidence');
  assert.equal(manifest.oauth_refresh.status, 'covered-alpha');
  assert.equal(manifest.oauth_refresh.provider_scope, 'single-reviewed-provider');
  exactSet(manifest.oauth_refresh.behaviors, [
    'initial_refresh_secret_capture',
    'cas_single_winner',
    'refresh_token_rotation',
    'refresh_token_retention',
    'scope_shrink_fail_closed',
    'invalid_grant_fail_closed',
    'transient_retry',
    'revoke_closes_candidate',
    'secret_isolation',
    'least_privilege',
  ], 'OAuth refresh behaviors');
  assert.ok(Array.isArray(manifest.oauth_refresh.evidence) && manifest.oauth_refresh.evidence.length === 5, 'OAuth refresh evidence is incomplete');
  manifest.oauth_refresh.evidence.forEach((item, index) => evidenceRef(item, `OAuth refresh evidence[${index}]`));

  exactKeys(manifest.limits, ['third_party_clients_certified', 'legacy_protocol_compatibility', 'a2a_protocol', 'multi_turn_agent', 'payment_accounting', 'production_proxy_certified'], 'G3 limits');
  assert.deepEqual(manifest.limits.third_party_clients_certified, [], 'No third-party MCP client is certified by this evidence package');
  for (const key of ['legacy_protocol_compatibility', 'a2a_protocol', 'multi_turn_agent', 'payment_accounting', 'production_proxy_certified']) {
    assert.equal(manifest.limits[key], false, `G3 limit ${key} must remain false`);
  }
  assert.equal(manifest.document, 'docs/engineering/2026-09-14-g3-compatibility-entry-matrix.md');
}

function localFile(root, name) {
  const path = resolve(root, name);
  const within = relative(root, path);
  assert.ok(!within.startsWith('..') && !isAbsolute(within) && statSync(path).isFile(), `Invalid G3 evidence path ${name}`);
  return path;
}

export function validateG3EvidenceFiles(manifest, root = projectRoot) {
  const goMod = readFileSync(localFile(root, 'backend/go.mod'), 'utf8');
  assert.match(goMod, /github\.com\/modelcontextprotocol\/go-sdk v1\.7\.0(?:\s|$)/, 'G3 SDK harness version is not pinned in backend/go.mod');
  const handler = readFileSync(localFile(root, 'backend/internal/processes/mcpbridge/adapters/inbound/httpapi/handler.go'), 'utf8');
  assert.match(handler, /ProtocolVersion\s*=\s*"2026-07-28"/, 'Certified MCP protocol drifted from implementation');

  for (const ref of [...manifest.mcp.evidence, ...manifest.entry_matrix.evidence, ...manifest.oauth_refresh.evidence]) {
    const content = readFileSync(localFile(root, ref.file), 'utf8');
    assert.ok(content.includes(`func ${ref.symbol}(`), `Missing G3 evidence symbol ${ref.symbol} in ${ref.file}`);
    if (ref.subtest !== undefined) assert.ok(content.includes(ref.subtest), `Missing G3 evidence subtest ${ref.subtest} in ${ref.file}`);
  }
  const document = readFileSync(localFile(root, manifest.document), 'utf8');
  for (const required of ['2026-07-28', '2025-11-25', 'v1.7.0', 'automated harness', 'not certified', 'ws_g3_matrix', 'T07', 'T08', 'OAuth refresh', 'invalid_grant', 'scope']) {
    assert.ok(document.includes(required), `G3 evidence document is missing required boundary text: ${required}`);
  }
}

export function loadG3Manifest(path = manifestPath) {
  return JSON.parse(readFileSync(path, 'utf8'));
}

export function checkG3Evidence(root = projectRoot) {
  const manifest = loadG3Manifest(resolve(root, 'docs/engineering/g3-evidence.json'));
  validateG3Manifest(manifest);
  validateG3EvidenceFiles(manifest, root);
  return manifest;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const manifest = checkG3Evidence();
  console.log(`PASS: ${manifest.gate} evidence certifies MCP ${manifest.mcp.certified_protocol_version} on ${manifest.mcp.distributions.length} distributions, ${manifest.entry_matrix.sources.length} real entry sources, and automated OAuth refresh T07/T08 semantics.`);
  console.log('LIMIT: certification is the pinned automated Go SDK harness plus local real-PostgreSQL fixtures; no third-party MCP client, legacy protocol, A2A, multi-turn Agent, payment accounting or production proxy is certified.');
}
