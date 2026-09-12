export type RunExecutionState =
  | 'queued'
  | 'running'
  | 'waiting_input'
  | 'cancel_requested'
  | 'reconciling'
  | 'succeeded'
  | 'failed'
  | 'canceled'
  | 'timed_out';

export interface RunRecord {
  runId: string;
  workspaceId: string;
  executionState: RunExecutionState;
  version: string;
  createdAt: string;
  updatedAt: string;
}

export interface RunEventRecord {
  version: string;
  eventType: 'run.state_changed';
  executionState: RunExecutionState;
  occurredAt: string;
  subjectId: string;
  reason: string;
}

export interface ArtifactRecord {
  artifactId: string;
  kind: string;
  mediaType: string;
  sizeBytes: number;
  createdAt: string;
}

export interface Page<T> {
  items: T[];
  nextCursor: string | null;
  requestId: string;
}

export interface RunEventsPage extends Page<RunEventRecord> {
  throughVersion: string;
}

export class MenderApiError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = 'MenderApiError';
    this.status = status;
    this.code = code;
  }
}

interface Access {
  workspaceId: string;
  token: string;
}

interface ListRunsRequest extends Access {
  limit?: number;
  cursor?: string;
  state?: RunExecutionState;
  signal?: AbortSignal;
}

interface RunRequest extends Access {
  runId: string;
  signal?: AbortSignal;
}

interface CancelRunRequest extends RunRequest {
  reason: string;
}

const idPattern = /^[A-Za-z0-9_-]{1,128}$/;
const states = new Set<RunExecutionState>([
  'queued', 'running', 'waiting_input', 'cancel_requested', 'reconciling',
  'succeeded', 'failed', 'canceled', 'timed_out',
]);

function requireId(value: string, name: string) {
  if (!idPattern.test(value)) throw new Error(`${name} 格式无效`);
}

function requireAccess(access: Access) {
  requireId(access.workspaceId, 'Workspace ID');
  if (access.token.length < 1 || access.token.length > 240 || /\s/.test(access.token)) {
    throw new Error('Run 凭据格式无效');
  }
}

function object(value: unknown, message = '服务返回了无法识别的响应'): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error(message);
  return value as Record<string, unknown>;
}

function string(value: unknown, message = '服务返回了无法识别的响应') {
  if (typeof value !== 'string') throw new Error(message);
  return value;
}

function optionalCursor(value: unknown) {
  if (value === null || value === undefined) return null;
  return string(value);
}

function parseState(value: unknown): RunExecutionState {
  const state = string(value) as RunExecutionState;
  if (!states.has(state)) throw new Error('服务返回了未知的 Run 状态');
  return state;
}

function parseRun(value: unknown): RunRecord {
  const raw = object(value);
  return {
    runId: string(raw.run_id),
    workspaceId: string(raw.workspace_id),
    executionState: parseState(raw.execution_state),
    version: string(raw.version),
    createdAt: string(raw.created_at),
    updatedAt: string(raw.updated_at),
  };
}

export function createConsoleRunsClient(baseUrl = '', fetcher: typeof fetch = fetch) {
  return createRunsClient(baseUrl, fetcher, '/api/console/v1');
}

function parseEvent(value: unknown): RunEventRecord {
  const raw = object(value);
  if (raw.event_type !== 'run.state_changed') throw new Error('服务返回了未知的 Run 事件');
  return {
    version: string(raw.version),
    eventType: raw.event_type,
    executionState: parseState(raw.execution_state),
    occurredAt: string(raw.occurred_at),
    subjectId: string(raw.subject_id),
    reason: string(raw.reason),
  };
}

