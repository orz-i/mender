import { MenderApiError } from './runs.ts';

export interface ConsoleSessionRecord { userId: string }
export interface ConsoleWorkspaceRecord { workspaceId: string; role: 'owner' | 'admin' | 'developer' | 'viewer' }

function object(value: unknown): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error('服务返回了无法识别的响应');
  return value as Record<string, unknown>;
}

function string(value: unknown) {
  if (typeof value !== 'string' || value.length === 0) throw new Error('服务返回了无法识别的响应');
  return value;
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

async function request(fetcher: typeof fetch, url: string, init: RequestInit = {}) {
  const timeout = AbortSignal.timeout(5_000);
  const signal = init.signal ? AbortSignal.any([init.signal, timeout]) : timeout;
  const response = await fetcher(url, {
    ...init,
    signal,
    cache: 'no-store',
    credentials: 'same-origin',
    referrerPolicy: 'same-origin',
    headers: { Accept: 'application/json', ...init.headers },
  });
  if (!response.ok) throw await errorFrom(response);
  return response;
}

export function createConsoleIdentityClient(baseUrl = '', fetcher: typeof fetch = fetch) {
  const base = baseUrl.replace(/\/$/, '');
  return {
    async getSession(signal?: AbortSignal): Promise<ConsoleSessionRecord> {
      const response = await request(fetcher, `${base}/api/console/v1/session`, { signal });
      const raw = object(await response.json());
      const data = object(raw.data);
      return { userId: string(data.user_id) };
    },
    async listWorkspaces(signal?: AbortSignal): Promise<ConsoleWorkspaceRecord[]> {
      const response = await request(fetcher, `${base}/api/console/v1/workspaces`, { signal });
      const raw = object(await response.json());
      if (!Array.isArray(raw.data)) throw new Error('服务返回了无法识别的 Workspace 列表');
      return raw.data.map((value) => {
        const item = object(value);
        const role = string(item.role);
        if (!['owner', 'admin', 'developer', 'viewer'].includes(role)) throw new Error('服务返回了无法识别的 Workspace 权限');
        return { workspaceId: string(item.workspace_id), role: role as ConsoleWorkspaceRecord['role'] };
      });
    },
    async logout(csrfToken: string, signal?: AbortSignal): Promise<void> {
      if (!csrfToken || csrfToken.length > 256) throw new Error('CSRF token unavailable');
      await request(fetcher, `${base}/api/console/v1/session`, { method: 'DELETE', signal, headers: { 'X-Mender-CSRF': csrfToken } });
    },
  };
}
