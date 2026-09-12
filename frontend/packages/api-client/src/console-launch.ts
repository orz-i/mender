import { MenderApiError } from './runs.ts';

export interface ConsoleLaunchOption {
  toolsetVersionId: string;
  toolId: string;
  toolVersion: string;
  toolVersionId: string;
  title: string;
  description: string;
  inputSchema: Record<string, unknown>;
  sideEffect: 'read_only' | 'write';
  idempotency: 'safe_read' | 'idempotent' | 'unsafe';
  connectionId: string;
  providerId: string;
  currency: string;
  reserveMicro: string;
}

function object(value: unknown): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error('服务返回了无法识别的启动选项');
  return value as Record<string, unknown>;
}

function string(value: unknown) {
  if (typeof value !== 'string' || value.length === 0) throw new Error('服务返回了无法识别的启动选项');
  return value;
}

function strictMicro(value: unknown) {
  const raw = string(value);
  if (!/^(0|[1-9][0-9]{0,18})$/.test(raw)) throw new Error('服务返回了无效的费用上限');
  return raw;
}

function parse(value: unknown): ConsoleLaunchOption {
  const raw = object(value);
  for (const forbidden of ['credential_version_ref', 'secret', 'token', 'budget_id', 'period_id', 'remaining_micro']) {
    if (forbidden in raw) throw new Error('服务返回了不应暴露的启动数据');
  }
  const schema = object(raw.input_schema);
  if (schema.type !== 'object') throw new Error('服务返回了无效的 Tool 输入 Schema');
  const sideEffect = string(raw.side_effect);
  const idempotency = string(raw.idempotency);
  if (sideEffect !== 'read_only' && sideEffect !== 'write') throw new Error('服务返回了未知副作用级别');
  if (!['safe_read', 'idempotent', 'unsafe'].includes(idempotency)) throw new Error('服务返回了未知幂等策略');
  const currency = string(raw.currency);
  if (!/^[A-Z]{3}$/.test(currency)) throw new Error('服务返回了无效币种');
  return {
    toolsetVersionId: string(raw.toolset_version_id), toolId: string(raw.tool_id), toolVersion: string(raw.tool_version), toolVersionId: string(raw.tool_version_id),
    title: string(raw.title), description: typeof raw.description === 'string' ? raw.description : '', inputSchema: schema,
    sideEffect, idempotency: idempotency as ConsoleLaunchOption['idempotency'], connectionId: string(raw.connection_id), providerId: string(raw.provider_id),
    currency, reserveMicro: strictMicro(raw.reserve_micro),
  };
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

export function createConsoleLaunchClient(baseUrl = '', fetcher: typeof fetch = fetch) {
  const base = baseUrl.replace(/\/$/, '');
  return {
    async list(workspaceId: string, signal?: AbortSignal): Promise<ConsoleLaunchOption[]> {
      if (!workspaceId) throw new Error('Workspace ID is required');
      const timeout = AbortSignal.timeout(5_000);
      const response = await fetcher(`${base}/api/console/v1/workspaces/${encodeURIComponent(workspaceId)}/launch-options`, {
        method: 'GET', signal: signal ? AbortSignal.any([signal, timeout]) : timeout,
        cache: 'no-store', credentials: 'same-origin', referrerPolicy: 'same-origin', headers: { Accept: 'application/json' },
      });
      if (!response.ok) throw await errorFrom(response);
      const raw = object(await response.json());
      if (!Array.isArray(raw.data)) throw new Error('服务返回了无法识别的启动选项');
      return raw.data.map(parse);
    },
  };
}
