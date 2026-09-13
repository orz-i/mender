import { MenderApiError } from './runs.ts';

export type ConsoleQuotaState = 'held' | 'released' | 'settled';
export type ConsoleQuotaOutcome = 'succeeded' | 'failed' | 'canceled';

export interface ConsoleBudgetPeriod {
  budgetId: string;
  periodId: string;
  currency: string;
  startsAt: string;
  endsAt: string;
  active: boolean;
  limitMicro: string;
  consumedMicro: string;
  reservedMicro: string;
  availableMicro: string;
  revision: string;
}

export interface ConsoleUsageEntry {
  runId: string;
  budgetId: string;
  periodId: string;
  currency: string;
  quotaState: ConsoleQuotaState;
  reservedMicro: string;
  chargedMicro: string | null;
  releasedMicro: string;
  outcome: ConsoleQuotaOutcome | null;
  createdAt: string;
  finalizedAt: string | null;
}

export interface ConsoleUsageSnapshot {
  budgetPeriods: ConsoleBudgetPeriod[];
  usageEntries: ConsoleUsageEntry[];
}

const idPattern = /^[A-Za-z0-9_-]{1,128}$/;
const microPattern = /^(0|[1-9][0-9]{0,18})$/;
const revisionPattern = /^[1-9][0-9]*$/;

function object(value: unknown, message = '服务返回了无法识别的 Usage 数据'): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error(message);
  return value as Record<string, unknown>;
}

function string(value: unknown, message = '服务返回了无法识别的 Usage 数据') {
  if (typeof value !== 'string' || value.length === 0) throw new Error(message);
  return value;
}

function exactKeys(raw: Record<string, unknown>, expected: string[]) {
  const actual = Object.keys(raw).sort();
  const allowed = [...expected].sort();
  if (actual.length !== allowed.length || actual.some((key, index) => key !== allowed[index])) {
    throw new Error('服务返回了不应暴露或无法识别的 Usage 字段');
  }
}

function micro(value: unknown) {
  const raw = string(value, '服务返回了无效的 micro amount');
  if (!microPattern.test(raw)) throw new Error('服务返回了无效的 micro amount');
  return raw;
}

function timestamp(value: unknown) {
  const raw = string(value);
  if (!Number.isFinite(Date.parse(raw))) throw new Error('服务返回了无效的时间');
  return raw;
}

function parseBudget(value: unknown): ConsoleBudgetPeriod {
  const raw = object(value);
  exactKeys(raw, ['budget_id', 'period_id', 'currency', 'starts_at', 'ends_at', 'active', 'limit_micro', 'consumed_micro', 'reserved_micro', 'available_micro', 'revision']);
  const budgetId = string(raw.budget_id);
  const periodId = string(raw.period_id);
  const currency = string(raw.currency);
  const limitMicro = micro(raw.limit_micro);
  const consumedMicro = micro(raw.consumed_micro);
  const reservedMicro = micro(raw.reserved_micro);
  const availableMicro = micro(raw.available_micro);
  const revision = string(raw.revision);
  if (!idPattern.test(budgetId) || !idPattern.test(periodId) || !/^[A-Z]{3}$/.test(currency) || typeof raw.active !== 'boolean' || !revisionPattern.test(revision)) {
    throw new Error('服务返回了无法识别的 Budget period');
  }
  const limit = BigInt(limitMicro);
  const consumed = BigInt(consumedMicro);
  const reserved = BigInt(reservedMicro);
  const available = BigInt(availableMicro);
  if (consumed > limit || reserved > limit - consumed || available !== limit - consumed - reserved) {
    throw new Error('服务返回了不一致的 Budget amount');
  }
  return {
    budgetId, periodId, currency, startsAt: timestamp(raw.starts_at), endsAt: timestamp(raw.ends_at), active: raw.active,
    limitMicro, consumedMicro, reservedMicro, availableMicro, revision,
  };
}

function parseUsage(value: unknown): ConsoleUsageEntry {
  const raw = object(value);
  exactKeys(raw, ['run_id', 'budget_id', 'period_id', 'currency', 'quota_state', 'reserved_micro', 'charged_micro', 'released_micro', 'outcome', 'created_at', 'finalized_at']);
  const runId = string(raw.run_id);
  const budgetId = string(raw.budget_id);
  const periodId = string(raw.period_id);
  const currency = string(raw.currency);
  const quotaState = string(raw.quota_state) as ConsoleQuotaState;
  const reservedMicro = micro(raw.reserved_micro);
  const chargedMicro = raw.charged_micro === null ? null : micro(raw.charged_micro);
  const releasedMicro = micro(raw.released_micro);
  const outcome = raw.outcome === null ? null : string(raw.outcome) as ConsoleQuotaOutcome;
  const finalizedAt = raw.finalized_at === null ? null : timestamp(raw.finalized_at);
  if (!idPattern.test(runId) || !idPattern.test(budgetId) || !idPattern.test(periodId) || !/^[A-Z]{3}$/.test(currency) || !['held', 'released', 'settled'].includes(quotaState)) {
    throw new Error('服务返回了无法识别的 Usage entry');
  }
  const reserved = BigInt(reservedMicro);
  const released = BigInt(releasedMicro);
  if (quotaState === 'held') {
    if (chargedMicro !== null || outcome !== null || finalizedAt !== null || released !== 0n) throw new Error('服务返回了不一致的 Usage entry');
  } else if (quotaState === 'released') {
    if (chargedMicro !== null || outcome !== null || finalizedAt === null || released !== reserved) throw new Error('服务返回了不一致的 Usage entry');
  } else {
    if (chargedMicro === null || finalizedAt === null || !['succeeded', 'failed', 'canceled'].includes(outcome ?? '')) throw new Error('服务返回了不一致的 Usage entry');
    const charged = BigInt(chargedMicro);
    if (charged > reserved || released !== reserved - charged) throw new Error('服务返回了不一致的 Usage entry');
  }
  return { runId, budgetId, periodId, currency, quotaState, reservedMicro, chargedMicro, releasedMicro, outcome, createdAt: timestamp(raw.created_at), finalizedAt };
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

export function createConsoleUsageClient(baseUrl = '', fetcher: typeof fetch = fetch) {
  const base = baseUrl.replace(/\/$/, '');
  return {
    async get(workspaceId: string, signal?: AbortSignal): Promise<ConsoleUsageSnapshot> {
      if (!idPattern.test(workspaceId)) throw new Error('Workspace ID 格式无效');
      const timeout = AbortSignal.timeout(5_000);
      const response = await fetcher(`${base}/api/console/v1/workspaces/${encodeURIComponent(workspaceId)}/usage`, {
        method: 'GET', signal: signal ? AbortSignal.any([signal, timeout]) : timeout,
        cache: 'no-store', credentials: 'same-origin', referrerPolicy: 'same-origin', headers: { Accept: 'application/json' },
      });
      if (!response.ok) throw await errorFrom(response);
      const root = object(await response.json());
      exactKeys(root, ['data']);
      const data = object(root.data);
      exactKeys(data, ['budget_periods', 'usage_entries']);
      if (!Array.isArray(data.budget_periods) || !Array.isArray(data.usage_entries) || data.budget_periods.length > 100 || data.usage_entries.length > 100) {
        throw new Error('服务返回了无法识别的 Usage 数据');
      }
      return { budgetPeriods: data.budget_periods.map(parseBudget), usageEntries: data.usage_entries.map(parseUsage) };
    },
  };
}
