import assert from 'node:assert/strict';
import test from 'node:test';
import { createConsoleStartRunClient } from './console-start-run.ts';

const constraint = {
  toolsetVersionId: 'set_alpha_v1', toolId: 'tool_alpha', toolVersion: '1.0.0', toolVersionId: 'tool_alpha_v1', connectionId: 'conn_alpha',
  currency: 'USD', maxChargeMicro: '75', idempotencyKey: 'human-start-alpha-0001',
};

test('mints exact StartRun delegation with browser session + CSRF only', async () => {
  const client = createConsoleStartRunClient('', async (url, init) => {
    assert.equal(url, '/api/console/v1/workspaces/ws_alpha/run-start-delegations');
    assert.equal(init.credentials, 'same-origin');
    assert.equal(init.headers.Authorization, undefined);
    assert.equal(init.headers['X-Mender-CSRF'], 'csrf-alpha');
    const body = JSON.parse(init.body);
    assert.equal(body.idempotency_key, constraint.idempotencyKey);
    return Response.json({ data: {
      delegation_id: 'rsd_alpha', workspace_id: 'ws_alpha', token: 'x'.repeat(43), expires_at: new Date(Date.now() + 60_000).toISOString(),
      toolset_version_id: constraint.toolsetVersionId, tool_id: constraint.toolId, tool_version: constraint.toolVersion, tool_version_id: constraint.toolVersionId,
      connection_id: constraint.connectionId, currency: constraint.currency, max_charge_micro: constraint.maxChargeMicro, idempotency_key: constraint.idempotencyKey,
    } }, { status: 201 });
  });
  const issued = await client.issueDelegation('ws_alpha', constraint, 'csrf-alpha');
  assert.equal(issued.delegationId, 'rsd_alpha');
  assert.equal(issued.idempotencyKey, constraint.idempotencyKey);
});

test('starts through delegated Console Admission without token in URL or cookies', async () => {
  const delegation = { ...constraint, delegationId: 'rsd_alpha', workspaceId: 'ws_alpha', token: 'x'.repeat(43), expiresAt: new Date(Date.now() + 60_000).toISOString() };
  const client = createConsoleStartRunClient('', async (url, init) => {
    assert.equal(url, '/api/console/v1/workspaces/ws_alpha/runs');
    assert.equal(url.includes(delegation.token), false);
    assert.equal(init.credentials, 'omit');
    assert.equal(init.headers.Authorization, `Bearer ${delegation.token}`);
    assert.equal(init.headers['Idempotency-Key'], constraint.idempotencyKey);
    const body = JSON.parse(init.body);
    assert.deepEqual(body.arguments, { query: 'hello' });
    return Response.json({ data: { run_id: 'run_alpha', execution_state: 'queued', billing_state: 'reserved', status_url: '/ignored' }, meta: { request_id: 'request-alpha', trace_id: 'request-alpha' } }, { status: 202 });
  });
  assert.deepEqual(await client.startRun('ws_alpha', delegation, { query: 'hello' }), { runId: 'run_alpha', replayed: false, requestId: 'request-alpha' });
});

test('delegation response fails closed on constraint drift', async () => {
  const client = createConsoleStartRunClient('', async () => Response.json({ data: {
    delegation_id: 'rsd_alpha', workspace_id: 'ws_alpha', token: 'x'.repeat(43), expires_at: new Date().toISOString(),
    toolset_version_id: constraint.toolsetVersionId, tool_id: constraint.toolId, tool_version: constraint.toolVersion, tool_version_id: constraint.toolVersionId,
    connection_id: 'conn_other', currency: constraint.currency, max_charge_micro: constraint.maxChargeMicro, idempotency_key: constraint.idempotencyKey,
  } }, { status: 201 }));
  await assert.rejects(client.issueDelegation('ws_alpha', constraint, 'csrf-alpha'), /错误的启动委托范围/);
});
