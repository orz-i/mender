import assert from 'node:assert/strict';
import { readFileSync, statSync } from 'node:fs';
import { isAbsolute, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const projectRoot = fileURLToPath(new URL('../', import.meta.url));
const evidencePath = resolve(projectRoot, 'docs/engineering/s4c-dangerous-operation-jit-evidence.json');

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
  assert.ok(!within.startsWith('..') && !isAbsolute(within) && statSync(path).isFile(), `Invalid S4-C evidence path ${name}`);
  return path;
}

export function loadS4CEvidence(path = evidencePath) {
  return JSON.parse(readFileSync(path, 'utf8'));
}

export function validateS4CEvidence(evidence) {
  exactKeys(evidence, ['schema_version', 'as_of', 'work_package', 'status', 'approval_contract', 'jit_support', 'security', 'surfaces', 'verification', 'deferred', 'next_work_package', 'document'], 'S4-C evidence');
  assert.equal(evidence.schema_version, 1);
  assert.equal(evidence.as_of, '2026-09-16');
  assert.equal(evidence.work_package, 'S4-C');
  assert.equal(evidence.status, 'complete');

  exactKeys(evidence.approval_contract, ['implemented_actions', 'maker_checker', 'self_approval_allowed', 'exact_binding_fields', 'approval_ttl_min_seconds', 'approval_ttl_max_seconds', 'one_time_consumption', 'browser_approval_flags_authoritative', 'release_revision_server_bound', 'release_emergency_requires_approval', 'support_parameters_digest_server_authoritative', 'amount_currency_binding_supported', 'financial_consumer_implemented', 'generic_submit_granted_to_restricted_manager'], 'S4-C approval contract');
  exactSet(evidence.approval_contract.implemented_actions, ['release.emergency_disable', 'support.workspace_read'], 'Dangerous actions');
  exactSet(evidence.approval_contract.exact_binding_fields, ['subject_kind', 'subject_id', 'action', 'target_kind', 'target_id', 'target_version', 'parameters_sha256', 'amount_micro', 'currency', 'expires_at'], 'Dangerous binding fields');
  assert.equal(evidence.approval_contract.maker_checker, true);
  assert.equal(evidence.approval_contract.self_approval_allowed, false);
  assert.equal(evidence.approval_contract.approval_ttl_min_seconds, 60);
  assert.equal(evidence.approval_contract.approval_ttl_max_seconds, 1800);
  assert.equal(evidence.approval_contract.one_time_consumption, true);
  assert.equal(evidence.approval_contract.browser_approval_flags_authoritative, false);
  assert.equal(evidence.approval_contract.release_revision_server_bound, true);
  assert.equal(evidence.approval_contract.release_emergency_requires_approval, true);
  assert.equal(evidence.approval_contract.support_parameters_digest_server_authoritative, true);
  assert.equal(evidence.approval_contract.amount_currency_binding_supported, true);
  assert.equal(evidence.approval_contract.financial_consumer_implemented, false);
  assert.equal(evidence.approval_contract.generic_submit_granted_to_restricted_manager, false);

  exactKeys(evidence.jit_support, ['platform_staff_separate_from_workspace_membership', 'creates_workspace_membership', 'platform_staff_roles', 'requester_roles', 'reviewer_roles', 'allowed_scopes', 'mutation_scopes_allowed', 'grant_ttl_min_seconds', 'grant_ttl_max_seconds', 'request_approval_ttl_seconds', 'activation_rebind_allowed', 'expiry_database_enforced', 'revocation_database_enforced', 'support_reader_direct_tenant_table_read_allowed', 'support_reader_function_only', 'safe_run_projection_fields'], 'S4-C JIT support');
  assert.equal(evidence.jit_support.platform_staff_separate_from_workspace_membership, true);
  assert.equal(evidence.jit_support.creates_workspace_membership, false);
  exactSet(evidence.jit_support.platform_staff_roles, ['support', 'reviewer', 'operator', 'auditor'], 'Platform Staff roles');
  exactSet(evidence.jit_support.requester_roles, ['support', 'operator'], 'JIT requester roles');
  exactSet(evidence.jit_support.reviewer_roles, ['reviewer', 'operator'], 'JIT reviewer roles');
  exactSet(evidence.jit_support.allowed_scopes, ['workspace:read', 'run:read', 'usage:read'], 'JIT scopes');
  assert.equal(evidence.jit_support.mutation_scopes_allowed, false);
  assert.equal(evidence.jit_support.grant_ttl_min_seconds, 300);
  assert.equal(evidence.jit_support.grant_ttl_max_seconds, 3600);
  assert.equal(evidence.jit_support.request_approval_ttl_seconds, 900);
  assert.equal(evidence.jit_support.activation_rebind_allowed, false);
  assert.equal(evidence.jit_support.expiry_database_enforced, true);
  assert.equal(evidence.jit_support.revocation_database_enforced, true);
  assert.equal(evidence.jit_support.support_reader_direct_tenant_table_read_allowed, false);
  assert.equal(evidence.jit_support.support_reader_function_only, true);
  exactSet(evidence.jit_support.safe_run_projection_fields, ['id', 'state', 'version', 'created_at', 'updated_at'], 'Safe support Run projection');

  exactKeys(evidence.security, ['force_rls', 'append_only_audit', 'browser_session_csrf', 'database_roles', 'dangerous_manager_can_consume_release_approval', 'release_manager_can_request_or_review_approval', 'support_reader_can_read_jit_table', 'support_reader_can_read_identity_tables', 'support_reader_can_read_execution_tables_directly', 'credential_or_secret_access', 'unrestricted_impersonation'], 'S4-C security');
  assert.equal(evidence.security.force_rls, true);
  assert.equal(evidence.security.append_only_audit, true);
  assert.equal(evidence.security.browser_session_csrf, true);
  exactSet(evidence.security.database_roles, ['dangerous-operation-manager', 'release-manager', 'support-reader'], 'S4-C database roles');
  for (const key of ['dangerous_manager_can_consume_release_approval', 'release_manager_can_request_or_review_approval', 'support_reader_can_read_jit_table', 'support_reader_can_read_identity_tables', 'support_reader_can_read_execution_tables_directly', 'credential_or_secret_access', 'unrestricted_impersonation']) assert.equal(evidence.security[key], false, `${key} must remain false`);

  exactKeys(evidence.surfaces, ['dangerous_operation_admin_api', 'support_jit_admin_api', 'support_run_read_api', 'dangerous_operation_frontend', 'support_jit_frontend'], 'S4-C surfaces');
  assert.equal(evidence.surfaces.dangerous_operation_admin_api, 'implemented');
  assert.equal(evidence.surfaces.support_jit_admin_api, 'implemented');
  assert.equal(evidence.surfaces.support_run_read_api, 'implemented');
  assert.equal(evidence.surfaces.dangerous_operation_frontend, 'deferred');
  assert.equal(evidence.surfaces.support_jit_frontend, 'deferred');

  exactKeys(evidence.verification, ['real_postgres_test', 'approval_jit_migration', 'release_approval_migration', 'support_request_migration', 'support_read_gate_migration', 'dangerous_operation_api', 'support_access_api'], 'S4-C verification files');
  exactSet(evidence.deferred, ['generalized-dangerous-operation-actions', 'financial-approval-consumer', 'payment-refund-accounting', 'unrestricted-support-impersonation', 'support-secret-or-credential-access', 'workspace-freeze-anomaly-admin', 'dangerous-operation-frontend', 'support-jit-frontend', 'mfa-production-identity-assurance', 'production-deploy'], 'S4-C deferred scope');
  assert.equal(evidence.next_work_package, 'S4-D-admin-operations');
  assert.equal(evidence.document, 'docs/engineering/2026-09-16-s4c-dangerous-operation-jit-boundary.md');
}