function parseArtifact(value: unknown): ArtifactRecord {
  const raw = object(value);
  if (typeof raw.size_bytes !== 'number' || !Number.isSafeInteger(raw.size_bytes) || raw.size_bytes < 0) {
    throw new Error('服务返回了无法识别的 Artifact');
  }
  return {
    artifactId: string(raw.artifact_id),
    kind: string(raw.kind),
    mediaType: string(raw.media_type),
    sizeBytes: raw.size_bytes,
    createdAt: string(raw.created_at),
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

function requestSignal(signal?: AbortSignal) {
  const timeout = AbortSignal.timeout(5_000);
  return signal ? AbortSignal.any([signal, timeout]) : timeout;
}

async function jsonRequest(fetcher: typeof fetch, url: string, access: Access, init: RequestInit = {}) {
  requireAccess(access);
  const headers = new Headers(init.headers);
  headers.set('Accept', 'application/json');
  headers.set('Authorization', `Bearer ${access.token}`);
  const response = await fetcher(url, {
    ...init,
    cache: 'no-store',
    credentials: 'omit',
    referrerPolicy: 'no-referrer',
    headers,
  });
  if (!response.ok) throw await errorFrom(response);
  return response.json() as Promise<unknown>;
}

export function createRunsClient(baseUrl = '', fetcher: typeof fetch = fetch, apiPrefix = '/api/v1') {
  const base = baseUrl.replace(/\/$/, '');
  if (!apiPrefix.startsWith('/') || apiPrefix.endsWith('/') || apiPrefix.includes('?') || apiPrefix.includes('#')) throw new Error('Run API prefix 无效');
  const runBase = (workspaceId: string) => `${base}${apiPrefix}/workspaces/${encodeURIComponent(workspaceId)}/runs`;
  return {
    async listRuns(request: ListRunsRequest): Promise<Page<RunRecord>> {
      requireAccess(request);
      const params = new URLSearchParams();
      const limit = request.limit ?? 20;
      if (!Number.isInteger(limit) || limit < 1 || limit > 100) throw new Error('Run 分页数量无效');
      params.set('limit', String(limit));
      if (request.cursor) params.set('cursor', request.cursor);
      if (request.state) {
        if (!states.has(request.state)) throw new Error('Run 状态筛选无效');
        params.set('state', request.state);
      }
      const raw = object(await jsonRequest(fetcher, `${runBase(request.workspaceId)}?${params}`, request, { signal: requestSignal(request.signal) }));
      if (!Array.isArray(raw.data)) throw new Error('服务返回了无法识别的 Run 列表');
      const meta = object(raw.meta);
      const items = raw.data.map(parseRun);
      if (items.some((item) => item.workspaceId !== request.workspaceId)) throw new Error('服务返回了错误 Workspace 的 Run');
      return { items, nextCursor: optionalCursor(meta.next_cursor), requestId: string(meta.request_id) };
    },
    async getRun(request: RunRequest): Promise<RunRecord> {
      requireId(request.runId, 'Run ID');
      const raw = object(await jsonRequest(fetcher, `${runBase(request.workspaceId)}/${encodeURIComponent(request.runId)}`, request, { signal: requestSignal(request.signal) }));
      const item = parseRun(raw.data);
      if (item.workspaceId !== request.workspaceId || item.runId !== request.runId) throw new Error('服务返回了错误的 Run');
      return item;
    },
    async listRunEvents(request: RunRequest): Promise<RunEventsPage> {
      requireId(request.runId, 'Run ID');
      const raw = object(await jsonRequest(fetcher, `${runBase(request.workspaceId)}/${encodeURIComponent(request.runId)}/events?limit=100`, request, { signal: requestSignal(request.signal) }));
      if (!Array.isArray(raw.data)) throw new Error('服务返回了无法识别的 Run 事件');
      const meta = object(raw.meta);
      return {
        items: raw.data.map(parseEvent), nextCursor: optionalCursor(meta.next_cursor), requestId: string(meta.request_id), throughVersion: string(meta.through_version),
      };
    },
    async listRunArtifacts(request: RunRequest): Promise<Page<ArtifactRecord>> {
      requireId(request.runId, 'Run ID');
      const raw = object(await jsonRequest(fetcher, `${runBase(request.workspaceId)}/${encodeURIComponent(request.runId)}/artifacts`, request, { signal: requestSignal(request.signal) }));
      if (!Array.isArray(raw.data)) throw new Error('服务返回了无法识别的 Artifact 列表');
      const meta = object(raw.meta);
      return { items: raw.data.map(parseArtifact), nextCursor: null, requestId: string(meta.request_id) };
    },
    async cancelRun(request: CancelRunRequest): Promise<RunRecord> {
      requireId(request.runId, 'Run ID');
      if (request.reason.length > 500) throw new Error('取消原因过长');
      const raw = object(await jsonRequest(fetcher, `${runBase(request.workspaceId)}/${encodeURIComponent(request.runId)}/cancel`, request, {
        method: 'POST', signal: requestSignal(request.signal), headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ reason: request.reason }),
      }));
      const item = parseRun(raw.data);
      if (item.workspaceId !== request.workspaceId || item.runId !== request.runId) throw new Error('服务返回了错误的 Run');
      return item;
    },
  };
}
