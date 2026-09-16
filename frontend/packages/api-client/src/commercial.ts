// Fixed, reviewed HTTP surfaces only. This client cannot choose an arbitrary endpoint,
// supply actor/state fields, forward credentials, or turn a redirect into payment truth.
import { MenderApiError } from './runs.ts';

export type CommercialJSON = null | boolean | number | string | CommercialJSON[] | { [key: string]: CommercialJSON };
type Check = (value: unknown) => CommercialJSON;
type Shape = Record<string, Check>;
const invalid = (): never => { throw new Error('服务合同不匹配或包含未允许的字段，请刷新后重试。'); };
const str = (max = 4000, min = 0): Check => (v) => typeof v === 'string' && v.length >= min && v.length <= max && !v.includes('\0') ? v : invalid();
const text = str();
const id: Check = (v) => typeof v === 'string' && /^[A-Za-z0-9_-]{1,128}$/.test(v) ? v : invalid();
const plugin: Check = (v) => typeof v === 'string' && /^[a-z][a-z0-9.-]{2,127}$/.test(v) ? v : invalid();
const version: Check = (v) => typeof v === 'string' && v.length <= 128 && /^\d+\.\d+\.\d+(?:-[A-Za-z0-9.-]+)?$/.test(v) ? v : invalid();
const bool: Check = (v) => typeof v === 'boolean' ? v : invalid();
const literal = (expected: CommercialJSON): Check => (v) => v === expected ? v as CommercialJSON : invalid();
const choice = (...values: string[]): Check => (v) => typeof v === 'string' && values.includes(v) ? v : invalid();
const nullable = (check: Check): Check => (v) => v === null ? null : check(v);
const integer = (signed = false, positive = false): Check => (v) => {
  if (typeof v !== 'string' || !(signed ? /^-?(0|[1-9]\d*)$/ : /^(0|[1-9]\d*)$/).test(v) || v === '-0' || v.length > 20) return invalid();
  const n = BigInt(v);
  return n >= (signed ? -9223372036854775808n : positive ? 1n : 0n) && n <= 9223372036854775807n ? v : invalid();
};
const micro = integer(false, true), decimal = integer(), signed = integer(true);
const currency: Check = (v) => typeof v === 'string' && /^[A-Z]{3}$/.test(v) ? v : invalid();
const time: Check = (v) => typeof v === 'string' && v.length <= 40 && /^\d{4}-\d{2}-\d{2}T/.test(v) && Number.isFinite(Date.parse(v)) ? v : invalid();
const hash: Check = (v) => typeof v === 'string' && /^[a-f0-9]{64}$/.test(v) ? v : invalid();
function object(shape: Shape): Check {
  return (v) => {
    if (!v || typeof v !== 'object' || Array.isArray(v)) return invalid();
    const record = v as Record<string, unknown>;
    if (Object.keys(record).length !== Object.keys(shape).length || Object.keys(record).some((key) => !Object.hasOwn(shape, key))) return invalid();
    return Object.fromEntries(Object.entries(shape).map(([key, check]) => [key, check(record[key])]));
  };
}
const list = (check: Check, max = 500): Check => (v) => Array.isArray(v) && v.length <= max ? v.map(check) : invalid();
const capability: Check = (v) => {
  if (!v || typeof v !== 'object') return invalid();
  const kind = (v as Record<string, unknown>).kind;
  return kind === 'agent' ? object({ kind: literal('agent'), deployment_revision: id })(v)
    : object({ kind: choice('api_tool', 'mcp_tool'), tool_version_id: id })(v);
};
const manifestShape = { apiVersion: literal('mender.io/plugin/v1alpha1'), plugin_id: plugin, version, publisher_id: id, display_name: str(200, 1), description: text, capabilities: list(capability, 64) };
const manifest: Check = (v) => {
  const result = object(manifestShape)(v) as Record<string, CommercialJSON>;
  const caps = result.capabilities as CommercialJSON[];
  if (!caps.length || new Set(caps.map((cap) => JSON.stringify(cap))).size !== caps.length) return invalid();
  return result;
};
const created = { created_at: time, updated_at: time };
const publisher = object({ publisher_id: id, display_name: str(200, 1), state: choice('active', 'frozen', 'disabled'), owned: bool, ...created });
const pluginRecord = object({ plugin_id: plugin, publisher_id: id, created_at: time });
const pluginVersion = object({ plugin_id: plugin, publisher_id: id, version, revision: decimal, state: choice('draft', 'submitted', 'approved', 'published', 'deprecated', 'disabled'), manifest, manifest_sha256: hash, ...created,
  submitted_at: nullable(time), approved_at: nullable(time), published_at: nullable(time), deprecated_at: nullable(time), disabled_at: nullable(time) });