export function checkS4CDangerousOperationJIT(root = projectRoot) {
  const evidence = loadS4CEvidence(resolve(root, 'docs/engineering/s4c-dangerous-operation-jit-evidence.json'));
  validateS4CEvidence(evidence);
  const approvalMigration = readFileSync(localFile(root, evidence.verification.approval_jit_migration), 'utf8');
  const releaseMigration = readFileSync(localFile(root, evidence.verification.release_approval_migration), 'utf8');
  const supportRequestMigration = readFileSync(localFile(root, evidence.verification.support_request_migration), 'utf8');
  const supportGateMigration = readFileSync(localFile(root, evidence.verification.support_read_gate_migration), 'utf8');
  const integration = readFileSync(localFile(root, evidence.verification.real_postgres_test), 'utf8');
  const dangerousAPI = readFileSync(localFile(root, evidence.verification.dangerous_operation_api), 'utf8');
  const supportAPI = readFileSync(localFile(root, evidence.verification.support_access_api), 'utf8');
  const dangerousRole = readFileSync(localFile(root, 'backend/migrations/dangerous_operation_manager_role.go'), 'utf8');
  const supportRole = readFileSync(localFile(root, 'backend/migrations/support_reader_role.go'), 'utf8');
  const bootstrap = readFileSync(localFile(root, 'backend/internal/bootstrap/persistence.go'), 'utf8');
  const operator = readFileSync(localFile(root, 'backend/internal/bootstrap/operator.go'), 'utf8');

  for (const phrase of ['dangerous_operation_approvals', 'dangerous_operation_audit_events', 'jit_support_grants', 'FORCE ROW LEVEL SECURITY', 'requester cannot', 'activate_jit_support', 'revoke_jit_support']) assert.ok(approvalMigration.includes(phrase) || (phrase === 'requester cannot' && approvalMigration.includes("reviewer_user_id<>requester_user_id")), `Approval/JIT migration missing ${phrase}`);
  for (const phrase of ['request_release_emergency_approval', 'consume_release_emergency_approval', 'emergency_disable_release']) assert.ok(releaseMigration.includes(phrase), `Release approval migration missing ${phrase}`);
  assert.ok(supportRequestMigration.includes("sha256(convert_to(parameters::text,'UTF8'))"), 'Support parameters digest is not database authoritative');
  for (const phrase of ['list_jit_support_runs', 'active JIT run read grant required', 'authorize_jit_support']) assert.ok(supportGateMigration.includes(phrase), `JIT read gate missing ${phrase}`);
  for (const phrase of ['cross-purpose restricted role was accepted', 'failed stale consumption altered approval', 'consumed emergency approval was reusable', 'Platform Staff unexpectedly became tenant members', 'JIT grant expiry drifted from approved TTL', 'JIT lifecycle mutated tenant membership', 'JIT audit sequence incomplete']) assert.ok(integration.includes(phrase), `T24/T26 PostgreSQL drill missing ${phrase}`);
  assert.ok(dangerousAPI.includes('DisallowUnknownFields') && dangerousAPI.includes('/release-emergency-requests') && dangerousAPI.includes('/approve') && dangerousAPI.includes('/reject'), 'Dangerous-operation HTTP boundary incomplete');
  assert.ok(supportAPI.includes('DisallowUnknownFields') && supportAPI.includes('/jit-requests') && supportAPI.includes('/activate') && supportAPI.includes('/revoke') && supportAPI.includes('/runs'), 'Support JIT HTTP boundary incomplete');
  assert.ok(dangerousRole.includes('REVOKE EXECUTE ON FUNCTION governance.submit_dangerous_operation') && !dangerousRole.includes('GRANT EXECUTE ON FUNCTION governance.consume_release_emergency_approval'), 'Dangerous-operation-manager authority drifted');
  assert.ok(
    supportRole.includes('REVOKE USAGE ON SCHEMA execution FROM') &&
      supportRole.includes('REVOKE ALL ON execution.runs FROM') &&
      supportRole.includes('GRANT EXECUTE ON FUNCTION governance.list_jit_support_runs') &&
      !supportRole.includes('GRANT SELECT ON execution.') &&
      !supportRole.includes('GRANT SELECT (workspace_id'),
    'Support-reader direct tenant access drifted',
  );
  for (const phrase of ['MENDER_ADMIN_DANGEROUS_OPERATION_ENABLED', 'MENDER_ADMIN_SUPPORT_ACCESS_ENABLED', 'MENDER_SUPPORT_READER_DATABASE_URL']) assert.ok(bootstrap.includes(phrase), `Bootstrap boundary missing ${phrase}`);
  for (const phrase of ['grant-dangerous-operation-manager', 'grant-support-reader', 'provision-platform-staff', 'without Workspace membership']) assert.ok(operator.includes(phrase), `Operator boundary missing ${phrase}`);

  const document = readFileSync(localFile(root, evidence.document), 'utf8');
  for (const phrase of ['S4-C Dangerous Operation / JIT Governance｜T24／T26：COMPLETE', '不是通用 impersonation 系统', '没有实现任何 payment/refund/financial approval consumer', '没有 execution tenant 表的直接 SELECT 权限', 'S4-D Admin Operations']) assert.ok(document.includes(phrase), `S4-C boundary document missing ${phrase}`);
  return evidence;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const evidence = checkS4CDangerousOperationJIT();
  console.log(`PASS: Mender ${evidence.work_package} Dangerous Operation / JIT Governance is ${evidence.status.toUpperCase()}.`);
  console.log('LIMIT: generalized dangerous actions, financial consumers, payment/refund accounting, unrestricted impersonation, secret access, admin operations UI, MFA assurance and production deploy remain deferred.');
}
