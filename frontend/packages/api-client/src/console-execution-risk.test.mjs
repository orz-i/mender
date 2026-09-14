import assert from 'node:assert/strict';
import test from 'node:test';
import { createConsoleExecutionRiskClient } from './console-execution-risk.ts';

const input = { toolsetVersionId: 'set_alpha', toolVersionId: 'tv_alpha', connectionId: 'conn_alpha', idempotencyKey: 'human-risk-alpha-0001', arguments: { query: 'hello' } };
const decision = (outcome = 'confirmation_required') => ({
  sequence: '9007199254740993', policy_revision_id: 'execution_policy_v2', policy_revision: '9007199254740995', toolset_version_id: input.toolsetVersionId,
  tool_version_id: input.toolVersionId, connection_id: input.connectionId, arguments_hash: 'b'.repeat(64), risk_level: 'critical', outcome,
  reason_codes: ['tool_write_unsafe', outcome === 'confirmation_required' ? 'human_confirmation_required' : 'within_unconfirmed_risk'], evaluated_at: '2026-09-13T12:00:00Z',
});

test('execution risk preview uses browser session + CSRF and preserves exact bigint strings', async () => {
  const client = createConsoleExecutionRiskClient('', async (url, init) => {
    assert.equal(url, '/api/console/v1/workspaces/ws_alpha/execution-risk/preview');
    assert.equal(init.credentials, 'same-origin'); assert.equal(new Headers(init.headers).get('Authorization'), null); assert.equal(new Headers(init.headers).get('X-Mender-CSRF'), 'csrf-alpha');
    assert.deepEqual(JSON.parse(init.body), { toolset_version_id: input.toolsetVersionId, tool_version_id: input.toolVersionId, connection_id: input.connectionId, idempotency_key: input.idempotencyKey, arguments: input.arguments });
    return Response.json({ data: decision() });
  });
  const value = await client.preview('ws_alpha', input, 'csrf-alpha');
  assert.equal(value.sequence, '9007199254740993'); assert.equal(value.policyRevision, '9007199254740995'); assert.equal(typeof value.sequence, 'string');
});

test('execution risk confirmation returns exact server-owned binding without raw arguments', async () => {
  const client = createConsoleExecutionRiskClient('', async (url) => {
    assert.equal(url, '/api/console/v1/workspaces/ws_alpha/execution-risk/confirm');
    return Response.json({ data: { decision: decision(), confirmation_id: 'exc_alpha', expires_at: '2026-09-13T12:05:00Z' } });
  });
  const value = await client.confirm('ws_alpha', input, 'csrf-alpha');
  assert.equal(value.confirmationId, 'exc_alpha'); assert.equal(value.decision.argumentsHash, 'b'.repeat(64));
});

test('execution risk projection fails closed on sensitive fields, precision drift and inconsistent confirmation', async () => {
  const leaked = decision(); leaked.arguments = { secret: true };
  await assert.rejects(createConsoleExecutionRiskClient('', async () => Response.json({ data: leaked })).preview('ws_alpha', input, 'csrf-alpha'), /敏感字段/);
  const numeric = decision(); numeric.sequence = Number('9007199254740993');
  await assert.rejects(createConsoleExecutionRiskClient('', async () => Response.json({ data: numeric })).preview('ws_alpha', input, 'csrf-alpha'), /execution risk 响应|exact integer/);
  await assert.rejects(createConsoleExecutionRiskClient('', async () => Response.json({ data: { decision: decision('allow'), confirmation_id: 'exc_bad', expires_at: '2026-09-13T12:05:00Z' } })).confirm('ws_alpha', input, 'csrf-alpha'), /不一致/);
});
