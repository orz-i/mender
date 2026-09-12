import { MenderApiError } from './runs.ts';

export type RunDelegationScope = 'run:read' | 'run:cancel';

export interface ConsoleRunDelegation {
  delegationId: string;
  workspaceId: string;
  token: string;
  scopes: RunDelegationScope[];
  expiresAt: string;
}

const idPattern = /^[A-Za-z0-9_-]{1,128}$/;

function object(value: unknown): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error('服务返回了无法识别的委托响应');
  return value as Record<string, unknown>;
}

function string(value: unknown) {
  if (typeof value !== 'string' || value.length === 0) throw new Error('服务返回了无法识别的委托响应');
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

function validateScopes(scopes: RunDelegationScope[]) {
  if (scopes.length < 1 || scopes.length > 2 || new Set(scopes).size !== scopes.length || scopes.some((scope) => scope !== 'run:read' && scope !== 'run:cancel')) {
    throw new Error('Run delegation scope 无效');
  }
}

async function request(fetcher: typeof fetch, url: string, csrf: string, init: RequestInit) {
  if (!csrf || csrf.length > 256) throw new Error('CSRF token unavailable');
  const timeout = AbortSignal.timeout(5_000);
  const signal = init.signal ? AbortSignal.any([init.signal, timeout]) : timeout;
  const headers = new Headers(init.headers);
  headers.set('Accept', 'application/json');
  headers.set('X-Mender-CSRF', csrf);
  const response = await fetcher(url, { ...init, signal, cache: 'no-store', credentials: 'same-origin', referrerPolicy: 'same-origin', headers });
  if (!response.ok) throw await errorFrom(response);
  return response;
}

export function createConsoleRunDelegationClient(baseUrl = '', fetcher: typeof fetch = fetch) {
  const base = baseUrl.replace(/\/$/, '');
  const delegationBase = (workspaceId: string) => {
    if (!idPattern.test(workspaceId)) throw new Error('Workspace ID 格式无效');
    return `${base}/api/console/v1/workspaces/${encodeURIComponent(workspaceId)}/run-delegations`;
  };
  return {
    async issue(workspaceId: string, scopes: RunDelegationScope[], csrf: string, signal?: AbortSignal): Promise<ConsoleRunDelegation> {
      validateScopes(scopes);
      const response = await request(fetcher, delegationBase(workspaceId), csrf, {
        method: 'POST', signal, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ scopes }),
      });
      const raw = object(await response.json());
      const data = object(raw.data);
      const delegationId = string(data.delegation_id);
      const returnedWorkspace = string(data.workspace_id);
      const token = string(data.token);
      const expiresAt = string(data.expires_at);
      if (!idPattern.test(delegationId) || returnedWorkspace !== workspaceId || token.length < 32 || token.length > 240 || /\s/.test(token) || !Array.isArray(data.scopes)) {
        throw new Error('服务返回了无法识别的委托响应');
      }
      const returnedScopes = data.scopes.map((value) => string(value) as RunDelegationScope);
      validateScopes(returnedScopes);
      if (returnedScopes.length !== scopes.length || returnedScopes.some((scope, index) => scope !== scopes[index])) throw new Error('服务返回了错误的委托范围');
      return { delegationId, workspaceId: returnedWorkspace, token, scopes: returnedScopes, expiresAt };
    },
    async revoke(workspaceId: string, delegationId: string, csrf: string, signal?: AbortSignal): Promise<void> {
      if (!idPattern.test(delegationId)) throw new Error('Delegation ID 格式无效');
      await request(fetcher, `${delegationBase(workspaceId)}/${encodeURIComponent(delegationId)}`, csrf, { method: 'DELETE', signal });
    },
  };
}
