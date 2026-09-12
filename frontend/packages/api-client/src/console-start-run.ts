import { MenderApiError } from './runs.ts';

export interface ConsoleStartConstraint {
  toolsetVersionId: string;
  toolId: string;
  toolVersion: string;
  toolVersionId: string;
  connectionId: string;
  currency: string;
  maxChargeMicro: string;
  idempotencyKey: string;
}

export interface ConsoleStartDelegation extends ConsoleStartConstraint {
  delegationId: string;
  workspaceId: string;
  token: string;
  expiresAt: string;
}

export interface ConsoleStartResult {
  runId: string;
  replayed: boolean;
  requestId: string;
}

const idPattern = /^[A-Za-z0-9_-]{1,128}$/;
const versionPattern = /^[A-Za-z0-9][A-Za-z0-9._:+-]{0,127}$/;
const moneyPattern = /^(0|[1-9][0-9]{0,18})$/;
const idemPattern = /^[!-~]{8,128}$/;

function object(value: unknown, message = '服务返回了无法识别的启动响应'): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error(message);
  return value as Record<string, unknown>;
}

function string(value: unknown, message = '服务返回了无法识别的启动响应') {
  if (typeof value !== 'string' || value.length === 0) throw new Error(message);
  return value;
}

function requireConstraint(value: ConsoleStartConstraint) {
  if (!idPattern.test(value.toolsetVersionId) || !idPattern.test(value.toolId) || !versionPattern.test(value.toolVersion) || !idPattern.test(value.toolVersionId) || !idPattern.test(value.connectionId)) {
    throw new Error('启动目标格式无效');
  }
  if (!/^[A-Z]{3}$/.test(value.currency) || !moneyPattern.test(value.maxChargeMicro) || !idemPattern.test(value.idempotencyKey)) {
    throw new Error('启动费用或幂等标识无效');
  }
}

function requireCSRF(csrf: string) {
  if (!csrf || csrf.length > 256) throw new Error('CSRF token unavailable');
}

function requestSignal(signal?: AbortSignal) {
  const timeout = AbortSignal.timeout(5_000);
  return signal ? AbortSignal.any([signal, timeout]) : timeout;
}

async function errorFrom(response: Response) {
  try {
    const raw = object(await response.json());
    const error = object(raw.error);
    return new MenderApiError(response.status, string(error.code), string(error.message));
  } catch (error) {
    if (error instanceof MenderApiError) return error;
    return new MenderApiError(response.status, 'HTTP_ERROR', `Mender API 请求失败（HTTP ${response.status}）`);
  }
}

function delegationBase(base: string, workspaceId: string) {
  if (!idPattern.test(workspaceId)) throw new Error('Workspace ID 格式无效');
  return `${base}/api/console/v1/workspaces/${encodeURIComponent(workspaceId)}/run-start-delegations`;
}

