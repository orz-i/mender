import assert from 'node:assert/strict';
import test from 'node:test';

import { createConsoleRunDelegationClient, createConsoleRunsClient } from './index.ts';

test('human Run delegation is minted with browser session + CSRF but returned token is used explicitly', async () => {
  const calls = [];
  const token = 'A'.repeat(43);
  const fetcher = async (input, init = {}) => {
    calls.push({ url: String(input), init });
    if (String(input).endsWith('/run-delegations')) {
      return new Response(JSON.stringify({ data: {
        delegation_id: 'rd_alpha', workspace_id: 'ws_alpha', token,
        scopes: ['run:read', 'run:cancel', 'run:input'], expires_at: '2026-09-12T11:00:00Z',
      } }), { status: 201, headers: { 'Content-Type': 'application/json' } });
    }
    if (String(input).includes('/runs?')) {
      return new Response(JSON.stringify({ data: [], meta: { request_id: 'req_alpha', next_cursor: null } }), { status: 200, headers: { 'Content-Type': 'application/json' } });
    }
    throw new Error(`unexpected request ${input}`);
  };

  const delegations = createConsoleRunDelegationClient('', fetcher);
  const issued = await delegations.issue('ws_alpha', ['run:read', 'run:cancel', 'run:input'], 'csrf_alpha');
  assert.equal(issued.token, token);
  assert.equal(calls[0].init.credentials, 'same-origin');
  assert.equal(new Headers(calls[0].init.headers).get('Authorization'), null);
  assert.equal(new Headers(calls[0].init.headers).get('X-Mender-CSRF'), 'csrf_alpha');
  assert.deepEqual(JSON.parse(calls[0].init.body), { scopes: ['run:read', 'run:cancel', 'run:input'] });

  const runs = createConsoleRunsClient('', fetcher);
  await runs.listRuns({ workspaceId: 'ws_alpha', token, limit: 20 });
  assert.match(calls[1].url, /^\/api\/console\/v1\/workspaces\/ws_alpha\/runs\?/);
  assert.equal(calls[1].init.credentials, 'omit');
  assert.equal(new Headers(calls[1].init.headers).get('Authorization'), `Bearer ${token}`);
});

test('delegation revoke uses only CSRF + browser-managed cookie and rejects unsafe scope payloads', async () => {
  const calls = [];
  const fetcher = async (input, init = {}) => {
    calls.push({ url: String(input), init });
    return new Response(null, { status: 204 });
  };
  const client = createConsoleRunDelegationClient('', fetcher);
  await client.revoke('ws_alpha', 'rd_alpha', 'csrf_alpha');
  assert.equal(calls[0].init.method, 'DELETE');
  assert.equal(calls[0].init.credentials, 'same-origin');
  assert.equal(new Headers(calls[0].init.headers).get('Authorization'), null);
  assert.equal(new Headers(calls[0].init.headers).get('X-Mender-CSRF'), 'csrf_alpha');
  await assert.rejects(() => client.issue('ws_alpha', ['run:read', 'run:read'], 'csrf_alpha'), /scope/);
});

test('delegation responses fail closed on workspace or scope drift', async () => {
  const client = createConsoleRunDelegationClient('', async () => new Response(JSON.stringify({ data: {
    delegation_id: 'rd_alpha', workspace_id: 'ws_other', token: 'A'.repeat(43), scopes: ['run:read'], expires_at: '2026-09-12T11:00:00Z',
  } }), { status: 201, headers: { 'Content-Type': 'application/json' } }));
  await assert.rejects(() => client.issue('ws_alpha', ['run:read'], 'csrf_alpha'), /委托响应/);
});
