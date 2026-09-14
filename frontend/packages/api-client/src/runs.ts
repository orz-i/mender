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

function parseRunCost(value: unknown): RunCostRecord {
  const raw = object(value, '服务返回了无法识别的 Run quota cost');
  exactKeys(raw, [
    'run_id', 'budget_id', 'period_id', 'currency', 'quota_state', 'reserved_micro', 'charged_micro',
    'released_micro', 'outcome', 'created_at', 'finalized_at', 'accounting_scope',
  ], '服务返回了无法识别的 Run quota cost');
  const runId = string(raw.run_id);
  const budgetId = string(raw.budget_id);
  const periodId = string(raw.period_id);
  const currency = string(raw.currency);
  const quotaState = string(raw.quota_state) as RunQuotaState;
  const reservedMicro = micro(raw.reserved_micro);
  const releasedMicro = micro(raw.released_micro);
  const chargedMicro = raw.charged_micro === null ? null : micro(raw.charged_micro);
  const outcome = raw.outcome === null ? null : string(raw.outcome) as RunQuotaOutcome;
  const finalizedAt = raw.finalized_at === null ? null : string(raw.finalized_at);
  if (!idPattern.test(runId) || !idPattern.test(budgetId) || !idPattern.test(periodId) || !/^[A-Z]{3}$/.test(currency) ||
      !['held', 'released', 'settled'].includes(quotaState) || raw.accounting_scope !== 'quota_only') {
    throw new Error('服务返回了无法识别的 Run quota cost');
  }
  const reserved = BigInt(reservedMicro);
  const released = BigInt(releasedMicro);
  if (released > reserved) throw new Error('服务返回了不一致的 Run quota cost');
  if (quotaState === 'held') {
    if (chargedMicro !== null || outcome !== null || finalizedAt !== null || released !== 0n) throw new Error('服务返回了不一致的 Run quota cost');
  } else if (quotaState === 'released') {
    if (chargedMicro !== null || outcome !== null || finalizedAt === null || released !== reserved) throw new Error('服务返回了不一致的 Run quota cost');
  } else {
    if (chargedMicro === null || finalizedAt === null || !['succeeded', 'failed', 'canceled'].includes(outcome ?? '')) throw new Error('服务返回了不一致的 Run quota cost');
    const charged = BigInt(chargedMicro);
    if (charged > reserved || released !== reserved - charged) throw new Error('服务返回了不一致的 Run quota cost');
  }
  return {
    runId, budgetId, periodId, currency, quotaState, reservedMicro, chargedMicro, releasedMicro,
    outcome, createdAt: string(raw.created_at), finalizedAt, accountingScope: 'quota_only',
  };
}

function micro(value: unknown, message = '服务返回了无效的 micro amount') {
  const raw = string(value, message);
  if (!/^(0|[1-9][0-9]{0,18})$/.test(raw)) throw new Error(message);
  return raw;
}

