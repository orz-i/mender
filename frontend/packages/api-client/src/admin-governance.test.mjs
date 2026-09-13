import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createAdminGovernanceClient } from './index.ts';

const approval = (state = 'pending') => ({ id: 'approval_a', target_kind: 'toolset', target_id: 'set_a', target_revision: '9007199254740993', requester_user_id: 'maker_a', state, requested_at: '2026-09-13T00:00:00Z', expires_at: '2026-09-13T00:30:00Z', reviewer_user_id: state === 'approved' ? 'reviewer_a' : null, reviewed_at: state === 'approved' ? '2026-09-13T00:01:00Z' : null, decision_note: state === 'approved' ? 'reviewed' : '', consumed_at: null });

test('Admin Governance lists exact revision with browser session transport only', async () => {
  const client = createAdminGovernanceClient('', async (url, init) => {
    assert.equal(url, '/api/admin/v1/workspaces/ws_a/publication-approvals'); assert.equal(init.credentials, 'same-origin'); assert.equal(new Headers(init.headers).get('Authorization'), null);
    return Response.json({ data: [approval()] });
  });
  const items = await client.list('ws_a'); assert.equal(items[0].targetRevision, '9007199254740993'); assert.equal(typeof items[0].targetRevision, 'string');
});

test('Admin Governance approve sends only review note plus CSRF and never reviewer identity', async () => {
  const client = createAdminGovernanceClient('', async (url, init) => {
    assert.equal(url, '/api/admin/v1/workspaces/ws_a/publication-approvals/approval_a/approve'); assert.equal(new Headers(init.headers).get('X-Mender-CSRF'), 'csrf_a'); assert.equal(new Headers(init.headers).get('Authorization'), null);
    assert.deepEqual(JSON.parse(init.body), { note: 'reviewed' }); return Response.json({ data: approval('approved') });
  });
  const item = await client.approve('ws_a', 'approval_a', 'reviewed', 'csrf_a'); assert.equal(item.reviewerUserId, 'reviewer_a');
});

test('Admin Governance fails closed on sensitive approval projection', async () => {
  const leaked = approval(); leaked.secret = 'forbidden';
  await assert.rejects(createAdminGovernanceClient('', async () => Response.json({ data: [leaked] })).list('ws_a'), /敏感字段/);
});

const auditEvent = () => ({
  sequence: '9007199254740997', approval_id: 'approval_a', target_kind: 'toolset', target_id: 'set_a',
  target_revision: '9007199254740993', observed_revision: '9007199254740994', event_kind: 'approval_expired',
  actor_user_id: null, occurred_at: '2026-09-13T00:02:00Z', reason_code: 'revision_drift', note: '',
});

test('Admin Governance history preserves exact sequence/revisions and bounded filters', async () => {
  const client = createAdminGovernanceClient('', async (url, init) => {
    assert.equal(url, '/api/admin/v1/workspaces/ws_a/publication-history?target_kind=toolset&target_id=set_a&approval_id=approval_a&event_kind=approval_expired&before_sequence=9007199254740998&limit=25');
    assert.equal(init.credentials, 'same-origin');
    assert.equal(new Headers(init.headers).get('Authorization'), null);
    assert.equal(new Headers(init.headers).get('X-Mender-CSRF'), null);
    return Response.json({ data: { events: [auditEvent()], next_before_sequence: '9007199254740997' } });
  });
  const page = await client.history('ws_a', { targetKind: 'toolset', targetId: 'set_a', approvalId: 'approval_a', eventKind: 'approval_expired', beforeSequence: '9007199254740998', limit: 25 });
  assert.equal(page.events[0].sequence, '9007199254740997');
  assert.equal(page.events[0].targetRevision, '9007199254740993');
  assert.equal(page.events[0].observedRevision, '9007199254740994');
  assert.equal(page.nextBeforeSequence, '9007199254740997');
});

