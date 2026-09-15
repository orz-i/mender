import assert from 'node:assert/strict';
import { readFileSync, statSync } from 'node:fs';
import { isAbsolute, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const projectRoot = fileURLToPath(new URL('../', import.meta.url));
const evidencePath = resolve(projectRoot, 'docs/engineering/s4a-plugin-publication-evidence.json');

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
  assert.ok(!within.startsWith('..') && !isAbsolute(within) && statSync(path).isFile(), `Invalid S4-A evidence path ${name}`);
  return path;
}

export function loadS4AEvidence(path = evidencePath) {
  return JSON.parse(readFileSync(path, 'utf8'));
}

export function validateS4AEvidence(evidence) {
  exactKeys(evidence, ['schema_version', 'as_of', 'work_package', 'status', 'manifest', 'publication', 'governance', 'security', 'surfaces', 'verification', 'deferred', 'next_work_package', 'document'], 'S4-A evidence');
  assert.equal(evidence.schema_version, 1);
  assert.equal(evidence.as_of, '2026-09-15');
  assert.equal(evidence.work_package, 'S4-A');
  assert.equal(evidence.status, 'complete');

  exactKeys(evidence.manifest, ['api_version', 'allowed_capability_kinds', 'arbitrary_executable_artifacts_allowed', 'arbitrary_scripts_allowed', 'secrets_allowed', 'client_controlled_permissions_allowed'], 'S4-A manifest boundary');
  assert.equal(evidence.manifest.api_version, 'mender.io/plugin/v1alpha1');
  exactSet(evidence.manifest.allowed_capability_kinds, ['api_tool', 'mcp_tool', 'agent'], 'S4-A capability kinds');
  for (const key of ['arbitrary_executable_artifacts_allowed', 'arbitrary_scripts_allowed', 'secrets_allowed', 'client_controlled_permissions_allowed']) assert.equal(evidence.manifest[key], false, `${key} must remain false`);

  exactKeys(evidence.publication, ['states', 'published_manifest_immutable', 'publisher_workspace_owned', 'exact_revision_approval_binding', 'capability_preflight_on_submit', 'capability_preflight_on_publish'], 'S4-A publication boundary');
  exactSet(evidence.publication.states, ['draft', 'submitted', 'approved', 'published', 'deprecated', 'disabled'], 'Plugin publication states');
  for (const key of ['published_manifest_immutable', 'publisher_workspace_owned', 'exact_revision_approval_binding', 'capability_preflight_on_submit', 'capability_preflight_on_publish']) assert.equal(evidence.publication[key], true, `${key} must remain true`);

  exactKeys(evidence.governance, ['maker_checker', 'self_approval_allowed', 'append_only_audit', 'publisher_can_approve', 'reviewer_can_publish'], 'S4-A governance boundary');
  assert.equal(evidence.governance.maker_checker, true);
  assert.equal(evidence.governance.self_approval_allowed, false);
  assert.equal(evidence.governance.append_only_audit, true);
  assert.equal(evidence.governance.publisher_can_approve, false);
  assert.equal(evidence.governance.reviewer_can_publish, false);

  exactKeys(evidence.security, ['force_rls', 'publisher_manager_least_privilege', 'governance_reviewer_least_privilege', 'browser_projection_exposes_owner_user_id', 'browser_projection_revision_encoding'], 'S4-A security boundary');
  assert.equal(evidence.security.force_rls, true);
  assert.equal(evidence.security.publisher_manager_least_privilege, true);
  assert.equal(evidence.security.governance_reviewer_least_privilege, true);
  assert.equal(evidence.security.browser_projection_exposes_owner_user_id, false);
  assert.equal(evidence.security.browser_projection_revision_encoding, 'decimal-string');

  exactKeys(evidence.surfaces, ['publisher_console_api', 'admin_review_api', 'publisher_frontend_workbench', 'admin_frontend_review_workbench'], 'S4-A surfaces');
  assert.equal(evidence.surfaces.publisher_console_api, 'implemented');
  assert.equal(evidence.surfaces.admin_review_api, 'implemented');
  assert.equal(evidence.surfaces.publisher_frontend_workbench, 'deferred');
  assert.equal(evidence.surfaces.admin_frontend_review_workbench, 'deferred');

  exactKeys(evidence.verification, ['real_postgres_test', 'contract_schema', 'supply_migration', 'governance_migration'], 'S4-A verification files');
  assert.deepEqual(evidence.verification, {
    real_postgres_test: 'backend/tests/integration/plugin_publication_test.go',
    contract_schema: 'contracts/plugin-manifest.schema.json',
    supply_migration: 'backend/migrations/0039_supply_plugin_publication.sql',
    governance_migration: 'backend/migrations/0040_governance_plugin_publication.sql',
  });
  exactSet(evidence.deferred, ['release-canary', 'release-drain', 'release-rollback', 'emergency-disable-admission', 'publisher-frontend-workbench', 'admin-frontend-review-workbench', 'commercial-accounting', 'payment-adapter', 'cli-skill-distribution', 'production-deploy'], 'S4-A deferred scope');
  assert.equal(evidence.next_work_package, 'S4-B-release-governance');
  assert.equal(evidence.document, 'docs/engineering/2026-09-15-s4a-plugin-publication-boundary.md');
}