const approvalState = choice('pending', 'approved', 'rejected', 'consumed', 'expired');
const review = { requester_user_id: id, state: approvalState, requested_at: time, expires_at: time, reviewer_user_id: nullable(id), reviewed_at: nullable(time), decision_note: text, consumed_at: nullable(time) };
const pluginApproval = object({ id, plugin_id: plugin, version, target_revision: decimal, ...review });
// Only the four reviewed parameter shapes may reach the approval UI. The reviewer
// must see the actual binding as well as its hash; arbitrary metadata is rejected.
const approvalParameters: Check = (v) => {
  if (!v || typeof v !== 'object' || Array.isArray(v)) return invalid();
  if (Object.hasOwn(v, 'mode')) return object({ mode: literal('emergency_disable') })(v);
  if (Object.hasOwn(v, 'scopes')) return object({ scopes: list(choice('workspace:read', 'run:read', 'usage:read'), 3), ttl_seconds: (n) => typeof n === 'number' && Number.isSafeInteger(n) && n >= 300 && n <= 3600 ? n : invalid() })(v);
  return object({ basis_id: id, basis_kind: choice('usage_settlement', 'run', 'incident', 'reconciliation'), business_key: str(128, 1), direction: choice('credit', 'debit') })(v);
};
const dangerousShape = { id, ...review, subject_kind: choice('workspace_member', 'platform_staff'), subject_id: id,
  action: choice('release.emergency_disable', 'support.workspace_read', 'commerce.refund', 'commerce.adjustment'), target_kind: str(64, 1), target_id: str(256, 1), target_version: str(256), parameters: approvalParameters,
  parameters_sha256: hash, amount_micro: nullable(micro), currency: nullable(currency), reason: str(1000, 1) };
const dangerous: Check = (v) => {
  const result = object(dangerousShape)(v) as Record<string, CommercialJSON>;
  const parameters = result.parameters as Record<string, CommercialJSON>;
  if (result.action === 'release.emergency_disable' && parameters.mode !== 'emergency_disable'
    || result.action === 'support.workspace_read' && !Array.isArray(parameters.scopes)
    || String(result.action).startsWith('commerce.') && !parameters.business_key) return invalid();
  return result;
};
const grant = object({ id, approval_id: id, user_id: id, scopes: list(choice('workspace:read', 'run:read', 'usage:read'), 3), reason: text, created_at: time, expires_at: time, revoked_at: nullable(time) });
const workspace = object({ workspace_id: id, frozen: bool, revision: decimal, reason: text, actor_user_id: str(128), ...created });
const provider = object({ provider_id: id, state: choice('active', 'quarantined'), revision: decimal, reason: text, actor_user_id: str(128), updated_at: time, deployment_count: decimal, active_deployment_count: decimal });
const incident = object({ id, target_kind: str(64, 1), target_id: id, severity: str(32, 1), code: str(128, 1), state: choice('open', 'resolved'), opened_by_user_id: id, open_reason: text, resolved_by_user_id: nullable(id), resolution: text, revision: decimal, opened_at: time, updated_at: time, resolved_at: nullable(time) });
const audit = object({ sequence: decimal, event_kind: str(128, 1), target_kind: str(64, 1), target_id: id, target_revision: decimal, actor_user_id: id, reason: text, occurred_at: time });
const run = object({ id, state: str(40, 1), version: decimal, ...created });
const plan = object({ release_id: id, plugin_id: plugin, plugin_version: version, toolset_version_id: id, tool_version_id: id, provider_id: id, stable_deployment_revision: id, candidate_deployment_revision: id, revision: decimal,
  state: choice('draft', 'canary', 'active', 'draining', 'rolled_back', 'disabled'), ...created, canary_started_at: nullable(time), observation_until: nullable(time), activated_at: nullable(time), draining_at: nullable(time), rolled_back_at: nullable(time), disabled_at: nullable(time) });
