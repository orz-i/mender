import assert from 'node:assert/strict';
import { test } from 'node:test';
import { ConsolePolicyDeniedError, createConsoleCatalogClient } from './index.ts';

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
    if (url.endsWith('/review-requests')) return Response.json({ data: { approval: snapshot().data.publication_approvals[0], policy_decision: { sequence: '9007199254740995', policy_revision_id: 'policy_a', policy_revision: '9007199254740994', target_kind: 'toolset', target_id: 'set_a', target_revision: '9', risk_level: 'low', outcome: 'allow', reason_codes: ['tool_read_only_safe', 'within_risk_ceiling'], evaluated_at: '2026-09-12T00:00:00Z' } } }, { status: 201 });
    return Response.json({ data: { id: 'set_a', revision: '1', state: 'draft', created_at: '2026-09-12T00:00:00Z', updated_at: '2026-09-12T00:00:00Z', published_at: null, retired_at: null, bindings: [] } }, { status: 201 });
  });
  await client.createToolset('ws_a', 'set_a', 'csrf_a');
  const result = await client.toolsetPreflight('ws_a', 'set_a', 'csrf_a');
  assert.equal(result.ready, false);
  assert.deepEqual(result.issues, [{ code: 'connection_unavailable', targetId: 'conn_a' }]);
  assert.equal(new Headers(calls[0].init.headers).get('X-Mender-CSRF'), 'csrf_a');
  assert.equal(calls[1].init.body, undefined);
  const review = await client.requestToolsetReview('ws_a', 'set_a', 'csrf_a');
  assert.equal(review.approval.state, 'approved'); assert.equal(review.policyDecision.sequence, '9007199254740995'); assert.equal(review.policyDecision.outcome, 'allow');
  assert.equal(calls[2].init.body, undefined); assert.equal(new Headers(calls[2].init.headers).get('X-Mender-CSRF'), 'csrf_a');
});

test('Console Catalog surfaces server-owned policy deny with exact bigint decision and no approval', async () => {
  const client = createConsoleCatalogClient('', async () => Response.json({
    error: { code: 'POLICY_DENIED', message: 'Publication policy denied this target revision.' },
    data: { policy_decision: { sequence: '9007199254740999', policy_revision_id: 'policy_strict', policy_revision: '9007199254740998', target_kind: 'tool_version', target_id: 'tv_a', target_revision: '9007199254740993', risk_level: 'critical', outcome: 'deny', reason_codes: ['tool_write_unsafe', 'risk_above_ceiling'], evaluated_at: '2026-09-12T00:02:00Z' } },
  }, { status: 409 }));
  await assert.rejects(client.requestToolVersionReview('ws_a', 'tv_a', 'csrf_a'), (error) => {
    assert.ok(error instanceof ConsolePolicyDeniedError);
    assert.equal(error.policyDecision.sequence, '9007199254740999');
    assert.equal(error.policyDecision.policyRevision, '9007199254740998');
    assert.equal(error.policyDecision.outcome, 'deny');
    return true;
  });
});

test('Console Catalog policy projection fails closed on sensitive fields', async () => {
  const decision = { sequence: '1', policy_revision_id: 'policy_a', policy_revision: '1', target_kind: 'toolset', target_id: 'set_a', target_revision: '9', risk_level: 'low', outcome: 'allow', reason_codes: ['tool_read_only_safe', 'within_risk_ceiling'], evaluated_at: '2026-09-12T00:00:00Z', token: 'forbidden' };
  const client = createConsoleCatalogClient('', async () => Response.json({ data: { approval: snapshot().data.publication_approvals[0], policy_decision: decision } }, { status: 201 }));
  await assert.rejects(client.requestToolsetReview('ws_a', 'set_a', 'csrf_a'), /publication policy.*敏感字段|敏感字段/);
});

test('Console Catalog refuses settlement fields in PriceVersion options', async () => {
  const leaked = snapshot(); leaked.data.price_versions[0].charge_micro = '10';
  await assert.rejects(createConsoleCatalogClient('', async () => Response.json(leaked)).snapshot('ws_a'), /settlement/);
});

const openAPIDiagnostic = (severity = 'warning') => ({ code: severity === 'warning' ? 'security_runtime_owned' : 'http_method_unsupported', severity, operation_id: 'searchCompanies', path: '/search', method: 'POST', message: severity === 'warning' ? 'Runtime credentials remain server-owned.' : 'Unsupported operation.' });
const openAPIOperation = () => ({
  operation_id: 'searchCompanies', method: 'POST', path: '/search', server_url: 'https://api.example.test/v1', title: 'Search companies', description: '',
  input_schema: { type: 'object', properties: { query: { type: 'string' } }, required: ['query'] }, output_schema: { type: 'object', properties: { items: { type: 'array', items: { type: 'string' } } } },
  side_effect: 'write', idempotency: 'unsafe', importable: true, diagnostics: [openAPIDiagnostic()],
});
const openAPIPreview = () => ({ data: { openapi_version: '3.1.0', title: 'Search API', operations: [openAPIOperation()], diagnostics: [] } });

