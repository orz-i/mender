import assert from 'node:assert/strict';
import { test } from 'node:test';
import { MenderApiError, createConsoleRunsClient, createRunsClient } from './runs.ts';

const run = (state = 'queued') => ({
  run_id: 'run_a', workspace_id: 'ws_a', execution_state: state, version: '1',
  created_at: '2026-09-12T00:00:00Z', updated_at: '2026-09-12T00:00:01Z',
});

test('Artifact object status and capability stay behind delegated run:read while signed content drops Authorization', async () => {
  const calls = [];
  const signedUrl = '/api/v1/artifact-objects/content?cap=signed_capability';
  const client = createConsoleRunsClient('', async (url, init) => {
    calls.push({ url, init });
    if (url.endsWith('/object')) return Response.json({ data: { state: 'available', expires_at: '2026-09-12T00:05:00Z' }, meta: { request_id: 'req_status' } });
    if (url.endsWith('/object-capability')) return Response.json({ data: { url: signedUrl, expires_at: '2026-09-12T00:01:00Z' }, meta: { request_id: 'req_grant' } }, { status: 201 });
    if (url === signedUrl) return Response.json({ answer: 42 }, { headers: { 'Content-Type': 'application/json' } });
    throw new Error(`unexpected URL ${url}`);
  });
  const access = { workspaceId: 'ws_a', token: 'short_lived_delegation', runId: 'run_a', artifactId: 'artifact_a' };
  const status = await client.getRunArtifactObjectStatus(access);
  assert.equal(status.state, 'available');
  assert.equal(status.expiresAt, '2026-09-12T00:05:00Z');
  const grant = await client.issueRunArtifactObjectCapability(access);
  assert.equal(grant.url, signedUrl);
  assert.deepEqual(await client.readRunArtifactObject(grant), { answer: 42 });
  assert.equal(calls[0].url, '/api/console/v1/workspaces/ws_a/runs/run_a/artifacts/artifact_a/object');
  assert.equal(calls[1].url, '/api/console/v1/workspaces/ws_a/runs/run_a/artifacts/artifact_a/object-capability');
  for (const call of calls.slice(0, 2)) {
    assert.equal(new Headers(call.init.headers).get('Authorization'), 'Bearer short_lived_delegation');
    assert.equal(call.init.credentials, 'omit');
    assert.ok(!call.url.includes('short_lived_delegation'));
  }
  assert.equal(new Headers(calls[2].init.headers).get('Authorization'), null);
  assert.equal(calls[2].init.credentials, 'omit');
  assert.equal(calls[2].init.referrerPolicy, 'no-referrer');
});

test('Artifact object client fails closed on inferred, leaked, or unsafe server projections', async () => {
  const access = { workspaceId: 'ws_a', token: 'short_lived_delegation', runId: 'run_a', artifactId: 'artifact_a' };
  const leaked = createConsoleRunsClient('', async () => Response.json({ data: { state: 'available', expires_at: '2026-09-12T00:05:00Z', object_key: 'objects/internal' }, meta: { request_id: 'req' } }));
  await assert.rejects(leaked.getRunArtifactObjectStatus(access), /Artifact object 状态/);
  const inconsistent = createConsoleRunsClient('', async () => Response.json({ data: { state: 'not_materialized', expires_at: '2026-09-12T00:05:00Z' }, meta: { request_id: 'req' } }));
  await assert.rejects(inconsistent.getRunArtifactObjectStatus(access), /不一致/);
  const unsafeUrl = createConsoleRunsClient('', async () => Response.json({ data: { url: 'https://evil.example/object?cap=secret', expires_at: '2026-09-12T00:01:00Z' }, meta: { request_id: 'req' } }, { status: 201 }));
  await assert.rejects(unsafeUrl.issueRunArtifactObjectCapability(access), /capability URL/);
  const duplicated = createConsoleRunsClient('', async () => Response.json({ data: { url: '/api/v1/artifact-objects/content?cap=a&cap=b', expires_at: '2026-09-12T00:01:00Z' }, meta: { request_id: 'req' } }, { status: 201 }));
  await assert.rejects(duplicated.issueRunArtifactObjectCapability(access), /capability URL/);
});

