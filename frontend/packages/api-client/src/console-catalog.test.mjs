import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createConsoleCatalogClient } from './index.ts';

const snapshot = () => ({ data: {
  tool_versions: [{ tool_version_id: 'tv_a', tool_id: 'tool_a', version: '1.0.0', provider_id: 'provider_a', price_version_id: 'price_a', deployment_revision: 'deploy_a', title: 'A', description: '', input_schema: { type: 'object' }, output_schema: { type: 'object' }, side_effect: 'read_only', idempotency: 'safe_read', mcp_publishable: false, revision: '9007199254740993', state: 'draft', created_at: '2026-09-12T00:00:00Z', updated_at: '2026-09-12T00:00:00Z', published_at: null, retired_at: null }],
  toolsets: [{ id: 'set_a', revision: '9', state: 'draft', created_at: '2026-09-12T00:00:00Z', updated_at: '2026-09-12T00:00:00Z', published_at: null, retired_at: null, bindings: [] }],
  connections: [{ connection_id: 'conn_a', provider_id: 'provider_a', state: 'active', revision: '7', created_at: '2026-09-12T00:00:00Z', expires_at: '2026-10-12T00:00:00Z' }],
  price_versions: [{ id: 'price_a', tool_version_id: 'tv_a', currency: 'USD', reserve_micro: '9007199254740993', starts_at: '2026-09-12T00:00:00Z', ends_at: '2026-10-12T00:00:00Z', active: true }],
  budget_periods: [{ budget_id: 'budget_a', period_id: 'period_a', currency: 'USD', starts_at: '2026-09-12T00:00:00Z', ends_at: '2026-10-12T00:00:00Z', active: true }],
  publication_approvals: [{ id: 'approval_a', target_kind: 'tool_version', target_id: 'tv_a', target_revision: '9007199254740993', requester_user_id: 'maker_a', state: 'approved', requested_at: '2026-09-12T00:00:00Z', expires_at: '2026-09-12T00:30:00Z', reviewer_user_id: 'reviewer_a', reviewed_at: '2026-09-12T00:01:00Z', decision_note: 'reviewed', consumed_at: null }],
} });

test('Console Catalog snapshot preserves fixed precision and rejects sensitive projections', async () => {
  const client = createConsoleCatalogClient('', async (url, init) => {
    assert.equal(url, '/api/console/v1/workspaces/ws_a/catalog');
    assert.equal(init.credentials, 'same-origin');
    assert.equal(new Headers(init.headers).get('Authorization'), null);
    return Response.json(snapshot());
  });
  const value = await client.snapshot('ws_a');
  assert.equal(value.priceVersions[0].reserveMicro, '9007199254740993');
  assert.equal(typeof value.priceVersions[0].reserveMicro, 'string');
  assert.equal(value.connections[0].revision, '7');
  assert.equal(value.toolVersions[0].revision, '9007199254740993');
  assert.equal(value.publicationApprovals[0].targetRevision, '9007199254740993');

  const leaked = snapshot(); leaked.data.connections[0].credential_version_ref = 'secret_ref';
  await assert.rejects(createConsoleCatalogClient('', async () => Response.json(leaked)).snapshot('ws_a'), /不允许暴露/);
  const budgetLeak = snapshot(); budgetLeak.data.budget_periods[0].limit_micro = '100';
  await assert.rejects(createConsoleCatalogClient('', async () => Response.json(budgetLeak)).snapshot('ws_a'), /Budget amount/);
});

test('Console Catalog mutations echo CSRF and preflight remains server authoritative', async () => {
  const calls = [];
  const client = createConsoleCatalogClient('', async (url, init) => {
    calls.push({ url, init });
    if (url.endsWith('/preflight')) return Response.json({ data: { ready: false, issues: [{ code: 'connection_unavailable', target_id: 'conn_a' }] } });
    if (url.endsWith('/review-requests')) return Response.json({ data: snapshot().data.publication_approvals[0] }, { status: 201 });
    return Response.json({ data: { id: 'set_a', revision: '1', state: 'draft', created_at: '2026-09-12T00:00:00Z', updated_at: '2026-09-12T00:00:00Z', published_at: null, retired_at: null, bindings: [] } }, { status: 201 });
  });
  await client.createToolset('ws_a', 'set_a', 'csrf_a');
  const result = await client.toolsetPreflight('ws_a', 'set_a', 'csrf_a');
  assert.equal(result.ready, false);
  assert.deepEqual(result.issues, [{ code: 'connection_unavailable', targetId: 'conn_a' }]);
  assert.equal(new Headers(calls[0].init.headers).get('X-Mender-CSRF'), 'csrf_a');
  assert.equal(calls[1].init.body, undefined);
  const review = await client.requestToolsetReview('ws_a', 'set_a', 'csrf_a');
  assert.equal(review.state, 'approved'); assert.equal(calls[2].init.body, undefined); assert.equal(new Headers(calls[2].init.headers).get('X-Mender-CSRF'), 'csrf_a');
});

test('Console Catalog refuses settlement fields in PriceVersion options', async () => {
  const leaked = snapshot(); leaked.data.price_versions[0].charge_micro = '10';
  await assert.rejects(createConsoleCatalogClient('', async () => Response.json(leaked)).snapshot('ws_a'), /settlement/);
});
