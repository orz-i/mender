import assert from 'node:assert/strict';
import { readFileSync, statSync } from 'node:fs';
import { isAbsolute, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const projectRoot = fileURLToPath(new URL('../', import.meta.url));
const evidencePath = resolve(projectRoot, 'docs/engineering/s4b-release-governance-evidence.json');

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
  assert.ok(!within.startsWith('..') && !isAbsolute(within) && statSync(path).isFile(), `Invalid S4-B evidence path ${name}`);
  return path;
}

export function loadS4BEvidence(path = evidencePath) {
  return JSON.parse(readFileSync(path, 'utf8'));
}

export function validateS4BEvidence(evidence) {
  exactKeys(evidence, ['schema_version', 'as_of', 'work_package', 'status', 'release_model', 'routing', 'emergency_disable', 'supply_evidence', 'security', 'surfaces', 'verification', 'deferred', 'next_work_package', 'document'], 'S4-B evidence');
  assert.equal(evidence.schema_version, 1);
  assert.equal(evidence.as_of, '2026-09-16');
  assert.equal(evidence.work_package, 'S4-B');
  assert.equal(evidence.status, 'complete');

  exactKeys(evidence.release_model, ['scope_key', 'plan_states', 'route_modes', 'canary_strategy', 'percentage_random_split', 'published_tool_version_mutated', 'historical_run_repinning_allowed', 'route_snapshot_separate_from_release_plan'], 'S4-B release model');
  exactSet(evidence.release_model.scope_key, ['workspace_id', 'toolset_version_id', 'tool_version_id'], 'Release scope key');
  exactSet(evidence.release_model.plan_states, ['draft', 'canary', 'active', 'draining', 'rolled_back', 'disabled'], 'ReleasePlan states');
  exactSet(evidence.release_model.route_modes, ['stable', 'canary', 'draining', 'disabled'], 'Release route modes');
  assert.equal(evidence.release_model.canary_strategy, 'workspace-toolset-toolversion-cohort');
  assert.equal(evidence.release_model.percentage_random_split, false);
  assert.equal(evidence.release_model.published_tool_version_mutated, false);
  assert.equal(evidence.release_model.historical_run_repinning_allowed, false);
  assert.equal(evidence.release_model.route_snapshot_separate_from_release_plan, true);

  exactKeys(evidence.routing, ['new_admission_route_server_authoritative', 'canary_selects_candidate', 'drain_selects_prior_stable', 'rollback_selects_prior_stable', 'promotion_advances_route_stable', 'run_admission_deployment_revision_is_pin', 'route_cas_revisioned'], 'S4-B routing');
  for (const key of Object.keys(evidence.routing)) assert.equal(evidence.routing[key], true, `${key} must remain true`);

  exactKeys(evidence.emergency_disable, ['distinct_from_rollback', 'blocks_new_admission', 'candidate_deployment_disabled', 'rollback_reenables_candidate', 'existing_run_control_allows_disabled_deployment'], 'S4-B emergency disable');
  assert.equal(evidence.emergency_disable.distinct_from_rollback, true);
  assert.equal(evidence.emergency_disable.blocks_new_admission, true);
  assert.equal(evidence.emergency_disable.candidate_deployment_disabled, true);
  assert.equal(evidence.emergency_disable.rollback_reenables_candidate, false);
  assert.equal(evidence.emergency_disable.existing_run_control_allows_disabled_deployment, true);

  exactKeys(evidence.supply_evidence, ['candidate_provider_must_match', 'stable_and_candidate_must_be_active_before_canary', 'published_plugin_version_required', 'published_toolset_binding_required', 'published_tool_version_required'], 'S4-B supply evidence');
  for (const key of Object.keys(evidence.supply_evidence)) assert.equal(evidence.supply_evidence[key], true, `${key} must remain true`);

  exactKeys(evidence.security, ['force_rls', 'release_manager_least_privilege', 'runtime_route_function_only', 'release_manager_direct_route_dml_allowed', 'release_manager_direct_deployment_dml_allowed', 'release_manage_roles', 'developer_release_manage_allowed', 'browser_client_controls_state_revision_actor'], 'S4-B security');
  assert.equal(evidence.security.force_rls, true);
  assert.equal(evidence.security.release_manager_least_privilege, true);
  assert.equal(evidence.security.runtime_route_function_only, true);
  assert.equal(evidence.security.release_manager_direct_route_dml_allowed, false);
  assert.equal(evidence.security.release_manager_direct_deployment_dml_allowed, false);
  exactSet(evidence.security.release_manage_roles, ['owner', 'admin'], 'Release manage roles');
  assert.equal(evidence.security.developer_release_manage_allowed, false);
  assert.equal(evidence.security.browser_client_controls_state_revision_actor, false);

  exactKeys(evidence.surfaces, ['admin_release_api', 'admission_route_resolver', 'admin_release_frontend', 'publisher_release_frontend'], 'S4-B surfaces');
  assert.equal(evidence.surfaces.admin_release_api, 'implemented');
  assert.equal(evidence.surfaces.admission_route_resolver, 'implemented');
  assert.equal(evidence.surfaces.admin_release_frontend, 'deferred');
  assert.equal(evidence.surfaces.publisher_release_frontend, 'deferred');

  exactKeys(evidence.verification, ['real_postgres_test', 'release_migration', 'admission_resolver', 'control_broker', 'release_api'], 'S4-B verification files');
  assert.deepEqual(evidence.verification, {
    real_postgres_test: 'backend/tests/integration/release_governance_test.go',
    release_migration: 'backend/migrations/0041_supply_release_governance.sql',
    admission_resolver: 'backend/internal/processes/admission/adapters/outbound/capabilities/resolver.go',
    control_broker: 'backend/internal/contexts/supply/application/broker.go',
    release_api: 'backend/internal/contexts/supply/adapters/inbound/httpapi/release_governance.go',
  });
  exactSet(evidence.deferred, ['percentage-weighted-canary', 'automated-metric-promotion', 'admin-release-frontend', 'publisher-release-frontend', 'dangerous-operation-jit-generalization', 'commercial-accounting', 'refund-ledger', 'payment-adapter', 'cli-skill-distribution', 'artifact-signing-dependency-scan', 'production-deploy'], 'S4-B deferred scope');
  assert.equal(evidence.next_work_package, 'S4-C-dangerous-operation-jit');
  assert.equal(evidence.document, 'docs/engineering/2026-09-16-s4b-release-governance-boundary.md');
}

