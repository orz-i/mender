import { MenderApiError } from './runs.ts';

export type ConsoleConnectionState = 'active' | 'expired' | 'revoked' | 'error';

export interface ConsoleConnectionRecord {
  connectionId: string;
  providerId: string;
  state: ConsoleConnectionState;
  revision: number;
  createdAt: string;
  expiresAt: string;
}

function object(value: unknown): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error('服务返回了无法识别的 Connection 响应');
  return value as Record<string, unknown>;
}

function string(value: unknown) {
  if (typeof value !== 'string' || value.length === 0) throw new Error('服务返回了无法识别的 Connection 响应');
  return value;
}

function parse(value: unknown): ConsoleConnectionRecord {
  const raw = object(value);
  if ('credential_version_ref' in raw || 'secret' in raw || 'token' in raw) throw new Error('服务返回了不允许暴露的 Connection 字段');
  const state = string(raw.state);
  if (!['active', 'expired', 'revoked', 'error'].includes(state) || typeof raw.revision !== 'number' || !Number.isSafeInteger(raw.revision) || raw.revision < 1) {
    throw new Error('服务返回了无法识别的 Connection 响应');
  }
  return {
    connectionId: string(raw.connection_id), providerId: string(raw.provider_id), state: state as ConsoleConnectionState,
    revision: raw.revision, createdAt: string(raw.created_at), expiresAt: string(raw.expires_at),
  };
}

async function failure(response: Response) {
  try {
    const raw = object(await response.json());
    const error = object(raw.error);
    return new MenderApiError(response.status, string(error.code), string(error.message));
  } catch (error) {
    if (error instanceof MenderApiError) return error;
    return new MenderApiError(response.status, 'HTTP_ERROR', `Mender API 请求失败（HTTP ${response.status}）`);
  }
}

async function request(fetcher: typeof fetch, url: string, init: RequestInit = {}) {
  const timeout = AbortSignal.timeout(5_000);
  const signal = init.signal ? AbortSignal.any([init.signal, timeout]) : timeout;
  const response = await fetcher(url, {
    ...init, signal, cache: 'no-store', credentials: 'same-origin', referrerPolicy: 'same-origin',
    headers: { Accept: 'application/json', ...init.headers },
  });
  if (!response.ok) throw await failure(response);
  return response;
}

export function createConsoleConnectionsClient(baseUrl = '', fetcher: typeof fetch = fetch) {
  const base = baseUrl.replace(/\/$/, '');
  const path = (workspaceId: string) => `${base}/api/console/v1/workspaces/${encodeURIComponent(workspaceId)}/connections`;
  return {
    async list(workspaceId: string, signal?: AbortSignal): Promise<ConsoleConnectionRecord[]> {
      if (!workspaceId) throw new Error('Workspace ID is required');
      const response = await request(fetcher, path(workspaceId), { signal });
      const raw = object(await response.json());
      if (!Array.isArray(raw.data)) throw new Error('服务返回了无法识别的 Connection 列表');
      return raw.data.map(parse);
    },
    async revoke(workspaceId: string, connectionId: string, csrfToken: string, signal?: AbortSignal): Promise<ConsoleConnectionRecord> {
      if (!workspaceId || !connectionId || !csrfToken || csrfToken.length > 256) throw new Error('Connection revoke request is invalid');
      const response = await request(fetcher, `${path(workspaceId)}/${encodeURIComponent(connectionId)}`, {
        method: 'DELETE', signal, headers: { 'X-Mender-CSRF': csrfToken },
      });
      return parse(object(await response.json()).data);
    },
  };
}
