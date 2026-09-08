import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createHealthClient } from './health.ts';

test('reads a live probe without claiming business readiness', async () => {
  const client = createHealthClient('', async (url, options) => {
    assert.equal(url, '/healthz');
    assert.ok(options.signal instanceof AbortSignal);
    return Response.json({ service: 'mender-api', status: 'ok', stage: 'bootstrap' });
  });
  assert.deepEqual(await client.getHealth(), { service: 'mender-api', status: 'ok', stage: 'bootstrap' });
});

test('rejects a failed HTTP response', async () => {
  const client = createHealthClient('', async () => new Response('', { status: 503 }));
  await assert.rejects(client.getHealth(), /HTTP 503/);
});

test('rejects an unrelated service or a malformed response', async () => {
  for (const body of [null, { status: 'ok' }, { service: 'other', status: 'ok', stage: 'bootstrap' }]) {
    const client = createHealthClient('', async () => Response.json(body));
    await assert.rejects(client.getHealth(), /无法识别/);
  }
});

test('propagates caller cancellation to the transport', async () => {
  const controller = new AbortController();
  controller.abort();
  const client = createHealthClient('', async (_url, options) => {
    options.signal.throwIfAborted();
    throw new Error('an aborted request must never continue');
  });
  await assert.rejects(client.getHealth(controller.signal), { name: 'AbortError' });
});