function exactKeys(raw: Record<string, unknown>, allowed: string[], message: string) {
  const actual = Object.keys(raw).sort();
  const expected = [...allowed].sort();
  if (actual.length !== expected.length || actual.some((key, index) => key !== expected[index])) throw new Error(message);
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

export interface ArtifactDetailRecord extends ArtifactRecord {
  content: unknown;
}

export type RunQuotaState = 'held' | 'released' | 'settled';
export type RunQuotaOutcome = 'succeeded' | 'failed' | 'canceled';

export interface RunCostRecord {
  runId: string;
  budgetId: string;
  periodId: string;
  currency: string;
  quotaState: RunQuotaState;
  reservedMicro: string;
  chargedMicro: string | null;
  releasedMicro: string;
  outcome: RunQuotaOutcome | null;
  createdAt: string;
  finalizedAt: string | null;
  accountingScope: 'quota_only';
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

interface EventRequest extends RunRequest {
  limit?: number;
  cursor?: string;
  expectedThroughVersion?: string;
}

interface ArtifactRequest extends RunRequest {
  artifactId: string;
}

interface CancelRunRequest extends RunRequest {
  reason: string;
}

const idPattern = /^[A-Za-z0-9_-]{1,128}$/;
const artifactIdPattern = /^[A-Za-z0-9._:-]{1,160}$/;
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
  const cursor = string(value);
  if (cursor.length < 1 || cursor.length > 2048) throw new Error('服务返回了无效的分页游标');
  return cursor;
}

function revision(value: unknown, message = '服务返回了无效的 Run revision') {
  const raw = string(value, message);
  if (!/^[1-9][0-9]*$/.test(raw)) throw new Error(message);
  return BigInt(raw);
}

function parseState(value: unknown): RunExecutionState {
  const state = string(value) as RunExecutionState;
  if (!states.has(state)) throw new Error('服务返回了未知的 Run 状态');
  return state;
}

function parseRun(value: unknown): RunRecord {
  const raw = object(value);
  exactKeys(raw, ['run_id', 'workspace_id', 'execution_state', 'version', 'created_at', 'updated_at'], '服务返回了无法识别的 Run');
  const runId = string(raw.run_id);
  const workspaceId = string(raw.workspace_id);
  const version = string(raw.version);
  if (!idPattern.test(runId) || !idPattern.test(workspaceId)) throw new Error('服务返回了无法识别的 Run');
  revision(version, '服务返回了无效的 Run revision');
  return {
    runId,
    workspaceId,
    executionState: parseState(raw.execution_state),
    version,
    createdAt: string(raw.created_at),
    updatedAt: string(raw.updated_at),
  };
}

export function createConsoleRunsClient(baseUrl = '', fetcher: typeof fetch = fetch) {
  const client = createRunsClient(baseUrl, fetcher, '/api/console/v1');
  const base = baseUrl.replace(/\/$/, '');
  const runBase = (workspaceId: string) => `${base}/api/console/v1/workspaces/${encodeURIComponent(workspaceId)}/runs`;
  return {
    ...client,
    async getRunCost(request: RunRequest): Promise<RunCostRecord> {
      requireId(request.runId, 'Run ID');
      const raw = object(await jsonRequest(fetcher, `${runBase(request.workspaceId)}/${encodeURIComponent(request.runId)}/cost`, request, { signal: requestSignal(request.signal) }));
      const item = parseRunCost(raw.data);
      if (item.runId !== request.runId) throw new Error('服务返回了错误的 Run quota cost');
      return item;
    },
  };
}

function parseEvent(value: unknown): RunEventRecord {
  const raw = object(value);
  exactKeys(raw, ['version', 'event_type', 'execution_state', 'occurred_at', 'subject_id', 'reason'], '服务返回了无法识别的 Run 事件');
  if (raw.event_type !== 'run.state_changed') throw new Error('服务返回了未知的 Run 事件');
  revision(raw.version, '服务返回了无效的 Run event revision');
  return {
    version: string(raw.version),
    eventType: raw.event_type,
    executionState: parseState(raw.execution_state),
    occurredAt: string(raw.occurred_at),
    subjectId: string(raw.subject_id),
    reason: string(raw.reason),
  };
}

function parseArtifact(value: unknown, detail = false): ArtifactRecord {
  const raw = object(value);
  exactKeys(raw, detail
    ? ['artifact_id', 'kind', 'media_type', 'size_bytes', 'created_at', 'content']
    : ['artifact_id', 'kind', 'media_type', 'size_bytes', 'created_at'], '服务返回了无法识别的 Artifact');
  if (typeof raw.size_bytes !== 'number' || !Number.isSafeInteger(raw.size_bytes) || raw.size_bytes < 0) {
    throw new Error('服务返回了无法识别的 Artifact');
  }
  const artifactId = string(raw.artifact_id);
  const kind = string(raw.kind);
  const mediaType = string(raw.media_type);
  if (!artifactIdPattern.test(artifactId) || kind !== 'provider_result' || mediaType !== 'application/json' || raw.size_bytes > 1_048_576) {
    throw new Error('服务返回了无法识别的 Artifact');
  }
  return {
    artifactId,
    kind,
    mediaType,
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
    async listRunEvents(request: EventRequest): Promise<RunEventsPage> {
      requireId(request.runId, 'Run ID');
      const limit = request.limit ?? 20;
      if (!Number.isInteger(limit) || limit < 1 || limit > 100) throw new Error('Run Event 分页数量无效');
      if (request.cursor !== undefined && (request.cursor.length < 1 || request.cursor.length > 2048)) throw new Error('Run Event cursor 无效');
      const params = new URLSearchParams({ limit: String(limit) });
      if (request.cursor) params.set('cursor', request.cursor);
      const raw = object(await jsonRequest(fetcher, `${runBase(request.workspaceId)}/${encodeURIComponent(request.runId)}/events?${params}`, request, { signal: requestSignal(request.signal) }));
      if (!Array.isArray(raw.data)) throw new Error('服务返回了无法识别的 Run 事件');
      const meta = object(raw.meta);
      const throughVersion = string(meta.through_version);
      const through = revision(throughVersion, '服务返回了无效的 through_version');
      if (request.expectedThroughVersion !== undefined && request.expectedThroughVersion !== throughVersion) throw new Error('Run Event 分页快照发生漂移');
      const items = raw.data.map(parseEvent);
      let previous = 1n;
      for (const item of items) {
        const current = revision(item.version, '服务返回了无效的 Run event revision');
        if (current <= previous || current > through) throw new Error('Run Event 分页顺序或 watermark 无效');
        previous = current;
      }
      return {
        items, nextCursor: optionalCursor(meta.next_cursor), requestId: string(meta.request_id), throughVersion,
      };
    },
    async listRunArtifacts(request: RunRequest): Promise<Page<ArtifactRecord>> {
      requireId(request.runId, 'Run ID');
      const raw = object(await jsonRequest(fetcher, `${runBase(request.workspaceId)}/${encodeURIComponent(request.runId)}/artifacts`, request, { signal: requestSignal(request.signal) }));
      if (!Array.isArray(raw.data)) throw new Error('服务返回了无法识别的 Artifact 列表');
      const meta = object(raw.meta);
      return { items: raw.data.map((item) => parseArtifact(item)), nextCursor: null, requestId: string(meta.request_id) };
    },
    async getRunArtifact(request: ArtifactRequest): Promise<ArtifactDetailRecord> {
      requireId(request.runId, 'Run ID');
      if (!artifactIdPattern.test(request.artifactId)) throw new Error('Artifact ID 格式无效');
      const raw = object(await jsonRequest(fetcher, `${runBase(request.workspaceId)}/${encodeURIComponent(request.runId)}/artifacts/${encodeURIComponent(request.artifactId)}`, request, { signal: requestSignal(request.signal) }));
      const data = object(raw.data);
      const metadata = parseArtifact(data, true);
      if (metadata.artifactId !== request.artifactId) throw new Error('服务返回了错误的 Artifact');
      return { ...metadata, content: data.content };
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
