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
  exactKeys(manifest, ['schema_version', 'gate', 'as_of', 'requirements', 'mcp', 'entry_matrix', 'oauth_refresh', 'remote_agent', 'mcp_cancellation', 'provider_callback', 'artifact_object', 'limits', 'document'], 'G3 manifest');
  assert.equal(manifest.schema_version, 1);
  assert.equal(manifest.gate, 'G3');
  assert.equal(manifest.as_of, '2026-09-15');
  exactSet(manifest.requirements, ['S3-13', 'S3-14', 'S3-15', 'S3-18', 'T07', 'T08', 'T13', 'T14', 'T15', 'T16', 'T17', 'T28', 'T30', 'T40'], 'G3 requirements');

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

  exactKeys(manifest.remote_agent, ['status', 'interaction_scope', 'behaviors', 'evidence'], 'Remote Agent evidence');
  assert.equal(manifest.remote_agent.status, 'covered-alpha');
  assert.equal(manifest.remote_agent.interaction_scope, 'one-shot-supplemental-input');
  exactSet(manifest.remote_agent.behaviors, [
    'reviewed_submit_status_cancel',
    'input_required_poll',
    'signed_input_required_callback',
    'run_input_authorization',
    'server_schema_validation',
    'answer_digest_only',
    'idempotent_answer_replay',
    'unknown_no_resend',
    'waiting_input_resume',
    'artifact_convergence',
    'least_privilege',
  ], 'Remote Agent behaviors');
  assert.ok(Array.isArray(manifest.remote_agent.evidence) && manifest.remote_agent.evidence.length === 4, 'Remote Agent evidence is incomplete');
  manifest.remote_agent.evidence.forEach((item, index) => evidenceRef(item, `Remote Agent evidence[${index}]`));

  exactKeys(manifest.mcp_cancellation, ['status', 'transport_scope', 'behaviors', 'evidence'], 'MCP cancellation evidence');
  assert.equal(manifest.mcp_cancellation.status, 'covered-alpha');
  assert.equal(manifest.mcp_cancellation.transport_scope, 'stateless-streamable-http-json-response');
  exactSet(manifest.mcp_cancellation.behaviors, [
    'request_context_cancellation',
    'persistent_run_start_no_implicit_cancel',
    'reconnect_idempotent_recovery',
    'fixed_tool_async_receipt_replay',
  ], 'MCP cancellation behaviors');
  assert.ok(Array.isArray(manifest.mcp_cancellation.evidence) && manifest.mcp_cancellation.evidence.length === 4, 'MCP cancellation evidence is incomplete');
  manifest.mcp_cancellation.evidence.forEach((item, index) => evidenceRef(item, `MCP cancellation evidence[${index}]`));

  exactKeys(manifest.provider_callback, ['status', 'ingress_scope', 'behaviors', 'evidence'], 'Provider Callback evidence');
  assert.equal(manifest.provider_callback.status, 'covered-alpha');
  assert.equal(manifest.provider_callback.ingress_scope, 'reviewed-signed-inbox');
  exactSet(manifest.provider_callback.behaviors, [
    'raw_body_hmac_before_parse',
    'timestamp_and_key_id_validation',
    'duplicate_delivery_dedup',
    'event_id_conflict_quarantine',
    'attempt_binding_fail_closed',
    'out_of_order_quarantine',
    'terminal_replay_quarantine',
    'run_artifact_convergence',
    'workspace_rls_isolation',
    'least_privilege',
    'safe_observer_projection',
  ], 'Provider Callback behaviors');
  assert.ok(Array.isArray(manifest.provider_callback.evidence) && manifest.provider_callback.evidence.length === 4, 'Provider Callback evidence is incomplete');
  manifest.provider_callback.evidence.forEach((item, index) => evidenceRef(item, `Provider Callback evidence[${index}]`));

  exactKeys(manifest.artifact_object, ['status', 'storage_scope', 'behaviors', 'evidence'], 'Artifact Object evidence');
  assert.equal(manifest.artifact_object.status, 'covered-alpha');
  assert.equal(manifest.artifact_object.storage_scope, 'reviewed-filesystem-sidecar');
  exactSet(manifest.artifact_object.behaviors, [
    'threshold_materialization',
    'deterministic_store',
    'run_read_authorization',
    'short_lived_capability',
    'digest_size_revalidation',
    'no_object_key_projection',
    'cross_workspace_denial',
    'expiry_physical_delete',
    'expired_capability_gone',
    'least_privilege',
  ], 'Artifact Object behaviors');
  assert.ok(Array.isArray(manifest.artifact_object.evidence) && manifest.artifact_object.evidence.length === 4, 'Artifact Object evidence is incomplete');
  manifest.artifact_object.evidence.forEach((item, index) => evidenceRef(item, `Artifact Object evidence[${index}]`));

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

  for (const ref of [...manifest.mcp.evidence, ...manifest.entry_matrix.evidence, ...manifest.oauth_refresh.evidence, ...manifest.remote_agent.evidence, ...manifest.mcp_cancellation.evidence, ...manifest.provider_callback.evidence, ...manifest.artifact_object.evidence]) {
    const content = readFileSync(localFile(root, ref.file), 'utf8');
    assert.ok(content.includes(`func ${ref.symbol}(`), `Missing G3 evidence symbol ${ref.symbol} in ${ref.file}`);
    if (ref.subtest !== undefined) assert.ok(content.includes(ref.subtest), `Missing G3 evidence subtest ${ref.subtest} in ${ref.file}`);
  }
  const document = readFileSync(localFile(root, manifest.document), 'utf8');
  for (const required of ['2026-07-28', '2025-11-25', 'v1.7.0', 'automated harness', 'not certified', 'ws_g3_matrix', 'T07', 'T08', 'T17', 'T28', 'T30', 'T40', 'OAuth refresh', 'one-shot supplemental input', 'unknown no-resend', 'request cancellation', 'fresh MCP connection', 'raw-body HMAC', 'event-id conflict', 'out-of-order', 'short-lived capability', 'physical delete', 'invalid_grant', 'scope']) {
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
  console.log(`PASS: ${manifest.gate} evidence certifies MCP ${manifest.mcp.certified_protocol_version} on ${manifest.mcp.distributions.length} distributions, ${manifest.entry_matrix.sources.length} real entry sources, OAuth T07/T08, Remote Agent T17, Provider Callback T28, Artifact Object T30, and MCP cancellation T40 Alpha semantics.`);
  console.log('LIMIT: certification is the pinned automated Go SDK harness plus local real-PostgreSQL fixtures; no third-party MCP client, legacy protocol, A2A, multi-turn Agent, payment accounting or production proxy is certified.');
}
