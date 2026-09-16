import assert from 'node:assert/strict';
import { readFileSync, statSync } from 'node:fs';
import { isAbsolute, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('../', import.meta.url));
const evidencePath = resolve(root, 'docs/engineering/s406-sandbox-payment-evidence.json');

function localFile(name) {
  const path = resolve(root, name);
  const rel = relative(root, path);
  assert.ok(!rel.startsWith('..') && !isAbsolute(rel) && statSync(path).isFile(), `invalid S4-06 evidence path ${name}`);
  return path;
}

export function loadS406Evidence(path = evidencePath) {
  return JSON.parse(readFileSync(path, 'utf8'));
}

export function validateS406Evidence(e) {
  assert.equal(e.schema_version, 1);
  assert.equal(e.as_of, '2026-09-16');
  assert.equal(e.work_package, 'S4-06');
  assert.equal(e.canonical_s4_range, 'S4-01..S4-18');
  assert.equal(e.invented_stage_id, false);
  assert.equal(e.status, 'complete');
  assert.equal(e.scope.mode, 'sandbox');
  assert.equal(e.scope.provider_neutral, true);
  assert.equal(e.scope.live_payments_enabled, false);
  assert.equal(e.scope.browser_redirect_is_payment_truth, false);
  assert.equal(e.scope.real_money_funding_enabled, false);
  for (const key of ['billing_journal_binding_required','collect_charge_requires_charge_journal','execute_refund_requires_refund_journal','amount_currency_exact','business_key_idempotent','binding_immutable']) assert.equal(e.intents[key], true, key);
  assert.equal(e.intents.terminal_revision, 2);
  for (const key of ['hmac_sha256','provider_account_domain_separated','verification_before_persistence','exact_duplicate_replay','fact_drift_quarantined','amount_currency_account_mismatch_quarantined','late_terminal_event_quarantined']) assert.equal(e.callbacks[key], true, key);
  assert.equal(e.callbacks.event_namespace, 'provider/account/event_id');
  for (const key of ['raw_body_persisted','raw_signature_persisted','secret_persisted']) assert.equal(e.callbacks[key], false, key);
  for (const key of ['collection_expected_vs_settled','refund_expected_vs_settled','currency_scoped','quarantine_counted']) assert.equal(e.reconciliation[key], true, key);
  assert.equal(e.reconciliation.refund_callback_mutates_original_journal, false);
  assert.equal(e.reconciliation.callback_creates_billing_journal, false);
  assert.equal(e.security.payment_manager_role, 'payment-manager');
  assert.equal(e.security.callback_role, 'payment-callback-ingestor');
  for (const key of ['roles_distinct','admin_browser_session_csrf','safe_callback_projection_only']) assert.equal(e.security[key], true, key);
  for (const key of ['direct_business_table_access','callback_can_create_intent','manager_can_ingest_callback']) assert.equal(e.security[key], false, key);
  assert.deepEqual([...e.deferred].sort(), ['live-payment-provider-network','production-merchant-credentials','real-money-wallet-funding','fx-conversion','tax-statutory-accounting','provider-payouts','billing-payment-frontend','production-deploy'].sort());
}

export function checkS406SandboxPayment() {
  const e = loadS406Evidence();
  validateS406Evidence(e);
  const contract = readFileSync(localFile(e.verification.contract_migration), 'utf8');
  const replay = readFileSync(localFile(e.verification.replay_hardening_migration), 'utf8');
  const reconciliation = readFileSync(localFile(e.verification.reconciliation_hardening_migration), 'utf8');
  const integration = readFileSync(localFile(e.verification.postgres_test), 'utf8');
  const hmac = readFileSync(localFile(e.verification.hmac_test), 'utf8');
  const config = readFileSync(localFile(e.verification.config_test), 'utf8');
  const api = readFileSync(localFile(e.verification.admin_http), 'utf8');
  const app = readFileSync(localFile('backend/internal/contexts/commerce/application/payment.go'), 'utf8');
  for (const phrase of ["mode='sandbox'", 'payment_intents', 'payment_callback_inbox', 'create_sandbox_payment_intent', 'ingest_sandbox_payment_callback', 'payment_reconciliation']) assert.ok(contract.includes(phrase), `0051 missing ${phrase}`);
  assert.ok(!contract.includes('raw_body') && !contract.includes('raw_signature') && !contract.includes('authorization_header'), 'callback schema gained raw sensitive payload fields');
  assert.ok(replay.includes('UPDATE commerce.payment_callback_inbox AS p') && replay.includes("'duplicate'::text"), 'replay hardening missing');
  assert.ok(reconciliation.includes('p.workspace_id=requested_workspace') && reconciliation.includes('p.currency=currency_value'), 'currency-scoped reconciliation hardening missing');
  const applicationTests = readFileSync(localFile('backend/internal/contexts/commerce/application/payment_test.go'), 'utf8');
  for (const phrase of ['live payment reached repository','unverified callback reached receiver','callback binding drifted']) assert.ok(applicationTests.includes(phrase), `application payment boundary test missing ${phrase}`);
  for (const phrase of ['same event ID fact drift was not quarantined','provider refund confirmation rewrote S4-03 refund journal','payment reconciliation did not rebuild from immutable intent/event facts','payment callback evidence was deletable']) assert.ok(integration.includes(phrase), `T27 PostgreSQL drill missing ${phrase}`);
  assert.ok(hmac.includes('TestPaymentHMACBindsProviderAccountTimestampAndBody'), 'HMAC domain-separation test missing');
  assert.ok(config.includes('MENDER_PAYMENT_MODE') && config.includes('live'), 'live-mode config rejection test missing');
  assert.ok(app.indexOf('VerifyPaymentCallback') < app.indexOf('decodePaymentCallback') && app.indexOf('VerifyPaymentCallback') < app.indexOf('IngestPaymentCallback'), 'callback verification no longer precedes decode/persistence');
  assert.ok(api.includes('/api/payment-callbacks/v1/providers/:provider_id/accounts/:provider_account_id') && api.includes('X-Mender-Payment-Signature'), 'callback HTTP boundary missing');
  assert.ok(!api.includes('provider_transaction_id":') && !api.includes('body_sha256') && !api.includes('key_id'), 'Admin payment projection leaks callback internals');
  const doc = readFileSync(localFile(e.document), 'utf8');
  for (const phrase of ['sandbox boundary','浏览器 redirect','不会创建新的 billing journal','不是 production payment readiness','S4-01..S4-18']) assert.ok(doc.includes(phrase), `S4-06 boundary document missing ${phrase}`);
  return e;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const e = checkS406SandboxPayment();
  console.log(`PASS: Mender canonical ${e.work_package} sandbox payment boundary is ${e.status.toUpperCase()}.`);
  console.log('LIMIT: live PSP, merchant credentials, real-money funding, FX, tax, payouts, billing frontend and production deploy remain deferred.');
}