export function createConsoleStartRunClient(baseUrl = '', fetcher: typeof fetch = fetch) {
  const base = baseUrl.replace(/\/$/, '');
  return {
    async issueDelegation(workspaceId: string, constraint: ConsoleStartConstraint, csrf: string, signal?: AbortSignal): Promise<ConsoleStartDelegation> {
      requireConstraint(constraint);
      requireCSRF(csrf);
      const response = await fetcher(delegationBase(base, workspaceId), {
        method: 'POST', signal: requestSignal(signal), cache: 'no-store', credentials: 'same-origin', referrerPolicy: 'same-origin',
        headers: { Accept: 'application/json', 'Content-Type': 'application/json', 'X-Mender-CSRF': csrf },
        body: JSON.stringify({
          toolset_version_id: constraint.toolsetVersionId, tool_id: constraint.toolId, tool_version: constraint.toolVersion, tool_version_id: constraint.toolVersionId,
          connection_id: constraint.connectionId, currency: constraint.currency, max_charge_micro: constraint.maxChargeMicro, idempotency_key: constraint.idempotencyKey,
        }),
      });
      if (!response.ok) throw await errorFrom(response);
      const raw = object(await response.json());
      const data = object(raw.data);
      const result: ConsoleStartDelegation = {
        delegationId: string(data.delegation_id), workspaceId: string(data.workspace_id), token: string(data.token), expiresAt: string(data.expires_at),
        toolsetVersionId: string(data.toolset_version_id), toolId: string(data.tool_id), toolVersion: string(data.tool_version), toolVersionId: string(data.tool_version_id),
        connectionId: string(data.connection_id), currency: string(data.currency), maxChargeMicro: string(data.max_charge_micro), idempotencyKey: string(data.idempotency_key),
      };
      if (!idPattern.test(result.delegationId) || result.workspaceId !== workspaceId || result.token.length < 32 || result.token.length > 240 || /\s/.test(result.token)) throw new Error('服务返回了无法识别的启动委托');
      for (const key of ['toolsetVersionId', 'toolId', 'toolVersion', 'toolVersionId', 'connectionId', 'currency', 'maxChargeMicro', 'idempotencyKey'] as const) {
        if (result[key] !== constraint[key]) throw new Error('服务返回了错误的启动委托范围');
      }
      return result;
    },
    async revokeDelegation(workspaceId: string, delegationId: string, csrf: string, signal?: AbortSignal): Promise<void> {
      if (!idPattern.test(delegationId)) throw new Error('Delegation ID 格式无效');
      requireCSRF(csrf);
      const response = await fetcher(`${delegationBase(base, workspaceId)}/${encodeURIComponent(delegationId)}`, {
        method: 'DELETE', signal: requestSignal(signal), cache: 'no-store', credentials: 'same-origin', referrerPolicy: 'same-origin',
        headers: { Accept: 'application/json', 'X-Mender-CSRF': csrf },
      });
      if (!response.ok) throw await errorFrom(response);
    },
    async startRun(workspaceId: string, delegation: ConsoleStartDelegation, argumentsValue: Record<string, unknown>, signal?: AbortSignal): Promise<ConsoleStartResult> {
      requireConstraint(delegation);
      if (delegation.workspaceId !== workspaceId || !idPattern.test(delegation.delegationId) || delegation.token.length < 32 || delegation.token.length > 240 || /\s/.test(delegation.token)) throw new Error('启动委托格式无效');
      if (typeof argumentsValue !== 'object' || argumentsValue === null || Array.isArray(argumentsValue)) throw new Error('Tool arguments 必须是 JSON object');
      const url = `${base}/api/console/v1/workspaces/${encodeURIComponent(workspaceId)}/runs`;
      const response = await fetcher(url, {
        method: 'POST', signal: requestSignal(signal), cache: 'no-store', credentials: 'omit', referrerPolicy: 'no-referrer',
        headers: { Accept: 'application/json', Authorization: `Bearer ${delegation.token}`, 'Content-Type': 'application/json', 'Idempotency-Key': delegation.idempotencyKey },
        body: JSON.stringify({
          tool_ref: { tool_id: delegation.toolId, version: delegation.toolVersion }, toolset_id: delegation.toolsetVersionId, connection_id: delegation.connectionId,
          arguments: argumentsValue, max_charge: { currency: delegation.currency, amount_micro: delegation.maxChargeMicro },
        }),
      });
      if (!response.ok) throw await errorFrom(response);
      if (response.status !== 200 && response.status !== 202) throw new Error('服务返回了无法识别的启动状态');
      const raw = object(await response.json());
      const data = object(raw.data);
      const meta = object(raw.meta);
      if (data.execution_state !== 'queued' || data.billing_state !== 'reserved') throw new Error('服务返回了无法识别的启动状态');
      const runId = string(data.run_id);
      if (!idPattern.test(runId)) throw new Error('服务返回了无法识别的 Run ID');
      return { runId, replayed: response.status === 200, requestId: string(meta.request_id) };
    },
  };
}
