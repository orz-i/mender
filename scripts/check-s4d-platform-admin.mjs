import assert from 'node:assert/strict';
import { readFileSync, statSync } from 'node:fs';
import { isAbsolute, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const projectRoot = fileURLToPath(new URL('../', import.meta.url));
const evidencePath = resolve(projectRoot, 'docs/engineering/s4d-platform-admin-evidence.json');

function exactKeys(value, keys, label) {
  assert.equal(typeof value, 'object', `${label} must be an object`);
  assert.ok(value !== null && !Array.isArray(value), `${label} must be an object`);
  assert.deepEqual(Object.keys(value).sort(), [...keys].sort(), `${label} keys drifted`);
}

function exactSet(values, expected, label) {
  assert.ok(Array.isArray(values), `${label} must be an array`);
  assert.equal(new Set(values).size, values.length, `${label} contains duplicates`);
  assert.deepEqual([...values].sort(), [...expected].sort(), `${label} drifted`);
}

function localFile(root, name) {
  const path = resolve(root, name);
  const within = relative(root, path);
  assert.ok(!within.startsWith('..') && !isAbsolute(within) && statSync(path).isFile(), `Invalid S4-D evidence path ${name}`);
  return path;
}

export function loadS4DEvidence(path = evidencePath) {
  return JSON.parse(readFileSync(path, 'utf8'));
}

export function validateS4DEvidence(evidence) {
  exactKeys(evidence, ['schema_version', 'as_of', 'work_package', 'status', 'platform_admin', 'platform_staff', 'workspace_freeze', 'provider_quarantine', 'audit', 'security', 'surfaces', 'verification', 'deferred', 'next_work_package', 'document'], 'S4-D evidence');
  assert.equal(evidence.schema_version, 1);
  assert.equal(evidence.as_of, '2026-09-16');
  assert.equal(evidence.work_package, 'S4-D');
  assert.equal(evidence.status, 'complete');

  exactKeys(evidence.platform_admin, ['workspace_freeze_implemented', 'provider_quarantine_implemented', 'incident_handling_implemented', 'audit_export_implemented', 'workspace_state_revisioned', 'provider_state_revisioned', 'incident_revisioned', 'mutation_reason_required', 'browser_session_csrf', 'browser_actor_fields_authoritative'], 'S4-D platform admin');
  for (const key of ['workspace_freeze_implemented', 'provider_quarantine_implemented', 'incident_handling_implemented', 'audit_export_implemented', 'workspace_state_revisioned', 'provider_state_revisioned', 'incident_revisioned', 'mutation_reason_required', 'browser_session_csrf']) assert.equal(evidence.platform_admin[key], true, `${key} must remain true`);
  assert.equal(evidence.platform_admin.browser_actor_fields_authoritative, false);

  exactKeys(evidence.platform_staff, ['separate_from_workspace_membership', 'creates_workspace_membership', 'roles', 'operate_roles', 'review_roles', 'audit_roles'], 'S4-D Platform Staff');
  assert.equal(evidence.platform_staff.separate_from_workspace_membership, true);
  assert.equal(evidence.platform_staff.creates_workspace_membership, false);
  exactSet(evidence.platform_staff.roles, ['support', 'reviewer', 'operator', 'auditor'], 'Platform Staff roles');
  exactSet(evidence.platform_staff.operate_roles, ['operator'], 'Platform operate roles');
  exactSet(evidence.platform_staff.review_roles, ['reviewer', 'operator'], 'Platform review roles');
  exactSet(evidence.platform_staff.audit_roles, ['reviewer', 'operator', 'auditor'], 'Platform audit roles');

  exactKeys(evidence.workspace_freeze, ['authority_source', 'api_key_rechecked', 'membership_rechecked', 'run_delegation_rechecked', 'start_run_delegation_rechecked', 'historical_facts_deleted'], 'S4-D workspace freeze');
  assert.equal(evidence.workspace_freeze.authority_source, 'identity.workspaces.disabled');
  for (const key of ['api_key_rechecked', 'membership_rechecked', 'run_delegation_rechecked', 'start_run_delegation_rechecked']) assert.equal(evidence.workspace_freeze[key], true, `${key} must remain true`);
  assert.equal(evidence.workspace_freeze.historical_facts_deleted, false);

  exactKeys(evidence.provider_quarantine, ['state_source', 'new_admission_blocked', 'new_submission_blocked', 'mcp_discovery_blocked', 'existing_run_control_allowed', 'deployment_state_mutated', 'historical_admission_rewritten', 'unknown_legacy_provider_default_allowed', 'function_only_runtime_gate'], 'S4-D provider quarantine');
  assert.equal(evidence.provider_quarantine.state_source, 'supply.provider_admin_states');
  for (const key of ['new_admission_blocked', 'new_submission_blocked', 'mcp_discovery_blocked', 'existing_run_control_allowed', 'unknown_legacy_provider_default_allowed', 'function_only_runtime_gate']) assert.equal(evidence.provider_quarantine[key], true, `${key} must remain true`);
  assert.equal(evidence.provider_quarantine.deployment_state_mutated, false);
  assert.equal(evidence.provider_quarantine.historical_admission_rewritten, false);

  exactKeys(evidence.audit, ['append_only', 'max_export_rows', 'safe_fields', 'raw_canonical_arguments_exposed', 'credential_references_exposed', 'secrets_exposed'], 'S4-D audit');
  assert.equal(evidence.audit.append_only, true);
  assert.equal(evidence.audit.max_export_rows, 500);
  exactSet(evidence.audit.safe_fields, ['sequence', 'event_kind', 'target_kind', 'target_id', 'target_revision', 'actor_user_id', 'reason', 'occurred_at'], 'S4-D audit fields');
  for (const key of ['raw_canonical_arguments_exposed', 'credential_references_exposed', 'secrets_exposed']) assert.equal(evidence.audit[key], false, `${key} must remain false`);

  exactKeys(evidence.security, ['database_role', 'direct_business_table_read', 'direct_business_table_mutation', 'runtime_direct_provider_state_read', 'executor_direct_provider_state_read', 'mcp_connector_direct_provider_state_read', 'dangerous_jit_authority_widened', 'financial_mutation', 'unrestricted_impersonation'], 'S4-D security');
  assert.equal(evidence.security.database_role, 'platform-admin-manager');
  for (const key of ['direct_business_table_read', 'direct_business_table_mutation', 'runtime_direct_provider_state_read', 'executor_direct_provider_state_read', 'mcp_connector_direct_provider_state_read', 'dangerous_jit_authority_widened', 'financial_mutation', 'unrestricted_impersonation']) assert.equal(evidence.security[key], false, `${key} must remain false`);

  exactKeys(evidence.surfaces, ['platform_admin_api', 'platform_admin_frontend'], 'S4-D surfaces');
  assert.equal(evidence.surfaces.platform_admin_api, 'implemented');
  assert.equal(evidence.surfaces.platform_admin_frontend, 'deferred');
  exactKeys(evidence.verification, ['real_postgres_test', 'admin_migration', 'runtime_gate_migration', 'function_hardening_migration', 'admin_role', 'admin_role_validator', 'admin_api'], 'S4-D verification');
  exactSet(evidence.deferred, ['platform-admin-frontend', 'financial-reconciliation-and-adjustment', 'payment-provider-integration', 'billing-refund-ledger', 'provider-commercial-onboarding-review', 'unrestricted-impersonation', 'production-deploy'], 'S4-D deferred scope');
  assert.equal(evidence.next_work_package, 'S4-03-commerce-adjustments-refunds');
  assert.equal(evidence.document, 'docs/engineering/2026-09-16-s4d-platform-admin-boundary.md');
}

export function checkS4DPlatformAdmin(root = projectRoot) {
  const evidence = loadS4DEvidence(resolve(root, 'docs/engineering/s4d-platform-admin-evidence.json'));
  validateS4DEvidence(evidence);
  const adminMigration = readFileSync(localFile(root, evidence.verification.admin_migration), 'utf8');
  const runtimeGate = readFileSync(localFile(root, evidence.verification.runtime_gate_migration), 'utf8');
  const hardening = readFileSync(localFile(root, evidence.verification.function_hardening_migration), 'utf8');
  const role = readFileSync(localFile(root, evidence.verification.admin_role), 'utf8');
  const roleValidator = readFileSync(localFile(root, evidence.verification.admin_role_validator), 'utf8');
  const api = readFileSync(localFile(root, evidence.verification.admin_api), 'utf8');
  const integration = readFileSync(localFile(root, evidence.verification.real_postgres_test), 'utf8');
  const staff = readFileSync(localFile(root, 'backend/internal/contexts/identity/domain/platform_staff.go'), 'utf8');
  const credentialRepo = readFileSync(localFile(root, 'backend/internal/contexts/identity/adapters/outbound/postgres/repository.go'), 'utf8');
  const humanRepo = readFileSync(localFile(root, 'backend/internal/contexts/identity/adapters/outbound/postgres/human_session.go'), 'utf8');
  const runDelegation = readFileSync(localFile(root, 'backend/internal/contexts/identity/adapters/outbound/postgres/run_delegation.go'), 'utf8');
  const startDelegation = readFileSync(localFile(root, 'backend/internal/contexts/identity/adapters/outbound/postgres/run_start_delegation.go'), 'utf8');
  const resolver = readFileSync(localFile(root, 'backend/internal/processes/admission/adapters/outbound/capabilities/resolver.go'), 'utf8');
  const broker = readFileSync(localFile(root, 'backend/internal/contexts/supply/application/broker.go'), 'utf8');
  const mcpRuntime = readFileSync(localFile(root, 'backend/internal/contexts/supply/application/mcp_runtime.go'), 'utf8');
  const runtimeGrant = readFileSync(localFile(root, 'backend/migrations/migrate.go'), 'utf8');
  const executorGrant = readFileSync(localFile(root, 'backend/migrations/executor_role.go'), 'utf8');
  const mcpGrant = readFileSync(localFile(root, 'backend/migrations/mcp_connector_role.go'), 'utf8');
  const bootstrap = readFileSync(localFile(root, 'backend/internal/bootstrap/persistence.go'), 'utf8');
  const operator = readFileSync(localFile(root, 'backend/internal/bootstrap/operator.go'), 'utf8');

  for (const phrase of ['workspace_admin_states', 'provider_admin_states', 'platform_incidents', 'platform_admin_audit_events', 'platform_admin_set_workspace_frozen', 'platform_admin_set_provider_state', 'platform_admin_open_incident', 'platform_admin_resolve_incident', 'platform_admin_audit_export', 'immutable_platform_admin_audit']) assert.ok(adminMigration.includes(phrase), `Admin migration missing ${phrase}`);
  assert.ok(runtimeGate.includes('SECURITY DEFINER') && runtimeGate.includes('provider_accepts_new_work') && runtimeGate.includes("state='quarantined'") && runtimeGate.includes('NOT EXISTS'), 'Provider new-work gate drifted');
  for (const phrase of ['CREATE OR REPLACE FUNCTION governance.platform_admin_set_workspace_frozen', 'CREATE OR REPLACE FUNCTION governance.platform_admin_set_provider_state', 'CREATE OR REPLACE FUNCTION governance.platform_admin_resolve_incident']) assert.ok(hardening.includes(phrase), `Function hardening migration missing ${phrase}`);

  assert.ok(role.includes('GRANT EXECUTE ON FUNCTION governance.platform_admin_') && !role.includes('GRANT SELECT ON identity.') && !role.includes('GRANT SELECT ON supply.') && !role.includes('GRANT SELECT ON execution.'), 'Platform Admin manager grants gained direct business reads');
  for (const schema of ['identity', 'supply', 'execution', 'connections', 'commerce']) assert.ok(roleValidator.includes(`NOT has_schema_privilege(current_user,'${schema}','USAGE')`), `Platform Admin role validator no longer isolates ${schema}`);
  assert.ok(roleValidator.includes("NOT has_function_privilege(current_user,'governance.request_support_jit_approval") && roleValidator.includes("NOT has_function_privilege(current_user,'governance.approve_dangerous_operation"), 'Platform Admin role gained JIT/dangerous authority');

  for (const phrase of ["case \"platform:operate\"", 'PlatformOperator', "case \"platform:review\"", "case \"platform:audit\""]) assert.ok(staff.includes(phrase), `Platform Staff authority missing ${phrase}`);
  assert.ok(credentialRepo.includes('JOIN identity.workspaces w ON w.id=k.workspace_id') && credentialRepo.includes('&c.WorkspaceDisabled'), 'API Key does not re-read workspace freeze');
  assert.ok(humanRepo.includes('JOIN identity.workspaces w ON w.id=m.workspace_id') && humanRepo.includes('&m.WorkspaceDisabled'), 'Workspace membership does not re-read workspace freeze');
  assert.ok(runDelegation.includes('JOIN identity.workspaces w ON w.id=d.workspace_id') && runDelegation.includes('&d.WorkspaceDisabled'), 'Run delegation does not re-read workspace freeze');
  assert.ok(startDelegation.includes('JOIN identity.workspaces w ON w.id=d.workspace_id') && startDelegation.includes('&d.WorkspaceDisabled'), 'StartRun delegation does not re-read workspace freeze');

  const providerCheck = resolver.indexOf('r.providers.EnsureProviderAvailable');
  assert.ok(providerCheck >= 0 && providerCheck < resolver.indexOf('r.releases.ResolveReleaseRoute') && providerCheck < resolver.indexOf('r.connections.ResolveAccess') && providerCheck < resolver.indexOf('r.pricing.ResolveAdmissionTerms'), 'Admission provider quarantine gate is too late or missing');
  assert.ok(broker.includes('if !allowDisabled') && broker.includes('ProviderAcceptsNewWork') && broker.includes('PrepareControl') && broker.includes('b.resolve(ctx, ref, at, true, false)'), 'Broker quarantine/control split drifted');
  assert.ok(mcpRuntime.includes('ProviderAcceptsNewWork') && mcpRuntime.includes('PrepareMCPDiscovery'), 'MCP discovery no longer enforces provider quarantine');

  for (const grant of [runtimeGrant, executorGrant, mcpGrant]) {
    assert.ok(grant.includes('GRANT EXECUTE ON FUNCTION supply.provider_accepts_new_work(text)'), 'Runtime-side role lacks provider function gate');
    assert.ok(!grant.includes('GRANT SELECT ON supply.provider_admin_states'), 'Runtime-side role gained raw provider admin reads');
  }

  assert.ok(api.includes('DisallowUnknownFields') && api.includes('X-Mender-CSRF') === false && api.includes('supportActor') && api.includes('expected_revision') && api.includes('/quarantine') && api.includes('/audit'), 'Platform Admin HTTP boundary incomplete');
  for (const phrase of ['Platform Staff unexpectedly received tenant membership', 'workspace freeze did not immediately revoke tenant-effective authorization', 'quarantined provider remained admissible', 'quarantined provider submission reached credential secret resolution', 'quarantine blocked existing-run control/reconciliation material', 'provider quarantine rewrote immutable deployment/admission facts', 'platform audit export leaked tenant payload or credential reference', 'platform audit export missing event']) assert.ok(integration.includes(phrase), `T26 PostgreSQL drill missing ${phrase}`);
  for (const phrase of ['MENDER_ADMIN_PLATFORM_OPERATIONS_ENABLED', 'MENDER_PLATFORM_ADMIN_MANAGER_DATABASE_URL', 'PlatformAdminManagerRole']) assert.ok(bootstrap.includes(phrase), `Bootstrap boundary missing ${phrase}`);
  assert.ok(operator.includes('grant-platform-admin-manager'), 'Operator grant command missing');

  const document = readFileSync(localFile(root, evidence.document), 'utf8');
  for (const phrase of ['S4-D Platform Admin Operations｜S4-05 / FR-018 / T26：COMPLETE', '不是 Deployment disabled，也不是 Release emergency-disable', '没有 `provider_admin_states` 记录的 provider 按原行为允许', 'S4-D 仍不是 production-ready 声明', 'canonical S4-03']) assert.ok(document.includes(phrase), `S4-D boundary document missing ${phrase}`);
  return evidence;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const evidence = checkS4DPlatformAdmin();
  console.log(`PASS: Mender ${evidence.work_package} Platform Admin Operations is ${evidence.status.toUpperCase()}.`);
  console.log('LIMIT: frontend Admin UI, finance reconciliation/adjustments, refunds, payment integration, commercial provider onboarding, impersonation and production deploy remain deferred.');
}
