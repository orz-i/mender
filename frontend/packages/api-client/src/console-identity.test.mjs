import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createConsoleIdentityClient, MenderApiError } from './index.ts';

test('Console identity uses same-origin cookies without Authorization headers', async () => {
  const requests = [];
  const client = createConsoleIdentityClient('', async (url, init) => {
    requests.push({ url, init });
    if (url.endsWith('/session')) return Response.json({ data: { user_id: 'user_alpha' } });
    return Response.json({ data: [{ workspace_id: 'ws_alpha', role: 'admin' }] });
  });
  assert.deepEqual(await client.getSession(), { userId: 'user_alpha' });
  assert.deepEqual(await client.listWorkspaces(), [{ workspaceId: 'ws_alpha', role: 'admin' }]);
  for (const request of requests) {
    assert.equal(request.init.credentials, 'same-origin');
    assert.equal(request.init.headers.Authorization, undefined);
  }
});

test('logout sends only the explicit CSRF header and keeps cookies browser-managed', async () => {
  const client = createConsoleIdentityClient('', async (url, init) => {
    assert.equal(url, '/api/console/v1/session');
    assert.equal(init.method, 'DELETE');
    assert.equal(init.credentials, 'same-origin');
    assert.equal(init.headers['X-Mender-CSRF'], 'csrf-value');
    return new Response(null, { status: 204 });
  });
  await client.logout('csrf-value');
});

test('unauthenticated Console response is surfaced without inventing a session', async () => {
  const client = createConsoleIdentityClient('', async () => Response.json({ error: { code: 'UNAUTHENTICATED', message: 'Login is required.' } }, { status: 401 }));
  await assert.rejects(client.getSession(), (error) => error instanceof MenderApiError && error.status === 401 && error.code === 'UNAUTHENTICATED');
});