const route = object({ toolset_version_id: id, tool_version_id: id, stable_deployment_revision: id, candidate_deployment_revision: nullable(id), mode: choice('stable', 'canary', 'draining', 'disabled'), release_plan_id: nullable(id), revision: decimal, updated_at: time });
const releaseEvent = object({ sequence: decimal, release_plan_id: id, plan_revision: decimal, route_revision: decimal, event_kind: str(128, 1), route_mode: str(64, 1), selected_deployment_revision: nullable(id), reason: text, occurred_at: time });
const billingSummary = object({ workspace_id: id, currency, charged_micro: decimal, refunded_micro: decimal, adjustment_debit_micro: decimal, adjustment_credit_micro: decimal, net_billed_micro: signed, journal_count: decimal, latest_journal_at: nullable(time) });
const billingReconciliation = object({ workspace_id: id, currency, usage_settlement_charged_micro: decimal, ledger_charge_micro: decimal, missing_charge_journal_count: decimal, pending_reconcile_count: decimal, difference_micro: signed });
const intent = object({ id, business_key: str(128, 1), purpose: choice('collect_charge', 'execute_refund'), billing_journal_id: id, currency, amount_micro: micro, state: choice('pending', 'settled', 'failed'), revision: decimal, ...created, settled_at: nullable(time), failed_at: nullable(time) });
const callback = object({ receipt_id: id, event_id: str(128, 1), intent_id: id, event_type: str(64, 1), currency, amount_micro: micro, event_state: str(64, 1), disposition: choice('accepted', 'duplicate', 'quarantined'), reason_code: nullable(str(128)), occurred_at: time, received_at: time, delivery_count: decimal });
const paymentReconciliation = object({ workspace_id: id, provider_id: id, provider_account_id: id, currency, expected_collection_micro: decimal, settled_collection_micro: decimal, expected_refund_micro: decimal, settled_refund_micro: decimal, pending_intent_count: decimal, quarantined_event_count: decimal, collection_difference_micro: signed, refund_difference_micro: signed });