test('Console Catalog OpenAPI preview sends only inline document and trusts server diagnostics/importability', async () => {
  const document = '{"openapi":"3.1.0"}';
  const client = createConsoleCatalogClient('', async (url, init) => {
    assert.equal(url, '/api/console/v1/workspaces/ws_a/catalog/openapi/preview');
    assert.equal(init.method, 'POST'); assert.equal(init.credentials, 'same-origin');
    assert.equal(new Headers(init.headers).get('Authorization'), null); assert.equal(new Headers(init.headers).get('X-Mender-CSRF'), null);
    assert.deepEqual(JSON.parse(init.body), { document });
    return Response.json(openAPIPreview());
  });
  const preview = await client.previewOpenAPI('ws_a', document);
  assert.equal(preview.openapiVersion, '3.1.0'); assert.equal(preview.operations[0].importable, true);
  assert.equal(preview.operations[0].diagnostics[0].code, 'security_runtime_owned');
  assert.equal(preview.operations[0].sideEffect, 'write'); assert.equal(preview.operations[0].idempotency, 'unsafe');
});

test('Console Catalog OpenAPI import sends explicit mapping only, requires CSRF, and preserves draft precision', async () => {
  const document = '{"openapi":"3.1.0"}'; const calls = [];
  const importedTool = { ...snapshot().data.tool_versions[0], tool_version_id: 'tv_import', tool_id: 'tool_import', version: '2.0.0', provider_id: 'provider_a', price_version_id: 'price_a', deployment_revision: 'deploy_a', title: 'Search companies', input_schema: openAPIOperation().input_schema, output_schema: openAPIOperation().output_schema, side_effect: 'write', idempotency: 'unsafe', mcp_publishable: false, revision: '9007199254740997' };
  const client = createConsoleCatalogClient('', async (url, init) => {
    calls.push({ url, init });
    return Response.json({ data: { tool_version: importedTool, source_operation: openAPIOperation() } }, { status: 201 });
  });
  const result = await client.importOpenAPIOperation('ws_a', { document, operationId: 'searchCompanies', toolVersionId: 'tv_import', toolId: 'tool_import', version: '2.0.0', providerId: 'provider_a', priceVersionId: 'price_a', deploymentRevision: 'deploy_a' }, 'csrf_a');
  assert.equal(calls[0].url, '/api/console/v1/workspaces/ws_a/catalog/openapi/import');
  assert.equal(new Headers(calls[0].init.headers).get('X-Mender-CSRF'), 'csrf_a'); assert.equal(new Headers(calls[0].init.headers).get('Authorization'), null);
  assert.deepEqual(JSON.parse(calls[0].init.body), { document, operation_id: 'searchCompanies', tool_version_id: 'tv_import', tool_id: 'tool_import', version: '2.0.0', provider_id: 'provider_a', price_version_id: 'price_a', deployment_revision: 'deploy_a' });
  assert.equal(result.toolVersion.revision, '9007199254740997'); assert.equal(result.toolVersion.state, 'draft'); assert.equal(result.toolVersion.mcpPublishable, false);
  assert.equal(result.sourceOperation.importable, true);
  await assert.rejects(client.importOpenAPIOperation('ws_a', { document, operationId: 'searchCompanies', toolVersionId: 'tv_import', toolId: 'tool_import', version: '2.0.0', providerId: 'provider_a', priceVersionId: 'price_a', deploymentRevision: 'deploy_a' }, ''), /CSRF/);
});

test('Console Catalog OpenAPI projections fail closed on leaked source, unknown fields, and inconsistent importability', async () => {
  const leaked = openAPIPreview(); leaked.data.document = 'forbidden source';
  await assert.rejects(createConsoleCatalogClient('', async () => Response.json(leaked)).previewOpenAPI('ws_a', '{}'), /OpenAPI preview/);
  const leakedOperation = openAPIPreview(); leakedOperation.data.operations[0].credential_version_ref = 'secret_ref';
  await assert.rejects(createConsoleCatalogClient('', async () => Response.json(leakedOperation)).previewOpenAPI('ws_a', '{}'), /OpenAPI operation/);
  const badSeverity = openAPIPreview(); badSeverity.data.operations[0].diagnostics[0].severity = 'info';
  await assert.rejects(createConsoleCatalogClient('', async () => Response.json(badSeverity)).previewOpenAPI('ws_a', '{}'), /severity/);
  const inconsistent = openAPIPreview(); inconsistent.data.operations[0].method = 'GET';
  await assert.rejects(createConsoleCatalogClient('', async () => Response.json(inconsistent)).previewOpenAPI('ws_a', '{}'), /importable contract/);
  const missingSchema = openAPIPreview(); missingSchema.data.operations[0].input_schema = null;
  await assert.rejects(createConsoleCatalogClient('', async () => Response.json(missingSchema)).previewOpenAPI('ws_a', '{}'), /importable contract/);
});
