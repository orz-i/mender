import assert from 'node:assert/strict';
import test from 'node:test';
import { createConsoleLaunchClient } from './console-launch.ts';

const option = {
  toolset_version_id: 'set_alpha_v1', tool_id: 'tool_search', tool_version: '1.0.0', tool_version_id: 'tool_search_v1',
  title: 'Search', description: 'Reviewed search', input_schema: { type: 'object', properties: { query: { type: 'string' } } },
  side_effect: 'read_only', idempotency: 'safe_read', connection_id: 'conn_alpha', provider_id: 'provider_alpha', currency: 'USD', reserve_micro: '60',
};

test('lists server-filtered launch options with browser session only', async () => {
  const client = createConsoleLaunchClient('', async (url, init) => {
    assert.equal(url, '/api/console/v1/workspaces/ws_alpha/launch-options');
    assert.equal(init.credentials, 'same-origin');
    assert.equal(init.headers.Authorization, undefined);
    return Response.json({ data: [option] });
  });
  const items = await client.list('ws_alpha');
  assert.equal(items[0].connectionId, 'conn_alpha');
  assert.equal(items[0].reserveMicro, '60');
  assert.equal(items[0].inputSchema.type, 'object');
});

test('launch discovery fails closed on credential, budget or malformed schema leakage', async () => {
  for (const mutation of [
    { credential_version_ref: 'secret-ref' },
    { budget_id: 'budget-alpha' },
    { input_schema: { type: 'string' } },
  ]) {
    const client = createConsoleLaunchClient('', async () => Response.json({ data: [{ ...option, ...mutation }] }));
    await assert.rejects(client.list('ws_alpha'));
  }
});