interface Operation { method: 'GET' | 'POST' | 'PUT'; path: string; params: Shape; body?: Shape; result: Check; meta?: Check; noBody?: boolean }
const ws = { workspace_id: id }, pluginPath = { ...ws, plugin_id: plugin, version };
const pbase = '/api/console/v1/workspaces/:workspace_id/publisher';
const admin = '/api/admin/v1/workspaces/:workspace_id';
const support = '/api/admin/v1/support/workspaces/:workspace_id';
const platform = '/api/admin/v1/platform';
const reason = { reason: str(1000, 1) }, note = { note: str(1000, 1) };
const seconds = (min: number, max: number): Check => (v) => typeof v === 'number' && Number.isSafeInteger(v) && v >= min && v <= max ? v : invalid();
const business = { business_key: str(128, 1), basis_kind: choice('usage_settlement', 'run', 'incident', 'reconciliation'), basis_id: id, direction: choice('debit', 'credit'), amount_micro: micro, currency, ...reason };
const paymentParams = { ...ws, provider_id: id, provider_account_id: id };
const sandboxMeta = object({ mode: literal('sandbox') });
export const commercialOperations: Record<string, Operation> = {
  'publisher.snapshot': { method: 'GET', path: pbase, params: ws, result: object({ publishers: list(publisher), plugins: list(pluginRecord), plugin_versions: list(pluginVersion) }) },
  'publisher.create': { method: 'POST', path: `${pbase}/publishers`, params: ws, body: { publisher_id: id, display_name: str(200, 1) }, result: publisher },
  'publisher.rename': { method: 'PUT', path: `${pbase}/publishers/:publisher_id`, params: { ...ws, publisher_id: id }, body: { display_name: str(200, 1) }, result: publisher },
  'plugin.create': { method: 'POST', path: `${pbase}/publishers/:publisher_id/plugins`, params: { ...ws, publisher_id: id }, body: { plugin_id: plugin }, result: pluginRecord },
  'version.create': { method: 'POST', path: `${pbase}/plugins/:plugin_id/versions`, params: { ...ws, plugin_id: plugin }, body: manifestShape, result: pluginVersion },
  'version.update': { method: 'PUT', path: `${pbase}/plugins/:plugin_id/versions/:version`, params: pluginPath, body: manifestShape, result: pluginVersion },
  'version.preflight': { method: 'POST', path: `${pbase}/plugins/:plugin_id/versions/:version/preflight`, params: pluginPath, noBody: true, result: object({ ready: bool, issues: list(object({ code: str(128, 1), target_id: str(256) })) }) },
  'version.submit': { method: 'POST', path: `${pbase}/plugins/:plugin_id/versions/:version/submit`, params: pluginPath, noBody: true, result: object({ approval_id: id, plugin_version: pluginVersion }) },
  'version.publish': { method: 'POST', path: `${pbase}/plugins/:plugin_id/versions/:version/publish`, params: pluginPath, noBody: true, result: object({ approval_id: id, plugin_version: pluginVersion }) },
  'plugin.reviews': { method: 'GET', path: `${admin}/plugin-publication-approvals`, params: ws, result: list(pluginApproval) },
  'release.snapshot': { method: 'GET', path: `${admin}/releases`, params: ws, result: object({ plans: list(plan), routes: list(route), events: list(releaseEvent) }) },
  'release.create': { method: 'POST', path: `${admin}/releases/plans`, params: ws, body: { plugin_id: plugin, plugin_version: version, toolset_version_id: id, tool_version_id: id, provider_id: id, stable_deployment_revision: id, candidate_deployment_revision: id, ...reason }, result: plan },
  'release.canary': { method: 'POST', path: `${admin}/releases/plans/:release_id/canary`, params: { ...ws, release_id: id }, body: { ...reason, observation_seconds: seconds(60, 86400) }, result: plan },
  'release.emergency-disable': { method: 'POST', path: `${admin}/releases/plans/:release_id/emergency-disable`, params: { ...ws, release_id: id }, body: { approval_id: id, ...reason }, result: plan },
  'platform.workspaces': { method: 'GET', path: `${platform}/workspaces`, params: {}, result: list(workspace) },
  'platform.providers': { method: 'GET', path: `${platform}/providers`, params: {}, result: list(provider) },
  'platform.incidents': { method: 'GET', path: `${platform}/incidents`, params: {}, result: list(incident, 100) },
  'platform.audit': { method: 'GET', path: `${platform}/audit`, params: {}, result: list(audit, 500) },
  'incident.create': { method: 'POST', path: `${platform}/incidents`, params: {}, body: { target_kind: choice('workspace', 'provider'), target_id: id, severity: choice('info', 'warning', 'critical'), code: str(128, 1), ...reason }, result: incident },
  'incident.resolve': { method: 'POST', path: `${platform}/incidents/:incident_id/resolve`, params: { incident_id: id }, body: { expected_revision: decimal, resolution: str(1000, 1) }, result: incident },
  'approval.list': { method: 'GET', path: `${admin}/dangerous-operations`, params: ws, result: list(dangerous) },
  'approval.get': { method: 'GET', path: `${admin}/dangerous-operations/:approval_id`, params: { ...ws, approval_id: id }, result: dangerous },
  'approval.emergency': { method: 'POST', path: `${admin}/dangerous-operations/release-emergency-requests`, params: ws, body: { release_id: id, ttl_seconds: seconds(60, 1800), ...reason }, result: dangerous },
  'approval.commerce': { method: 'POST', path: `${admin}/dangerous-operations/commerce-requests`, params: ws, body: { action: choice('commerce.refund', 'commerce.adjustment'), ...business, ttl_seconds: seconds(60, 1800) }, result: dangerous },
  'support.request': { method: 'POST', path: `${support}/jit-requests`, params: ws, body: { scopes: list(choice('workspace:read', 'run:read', 'usage:read'), 3), ttl_seconds: seconds(300, 3600), ...reason }, result: dangerous },
  'support.activate': { method: 'POST', path: `${support}/jit-requests/:approval_id/activate`, params: { ...ws, approval_id: id }, body: {}, result: grant },
  'support.revoke': { method: 'POST', path: `${support}/jit-grants/:grant_id/revoke`, params: { ...ws, grant_id: id }, body: reason, result: grant },
  'support.runs': { method: 'GET', path: `${support}/runs`, params: ws, result: list(run, 100) },
  'billing.summary': { method: 'GET', path: `${admin}/billing/summary?currency=:currency`, params: { ...ws, currency }, result: billingSummary },
  'billing.reconciliation': { method: 'GET', path: `${admin}/billing/reconciliation?currency=:currency`, params: { ...ws, currency }, result: billingReconciliation },
  'billing.refund': { method: 'POST', path: `${admin}/billing/refunds`, params: ws, body: { ...business, basis_kind: literal('usage_settlement'), direction: literal('credit'), approval_id: id }, result: object({ journal_id: id, replay: bool }) },
  'billing.adjustment': { method: 'POST', path: `${admin}/billing/adjustments`, params: ws, body: { ...business, basis_kind: choice('run', 'incident', 'reconciliation'), approval_id: id }, result: object({ journal_id: id, replay: bool }) },
  'payment.create': { method: 'POST', path: `${admin}/payments/intents`, params: ws, body: { business_key: str(128, 1), provider_id: id, provider_account_id: id, mode: literal('sandbox'), purpose: choice('collect_charge', 'execute_refund'), billing_journal_id: id, currency, amount_micro: micro }, result: object({ intent_id: id, replay: bool, mode: literal('sandbox') }) },
  'payment.intents': { method: 'GET', path: `${admin}/payments/intents?provider_id=:provider_id&provider_account_id=:provider_account_id`, params: paymentParams, result: list(intent), meta: sandboxMeta },
  'payment.callbacks': { method: 'GET', path: `${admin}/payments/callbacks?provider_id=:provider_id&provider_account_id=:provider_account_id`, params: paymentParams, result: list(callback), meta: sandboxMeta },
  'payment.reconciliation': { method: 'GET', path: `${admin}/payments/reconciliation?provider_id=:provider_id&provider_account_id=:provider_account_id&currency=:currency`, params: { ...paymentParams, currency }, result: paymentReconciliation, meta: object({ mode: literal('sandbox'), live_payments_enabled: literal(false) }) },
};
for (const action of ['approve', 'reject']) {
  commercialOperations[`plugin.${action}`] = { method: 'POST', path: `${admin}/plugin-publication-approvals/:approval_id/${action}`, params: { ...ws, approval_id: id }, body: note, result: pluginApproval };
  commercialOperations[`approval.${action}`] = { method: 'POST', path: `${admin}/dangerous-operations/:approval_id/${action}`, params: { ...ws, approval_id: id }, body: note, result: dangerous };
  commercialOperations[`support.${action}`] = { method: 'POST', path: `${support}/jit-requests/:approval_id/${action}`, params: { ...ws, approval_id: id }, body: note, result: dangerous };
}
for (const action of ['promote', 'drain', 'rollback']) commercialOperations[`release.${action}`] = { method: 'POST', path: `${admin}/releases/plans/:release_id/${action}`, params: { ...ws, release_id: id }, body: reason, result: plan };
for (const action of ['freeze', 'unfreeze']) commercialOperations[`workspace.${action}`] = { method: 'POST', path: `${platform}/workspaces/:workspace_id/${action}`, params: ws, body: { expected_revision: decimal, ...reason }, result: workspace };
for (const action of ['quarantine', 'restore']) commercialOperations[`provider.${action}`] = { method: 'POST', path: `${platform}/providers/:provider_id/${action}`, params: { provider_id: id }, body: { expected_revision: decimal, ...reason }, result: provider };

