import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createConsoleConnectionsClient } from './index.ts';

const record = { connection_id: 'conn_alpha', provider_id: 'provider_alpha', state: 'active', revision: 2, created_at: '2026-09-12T00:00:00Z', expires_at: '2026-09-13T00:00:00Z' };

test('lists only same-origin Connection metadata without Authorization', async () => {
  const client = createConsoleConnectionsClient('', async (url, init) => {
    assert.equal(url, '/api/console/v1/workspaces/ws_alpha/connections');
    assert.equal(init.credentials, 'same-origin');
    assert.equal(init.headers.Authorization, undefined);
    return Response.json({ data: [record] });
  });
  assert.deepEqual(await client.list('ws_alpha'), [{ connectionId: 'conn_alpha', providerId: 'provider_alpha', state: 'active', revision: 2, createdAt: record.created_at, expiresAt: record.expires_at }]);
});

test('revoke requires CSRF and never serializes a credential secret', async () => {
  const client = createConsoleConnectionsClient('', async (url, init) => {
    assert.equal(url, '/api/console/v1/workspaces/ws_alpha/connections/conn_alpha');
    assert.equal(init.method, 'DELETE');
    assert.equal(init.headers['X-Mender-CSRF'], 'csrf-alpha');
    assert.equal(init.body, undefined);
    return Response.json({ data: { ...record, state: 'revoked', revision: 3 } });
  });
  assert.equal((await client.revoke('ws_alpha', 'conn_alpha', 'csrf-alpha')).state, 'revoked');
});

test('fails closed if a server response exposes a credential reference', async () => {
  const client = createConsoleConnectionsClient('', async () => Response.json({ data: [{ ...record, credential_version_ref: 'secret-version' }] }));
  await assert.rejects(client.list('ws_alpha'), /不允许暴露/);
});