export function checkS4BReleaseGovernance(root = projectRoot) {
  const evidence = loadS4BEvidence(resolve(root, 'docs/engineering/s4b-release-governance-evidence.json'));
  validateS4BEvidence(evidence);
  const migration = readFileSync(localFile(root, evidence.verification.release_migration), 'utf8');
  const integration = readFileSync(localFile(root, evidence.verification.real_postgres_test), 'utf8');
  const resolver = readFileSync(localFile(root, evidence.verification.admission_resolver), 'utf8');
  const broker = readFileSync(localFile(root, evidence.verification.control_broker), 'utf8');
  const releaseAPI = readFileSync(localFile(root, evidence.verification.release_api), 'utf8');
  const role = readFileSync(localFile(root, 'backend/migrations/release_manager_role.go'), 'utf8');
  const runtimeRole = readFileSync(localFile(root, 'backend/internal/platform/postgres/pool.go'), 'utf8');

  for (const phrase of ['release_plans', 'release_routes', 'release_audit_events', 'FORCE ROW LEVEL SECURITY', 'release_plan_issues', 'start_release_canary', 'promote_release', 'drain_release', 'rollback_release', 'emergency_disable_release', 'resolve_release_route']) assert.ok(migration.includes(phrase), `Release migration missing ${phrase}`);
  for (const phrase of ['historical Run deployment was rewritten', 'production admission resolver did not select canary candidate', 'promotion mutated immutable ToolVersion deployment', 'emergency-disabled release still admitted new traffic', 'rollback silently re-enabled emergency-disabled candidate', 'release-manager RLS exposed another Workspace']) assert.ok(integration.includes(phrase), `T23 PostgreSQL drill missing ${phrase}`);
  assert.ok(resolver.includes('ResolveReleaseRoute') && resolver.includes('DeploymentRevision: route.DeploymentRevision'), 'Admission resolver does not pin the release decision');
  assert.ok(broker.includes('Disabled deployments may still reconcile') && broker.includes('PrepareControl'), 'Existing-run disabled deployment control boundary drifted');
  assert.ok(releaseAPI.includes('/canary') && releaseAPI.includes('/promote') && releaseAPI.includes('/drain') && releaseAPI.includes('/rollback') && releaseAPI.includes('/emergency-disable'), 'Admin release routes incomplete');
  assert.ok(role.includes('NOT') === false || role.includes('GRANT SELECT ON supply.release_plans'), 'Release-manager role evidence missing');
  assert.ok(role.includes('GRANT EXECUTE ON FUNCTION supply.emergency_disable_release') && !role.includes('GRANT UPDATE ON supply.deployments'), 'Release-manager authority drifted');
  assert.ok(runtimeRole.includes("supply.resolve_release_route") && runtimeRole.includes("NOT has_table_privilege(current_user,'supply.release_routes'"), 'Runtime route-only contract drifted');

  const document = readFileSync(localFile(root, evidence.document), 'utf8');
  for (const phrase of ['S4-B Release Governance / T23：COMPLETE', '不是百分比随机分流', 'Emergency disable **不是普通 rollback**', '不会迁移旧 Run', '不是 production-ready 声明']) assert.ok(document.includes(phrase), `S4-B boundary document missing ${phrase}`);
  return evidence;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const evidence = checkS4BReleaseGovernance();
  console.log(`PASS: Mender ${evidence.work_package} Release Governance / T23 is ${evidence.status.toUpperCase()}.`);
  console.log('LIMIT: percentage-weighted canary, automated metric promotion, release frontends, JIT generalization, commercial accounting, payment, supply-chain certification and production deploy remain deferred.');
}