test('Admin Governance history rejects numeric precision drift and sensitive payloads', async () => {
  const numeric = auditEvent(); numeric.sequence = 9007199254740996;
  await assert.rejects(createAdminGovernanceClient('', async () => Response.json({ data: { events: [numeric], next_before_sequence: null } })).history('ws_a'), /Governance 响应|exact integer/);
  const leaked = auditEvent(); leaked.arguments = { secret: true };
  await assert.rejects(createAdminGovernanceClient('', async () => Response.json({ data: { events: [leaked], next_before_sequence: null } })).history('ws_a'), /敏感字段/);
  const client = createAdminGovernanceClient('', async () => Response.json({ data: { events: [], next_before_sequence: null } }));
  await assert.rejects(client.history('ws_a', { beforeSequence: '0' }), /cursor/);
  await assert.rejects(client.history('ws_a', { limit: 101 }), /limit/);
});

const policyRevision = (state = 'active') => ({
  id: 'policy_2', revision: '9007199254740993', state, max_risk_level: 'high', deny_unsafe_write: true, deny_mcp_unsafe_write: true,
  created_by_user_id: 'owner_a', created_at: '2026-09-13T00:00:00Z', activated_by_user_id: state === 'draft' ? null : 'admin_a',
  activated_at: state === 'draft' ? null : '2026-09-13T00:01:00Z', retired_at: null,
});
const policyDecision = () => ({
  sequence: '9007199254740997', policy_revision_id: 'policy_2', policy_revision: '9007199254740993', target_kind: 'toolset', target_id: 'set_a',
  target_revision: '9007199254740995', risk_level: 'critical', outcome: 'deny', reason_codes: ['tool_write_unsafe', 'risk_above_ceiling'], evaluated_at: '2026-09-13T00:02:00Z',
});

test('Admin Governance policy snapshot preserves exact revisions and safe server decisions', async () => {
  const client = createAdminGovernanceClient('', async (url, init) => {
    assert.equal(url, '/api/admin/v1/workspaces/ws_a/publication-policy');
    assert.equal(init.credentials, 'same-origin'); assert.equal(new Headers(init.headers).get('Authorization'), null);
    return Response.json({ data: { revisions: [policyRevision()], decisions: [policyDecision()] } });
  });
  const result = await client.policy('ws_a');
  assert.equal(result.revisions[0].revision, '9007199254740993');
  assert.equal(result.decisions[0].sequence, '9007199254740997');
  assert.equal(result.decisions[0].targetRevision, '9007199254740995');
});

test('Admin Governance creates only declarative policy fields and activates with empty body', async () => {
  const calls = [];
  const client = createAdminGovernanceClient('', async (url, init) => {
    calls.push({ url, init });
    return Response.json({ data: policyRevision(url.endsWith('/activate') ? 'active' : 'draft') }, { status: url.endsWith('/activate') ? 200 : 201 });
  });
  await client.createPolicy('ws_a', { id: 'policy_2', maxRiskLevel: 'high', denyUnsafeWrite: true, denyMcpUnsafeWrite: false }, 'csrf_a');
  assert.equal(calls[0].url, '/api/admin/v1/workspaces/ws_a/publication-policy/revisions');
  assert.deepEqual(JSON.parse(calls[0].init.body), { id: 'policy_2', max_risk_level: 'high', deny_unsafe_write: true, deny_mcp_unsafe_write: false });
  assert.equal(new Headers(calls[0].init.headers).get('X-Mender-CSRF'), 'csrf_a');
  await client.activatePolicy('ws_a', 'policy_2', 'csrf_a');
  assert.equal(calls[1].url, '/api/admin/v1/workspaces/ws_a/publication-policy/revisions/policy_2/activate');
  assert.equal(calls[1].init.body, undefined);
});

test('Admin Governance policy fails closed on precision drift and sensitive projection', async () => {
  const numeric = policyDecision(); numeric.sequence = Number('9007199254740997');
  await assert.rejects(createAdminGovernanceClient('', async () => Response.json({ data: { revisions: [policyRevision()], decisions: [numeric] } })).policy('ws_a'), /exact integer|Governance 响应/);
  const leaked = policyDecision(); leaked.arguments = { secret: true };
  await assert.rejects(createAdminGovernanceClient('', async () => Response.json({ data: { revisions: [policyRevision()], decisions: [leaked] } })).policy('ws_a'), /敏感字段/);
});