async function boundedJSON(response: Response, signal: AbortSignal) {
  if (!response.headers.get('content-type')?.includes('application/json') || !response.body) return invalid();
  const reader = response.body.getReader();
  let bytes = 0;
  const chunks: Uint8Array[] = [];
  try {
    for (;;) {
      signal.throwIfAborted();
      const next = await reader.read();
      if (next.done) break;
      bytes += next.value.byteLength;
      if (bytes > 2 * 1024 * 1024) return invalid();
      chunks.push(next.value);
    }
    const buffer = new Uint8Array(bytes); let offset = 0;
    for (const chunk of chunks) { buffer.set(chunk, offset); offset += chunk.length; }
    return JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(buffer)) as unknown;
  } finally { await reader.cancel().catch(() => undefined); }
}

export function createCommercialClient(fetcher: typeof fetch = fetch) {
  return {
    async call(operation: string, input: Record<string, CommercialJSON>, csrf = '', signal?: AbortSignal): Promise<CommercialJSON> {
      if (!Object.hasOwn(commercialOperations, operation)) throw new Error('未开放的操作');
      const def = commercialOperations[operation]!;
      const keys = { ...def.params, ...def.body };
      const clean = object(keys)(input) as Record<string, CommercialJSON>;
      if (operation === 'version.create' || operation === 'version.update') {
        manifest(Object.fromEntries(Object.keys(manifestShape).map((key) => [key, clean[key]])));
      }
      const mutation = def.method !== 'GET';
      if (mutation && (!csrf || csrf.length > 256 || /[\r\n]/.test(csrf))) throw new Error('CSRF token unavailable，请重新登录。');
      const url = def.path.replace(/:([a-z_]+)/g, (_, key: string) => encodeURIComponent(String(clean[key])));
      const requestSignal = signal ? AbortSignal.any([signal, AbortSignal.timeout(10_000)]) : AbortSignal.timeout(10_000);
      const body = def.body ? JSON.stringify(Object.fromEntries(Object.keys(def.body).map((key) => [key, clean[key]]))) : undefined;
      if (body && new TextEncoder().encode(body).length > (operation.startsWith('version.') ? 1024 * 1024 : 8192)) return invalid();
      const response = await fetcher(url, { method: def.method, body: def.noBody ? undefined : body, signal: requestSignal,
        credentials: 'same-origin', redirect: 'error', referrerPolicy: 'no-referrer', cache: 'no-store',
        headers: { Accept: 'application/json', ...(body ? { 'Content-Type': 'application/json' } : {}), ...(mutation ? { 'X-Mender-CSRF': csrf } : {}) } });
      if (!response.ok) {
        await response.body?.cancel();
        const messages: Record<number, string> = { 401: '登录已过期，请重新登录。', 403: '无操作权限、自审或 CSRF 校验失败。', 404: '对象不存在或该能力未启用。', 409: '版本、审批绑定或业务事实已变化；请刷新并重新核对，不要盲目重试。' };
        throw new MenderApiError(response.status, 'COMMERCIAL_HTTP_ERROR', messages[response.status] ?? `请求失败（HTTP ${response.status}），状态以服务端事实为准。`);
      }
      const envelope = object({ data: def.result, ...(def.meta ? { meta: def.meta } : {}) })(await boundedJSON(response, requestSignal)) as Record<string, CommercialJSON>;
      return envelope.data!;
    },
  };
}
