import assert from 'node:assert/strict';
import { test } from 'node:test';
import { MenderApiError, createRunsClient } from './runs.ts';

const run = (state = 'queued') => ({
  run_id: 'run_a', workspace_id: 'ws_a', execution_state: state, version: '1',
  created_at: '2026-09-12T00:00:00Z', updated_at: '2026-09-12T00:00:01Z',
});

test('lists protected runs without placing the machine token in the URL', async () => {
  const client = createRunsClient('', async (url, init) => {
    assert.equal(url, '/api/v1/workspaces/ws_a/runs?limit=20&state=queued');
    assert.equal(new Headers(init.headers).get('Authorization'), 'Bearer mender_live_test.secret');
    assert.equal(init.credentials, 'omit');
    assert.equal(init.cache, 'no-store');
    assert.ok(!url.includes('mender_live'));
    return Response.json({ data: [run()], meta: { request_id: 'req_a', next_cursor: 'cursor_a' } });
  });
  const page = await client.listRuns({ workspaceId: 'ws_a', token: 'mender_live_test.secret', state: 'queued' });
  assert.equal(page.items[0].runId, 'run_a');
  assert.equal(page.nextCursor, 'cursor_a');
});

test('loads detail, events and artifacts from the existing protected surface', async () => {
  const urls = [];
  const client = createRunsClient('', async (url) => {
    urls.push(url);
    if (url.endsWith('/events?limit=100')) return Response.json({ data: [{ version: '2', event_type: 'run.state_changed', execution_state: 'running', occurred_at: '2026-09-12T00:00:02Z', subject_id: 'sa_a', reason: '' }], meta: { request_id: 'req_e', next_cursor: null, through_version: '2' } });
    if (url.endsWith('/artifacts')) return Response.json({ data: [{ artifact_id: 'artifact_a', kind: 'result', media_type: 'application/json', size_bytes: 12, created_at: '2026-09-12T00:00:03Z' }], meta: { request_id: 'req_art' } });
    return Response.json({ data: run('running'), meta: { request_id: 'req_run' } });
  });
  const access = { workspaceId: 'ws_a', token: 'mender_live_test.secret', runId: 'run_a' };
  const [detail, events, artifacts] = await Promise.all([client.getRun(access), client.listRunEvents(access), client.listRunArtifacts(access)]);
  assert.equal(detail.executionState, 'running');
  assert.equal(events.items[0].version, '2');
  assert.equal(artifacts.items[0].artifactId, 'artifact_a');
  assert.equal(urls.length, 3);
});

test('cancels with strict JSON and surfaces API errors without token leakage', async () => {
  const client = createRunsClient('', async (url, init) => {
    assert.equal(url, '/api/v1/workspaces/ws_a/runs/run_a/cancel');
    assert.equal(init.method, 'POST');
    assert.equal(init.body, JSON.stringify({ reason: 'operator stop' }));
    return Response.json({ data: run('cancel_requested'), meta: { request_id: 'req_c' } }, { status: 202 });
  });
  const canceled = await client.cancelRun({ workspaceId: 'ws_a', token: 'mender_live_test.secret', runId: 'run_a', reason: 'operator stop' });
  assert.equal(canceled.executionState, 'cancel_requested');

  const denied = createRunsClient('', async () => Response.json({ error: { code: 'FORBIDDEN', message: 'Operation is not permitted.' }, meta: { request_id: 'req_x' } }, { status: 403 }));
  await assert.rejects(denied.getRun({ workspaceId: 'ws_a', token: 'mender_live_test.secret', runId: 'run_a' }), (error) => {
    assert.ok(error instanceof MenderApiError);
    assert.equal(error.status, 403);
    assert.equal(error.code, 'FORBIDDEN');
    assert.ok(!error.message.includes('mender_live'));
    return true;
  });
});

test('rejects malformed identities, unknown states and malformed payloads', async () => {
  const client = createRunsClient('', async () => Response.json({ data: [run('invented')], meta: { request_id: 'req' } }));
  await assert.rejects(client.listRuns({ workspaceId: '../escape', token: 'mender_live_test.secret' }), /Workspace ID/);
  await assert.rejects(client.listRuns({ workspaceId: 'ws_a', token: 'contains whitespace' }), /凭据/);
  await assert.rejects(client.listRuns({ workspaceId: 'ws_a', token: 'mender_live_test.secret' }), /未知的 Run 状态/);
});

test('fails closed if a successful response crosses the requested workspace boundary', async () => {
  const client = createRunsClient('', async () => Response.json({ data: [{ ...run(), workspace_id: 'ws_other' }], meta: { request_id: 'req' } }));
  await assert.rejects(client.listRuns({ workspaceId: 'ws_a', token: 'mender_live_test.secret' }), /错误 Workspace/);
});