export function checkS4APluginPublication(root = projectRoot) {
  const evidence = loadS4AEvidence(resolve(root, 'docs/engineering/s4a-plugin-publication-evidence.json'));
  validateS4AEvidence(evidence);
  const schema = JSON.parse(readFileSync(localFile(root, evidence.verification.contract_schema), 'utf8'));
  assert.equal(schema.properties.apiVersion.const, evidence.manifest.api_version);
  assert.equal(schema.additionalProperties, false);
  const capabilityKinds = schema.properties.capabilities.items.oneOf.map((item) => item.properties.kind.const);
  exactSet(capabilityKinds, evidence.manifest.allowed_capability_kinds, 'Schema capability kinds');
  for (const forbidden of ['artifact', 'script', 'endpoint', 'permissions', 'secret']) assert.ok(!(forbidden in schema.properties), `Manifest schema exposes forbidden top-level field ${forbidden}`);

  const supplyMigration = readFileSync(localFile(root, evidence.verification.supply_migration), 'utf8');
  const governanceMigration = readFileSync(localFile(root, evidence.verification.governance_migration), 'utf8');
  const integration = readFileSync(localFile(root, evidence.verification.real_postgres_test), 'utf8');
  const bootstrap = readFileSync(localFile(root, 'backend/internal/bootstrap/persistence.go'), 'utf8');
  const publisherHTTP = readFileSync(localFile(root, 'backend/internal/contexts/supply/adapters/inbound/httpapi/publication_workflow.go'), 'utf8');
  const reviewHTTP = readFileSync(localFile(root, 'backend/internal/contexts/governance/adapters/inbound/httpapi/plugin_publication_review.go'), 'utf8');
  for (const phrase of ['FORCE ROW LEVEL SECURITY', 'guard_plugin_version_mutation', 'plugin_version_publish_issues', 'mark_plugin_submitted', 'publish_plugin_version']) assert.ok(supplyMigration.includes(phrase), `Supply migration missing ${phrase}`);
  for (const phrase of ['plugin_publication_approvals', 'plugin_publication_audit_events', 'submit_plugin_publication', 'approve_plugin_publication', 'reject_plugin_publication', 'publish_approved_plugin', 'requester cannot approve own plugin publication']) assert.ok(governanceMigration.includes(phrase), `Governance migration missing ${phrase}`);
  for (const phrase of ['direct malformed Plugin manifest', 'Publisher RLS exposed another Workspace', 'requester self-approved Plugin publication', 'stale capability approval bypassed final publication preflight', 'published PluginVersion was deletable', 'raw lifecycle function authority']) assert.ok(integration.includes(phrase), `PostgreSQL integration evidence missing ${phrase}`);
  for (const phrase of ['MENDER_CONSOLE_PUBLISHER_ENABLED', 'MENDER_PUBLISHER_MANAGER_DATABASE_URL', 'MENDER_ADMIN_PLUGIN_REVIEW_ENABLED', 'PublisherManagerRole', 'GovernanceReviewerRole']) assert.ok(bootstrap.includes(phrase), `Bootstrap boundary missing ${phrase}`);
  assert.ok(publisherHTTP.includes('/submit') && publisherHTTP.includes('/publish'), 'Publisher workflow HTTP routes missing');
  assert.ok(reviewHTTP.includes('/approve') && reviewHTTP.includes('/reject'), 'Admin Plugin review HTTP routes missing');

  const document = readFileSync(localFile(root, evidence.document), 'utf8');
  for (const phrase of ['S4-A：COMPLETE', '不是公共插件代码执行平台', 'S4-B Release Governance', 'production deploy', '不能解释为 payment / revenue accounting']) assert.ok(document.includes(phrase), `S4-A boundary document missing ${phrase}`);
  return evidence;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const evidence = checkS4APluginPublication();
  console.log(`PASS: Mender ${evidence.work_package} Plugin / Publisher publication model is ${evidence.status.toUpperCase()}.`);
  console.log('LIMIT: release canary/drain/rollback/emergency disable, frontend workbenches, commercial accounting, payment, CLI/Skill distribution and production deploy remain deferred.');
}