test('reads exact Run quota cost with delegated Bearer and rejects accounting drift', async () => {
  assert.equal('getRunCost' in createRunsClient(), false, 'Machine Run client must not advertise the Console-only cost route');
  const client = createConsoleRunsClient('', async (url, init) => {
    assert.equal(url, '/api/console/v1/workspaces/ws_a/runs/run_a/cost');
    assert.equal(new Headers(init.headers).get('Authorization'), 'Bearer delegated_alpha');
    assert.equal(init.credentials, 'omit');
    return Response.json({ data: {
      run_id: 'run_a', budget_id: 'budget_a', period_id: 'period_a', currency: 'USD', quota_state: 'settled',
      reserved_micro: '200000', charged_micro: '125000', released_micro: '75000', outcome: 'succeeded',
      created_at: '2026-09-12T00:00:00Z', finalized_at: '2026-09-12T00:01:00Z', accounting_scope: 'quota_only',
    } });
  });
  const cost = await client.getRunCost({ workspaceId: 'ws_a', token: 'delegated_alpha', runId: 'run_a' });
  assert.equal(cost.chargedMicro, '125000');
  assert.equal(cost.releasedMicro, '75000');
  assert.equal(typeof cost.reservedMicro, 'string');

  const drifted = createConsoleRunsClient('', async () => Response.json({ data: {
    run_id: 'run_a', budget_id: 'budget_a', period_id: 'period_a', currency: 'USD', quota_state: 'settled',
    reserved_micro: '200', charged_micro: '125', released_micro: '76', outcome: 'succeeded',
    created_at: '2026-09-12T00:00:00Z', finalized_at: '2026-09-12T00:01:00Z', accounting_scope: 'quota_only',
  } }));
  await assert.rejects(drifted.getRunCost({ workspaceId: 'ws_a', token: 'delegated_alpha', runId: 'run_a' }), /不一致/);

  const leaked = createConsoleRunsClient('', async () => Response.json({ data: {
    run_id: 'run_a', budget_id: 'budget_a', period_id: 'period_a', currency: 'USD', quota_state: 'held',
    reserved_micro: '75', charged_micro: null, released_micro: '0', outcome: null,
    created_at: '2026-09-12T00:00:00Z', finalized_at: null, accounting_scope: 'quota_only', reservation_id: 'secret_internal',
  } }));
  await assert.rejects(leaked.getRunCost({ workspaceId: 'ws_a', token: 'delegated_alpha', runId: 'run_a' }), /无法识别/);
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

test('loads detail, paged events and artifact content from the existing protected surface', async () => {
  const urls = [];
  const client = createRunsClient('', async (url) => {
    urls.push(url);
    if (url.endsWith('/events?limit=1')) return Response.json({ data: [{ version: '2', event_type: 'run.state_changed', execution_state: 'running', occurred_at: '2026-09-12T00:00:02Z', subject_id: 'sa_a', reason: '' }], meta: { request_id: 'req_e1', next_cursor: 'cursor_e', through_version: '3' } });
    if (url.endsWith('/events?limit=1&cursor=cursor_e')) return Response.json({ data: [{ version: '3', event_type: 'run.state_changed', execution_state: 'succeeded', occurred_at: '2026-09-12T00:00:03Z', subject_id: 'worker', reason: '' }], meta: { request_id: 'req_e2', next_cursor: null, through_version: '3' } });
    if (url.endsWith('/artifacts/artifact_a')) return Response.json({ data: { artifact_id: 'artifact_a', kind: 'provider_result', media_type: 'application/json', size_bytes: 12, created_at: '2026-09-12T00:00:04Z', content: { answer: 42 } }, meta: { request_id: 'req_art_detail' } });
    if (url.endsWith('/artifacts')) return Response.json({ data: [{ artifact_id: 'artifact_a', kind: 'provider_result', media_type: 'application/json', size_bytes: 12, created_at: '2026-09-12T00:00:04Z' }], meta: { request_id: 'req_art' } });
    return Response.json({ data: run('running'), meta: { request_id: 'req_run' } });
  });
  const access = { workspaceId: 'ws_a', token: 'mender_live_test.secret', runId: 'run_a' };
  const [detail, events, artifacts, artifact] = await Promise.all([client.getRun(access), client.listRunEvents({ ...access, limit: 1 }), client.listRunArtifacts(access), client.getRunArtifact({ ...access, artifactId: 'artifact_a' })]);
  assert.equal(detail.executionState, 'running');
  assert.equal(events.items[0].version, '2');
  assert.equal(events.nextCursor, 'cursor_e');
  const page2 = await client.listRunEvents({ ...access, limit: 1, cursor: events.nextCursor, expectedThroughVersion: events.throughVersion });
  assert.equal(page2.items[0].executionState, 'succeeded');
  assert.equal(page2.throughVersion, '3');
  assert.equal(artifacts.items[0].artifactId, 'artifact_a');
  assert.deepEqual(artifact.content, { answer: 42 });
  assert.equal(urls.length, 5);
});

test('event pagination and artifact detail fail closed on watermark or contract drift', async () => {
  const access = { workspaceId: 'ws_a', token: 'mender_live_test.secret', runId: 'run_a' };
  const drift = createRunsClient('', async (url) => {
    if (url.includes('/events')) return Response.json({ data: [{ version: '4', event_type: 'run.state_changed', execution_state: 'running', occurred_at: '2026-09-12T00:00:02Z', subject_id: 'sa_a', reason: '' }], meta: { request_id: 'req', next_cursor: null, through_version: '3' } });
    return Response.json({ data: { artifact_id: 'artifact_a', kind: 'provider_result', media_type: 'text/plain', size_bytes: 2, created_at: '2026-09-12T00:00:04Z', content: {} }, meta: { request_id: 'req' } });
  });
  await assert.rejects(drift.listRunEvents({ ...access, expectedThroughVersion: '3' }), /顺序或 watermark/);
  await assert.rejects(drift.getRunArtifact({ ...access, artifactId: 'artifact_a' }), /Artifact/);
});

test('Console result reads use the delegated prefix and never browser cookies', async () => {
  const calls = [];
  const client = createConsoleRunsClient('', async (url, init) => {
    calls.push({ url, init });
    if (url.includes('/events?')) return Response.json({ data: [], meta: { request_id: 'req_events', next_cursor: null, through_version: '1' } });
    return Response.json({ data: { artifact_id: 'artifact_a', kind: 'provider_result', media_type: 'application/json', size_bytes: 2, created_at: '2026-09-12T00:00:04Z', content: {} }, meta: { request_id: 'req_artifact' } });
  });
  const access = { workspaceId: 'ws_a', token: 'short_lived_delegation', runId: 'run_a' };
  await client.listRunEvents(access);
  await client.getRunArtifact({ ...access, artifactId: 'artifact_a' });
  assert.equal(calls[0].url, '/api/console/v1/workspaces/ws_a/runs/run_a/events?limit=20');
  assert.equal(calls[1].url, '/api/console/v1/workspaces/ws_a/runs/run_a/artifacts/artifact_a');
  for (const call of calls) {
    assert.equal(call.init.credentials, 'omit');
    assert.equal(new Headers(call.init.headers).get('Authorization'), 'Bearer short_lived_delegation');
    assert.ok(!call.url.includes('short_lived_delegation'));
  }
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

test('fails closed if Run surfaces leak Remote Agent or provider-control internals', async () => {
  const access = { workspaceId: 'ws_a', token: 'mender_live_test.secret', runId: 'run_a' };
  const leakedRun = createRunsClient('', async () => Response.json({
    data: { ...run('running'), provider_request_id: 'agent/request-internal', deployment_revision: 'deploy_agent_internal' },
    meta: { request_id: 'req_run_leak' },
  }));
  await assert.rejects(leakedRun.getRun(access), /无法识别的 Run/);

  const leakedEvent = createRunsClient('', async () => Response.json({
    data: [{ version: '2', event_type: 'run.state_changed', execution_state: 'reconciling', occurred_at: '2026-09-12T00:00:02Z', subject_id: 'worker', reason: '', external_task_id: 'agent/task-internal' }],
    meta: { request_id: 'req_event_leak', next_cursor: null, through_version: '2' },
  }));
  await assert.rejects(leakedEvent.listRunEvents(access), /无法识别的 Run 事件/);

  const leakedArtifact = createRunsClient('', async () => Response.json({
    data: { artifact_id: 'artifact_a', kind: 'provider_result', media_type: 'application/json', size_bytes: 2, created_at: '2026-09-12T00:00:04Z', credential_version_ref: 'credv_internal' },
    meta: { request_id: 'req_artifact_leak' },
  }));
  await assert.rejects(leakedArtifact.listRunArtifacts(access), /无法识别的 Artifact/);
});

test('Agent supplemental input uses exact safe projection and never exposes provider delivery internals', async () => {
  const calls = [];
  const client = createConsoleRunsClient('', async (url, init) => {
    calls.push({ url, init });
    if (init?.method === 'POST') {
      assert.equal(init.body, JSON.stringify({ input_request_id: 'input.req.1', answer: { region: 'eu' } }));
      return Response.json({ data: {
        input_request_id: 'input.req.1', state: 'submitted', prompt: 'Choose region',
        input_schema: { type: 'object', properties: { region: { type: 'string' } } },
        requested_at: '2026-09-15T04:00:00Z', updated_at: '2026-09-15T04:01:00Z',
      }, meta: { request_id: 'req_input_submit' } });
    }
    return Response.json({ data: {
      input_request_id: 'input.req.1', state: 'pending', prompt: 'Choose region',
      input_schema: { type: 'object', properties: { region: { type: 'string' } } },
      requested_at: '2026-09-15T04:00:00Z', updated_at: '2026-09-15T04:00:00Z',
    }, meta: { request_id: 'req_input_get' } });
  });
  const access = { workspaceId: 'ws_a', token: 'short_lived_delegation', runId: 'run_a' };
  const pending = await client.getAgentInput(access);
  assert.equal(pending.state, 'pending');
  const submitted = await client.submitAgentInput({ ...access, inputRequestId: 'input.req.1', answer: { region: 'eu' } });
  assert.equal(submitted.state, 'submitted');
  assert.equal(calls[0].url, '/api/console/v1/workspaces/ws_a/runs/run_a/input');
  assert.equal(calls[1].url, '/api/console/v1/workspaces/ws_a/runs/run_a/input');
  for (const call of calls) {
    assert.equal(new Headers(call.init.headers).get('Authorization'), 'Bearer short_lived_delegation');
    assert.equal(call.init.credentials, 'omit');
  }

  for (const leakedField of ['provider_request_id', 'external_task_id', 'answer_sha256', 'submission_id', 'input_endpoint_url']) {
    const leaked = createConsoleRunsClient('', async () => Response.json({ data: {
      input_request_id: 'input.req.1', state: 'pending', prompt: 'Choose region', input_schema: { type: 'object' },
      requested_at: '2026-09-15T04:00:00Z', updated_at: '2026-09-15T04:00:00Z', [leakedField]: 'internal',
    }, meta: { request_id: 'req_leak' } }));
    await assert.rejects(leaked.getAgentInput(access), /Agent supplemental input/);
  }
});
