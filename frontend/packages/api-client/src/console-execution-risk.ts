import { MenderApiError } from './runs.ts';

export type ConsoleExecutionRiskLevel = 'low' | 'medium' | 'high' | 'critical';
export type ConsoleExecutionRiskOutcome = 'allow' | 'confirmation_required' | 'deny';

export interface ConsoleExecutionRiskInput {
  toolsetVersionId: string;
  toolVersionId: string;
  connectionId: string;
  idempotencyKey: string;
  arguments: Record<string, unknown>;
}

export interface ConsoleExecutionRiskDecision {
  sequence: string;
  policyRevisionId: string;
  policyRevision: string;
  toolsetVersionId: string;
  toolVersionId: string;
  connectionId: string;
  argumentsHash: string;
  riskLevel: ConsoleExecutionRiskLevel;
  outcome: ConsoleExecutionRiskOutcome;
  reasonCodes: string[];
  evaluatedAt: string;
}

export interface ConsoleExecutionRiskConfirmation {
  decision: ConsoleExecutionRiskDecision;
  confirmationId: string | null;
  expiresAt: string | null;
}

const idPattern = /^[A-Za-z0-9_-]{1,128}$/;
const idemPattern = /^[!-~]{8,128}$/;
const hashPattern = /^[a-f0-9]{64}$/;
const exactPositive = /^[1-9][0-9]*$/;
const forbidden = ['credential_version_ref', 'secret', 'token', 'artifact', 'artifact_body', 'reserve_micro', 'limit_micro', 'charge_micro', 'payment', 'invoice', 'revenue', 'arguments', 'idempotency_key_hash'];

function object(value: unknown): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error('服务返回了无法识别的 execution risk 响应');
  return value as Record<string, unknown>;
}
function string(value: unknown) {
  if (typeof value !== 'string' || value.length === 0) throw new Error('服务返回了无法识别的 execution risk 响应');
  return value;
}
function safe(raw: Record<string, unknown>) {
  for (const key of forbidden) if (key in raw) throw new Error('服务返回了不应暴露给 execution risk 的敏感字段');
}
function exact(value: unknown) {
  const raw = string(value);
  if (!exactPositive.test(raw)) throw new Error('服务返回了无法识别的 execution risk exact integer');
  return raw;
}
function decision(value: unknown): ConsoleExecutionRiskDecision {
  const raw = object(value);
  safe(raw);
  const risk = string(raw.risk_level);
  if (!['low', 'medium', 'high', 'critical'].includes(risk)) throw new Error('服务返回了无法识别的 execution risk level');
  const outcome = string(raw.outcome);
  if (!['allow', 'confirmation_required', 'deny'].includes(outcome)) throw new Error('服务返回了无法识别的 execution risk outcome');
  const argumentsHash = string(raw.arguments_hash);
  if (!hashPattern.test(argumentsHash)) throw new Error('服务返回了无法识别的 arguments hash');
  if (!Array.isArray(raw.reason_codes) || raw.reason_codes.length === 0 || raw.reason_codes.some((item) => typeof item !== 'string' || item.length === 0)) throw new Error('服务返回了无法识别的 execution risk reasons');
  return {
    sequence: exact(raw.sequence), policyRevisionId: string(raw.policy_revision_id), policyRevision: exact(raw.policy_revision),
    toolsetVersionId: string(raw.toolset_version_id), toolVersionId: string(raw.tool_version_id), connectionId: string(raw.connection_id), argumentsHash,
    riskLevel: risk as ConsoleExecutionRiskLevel, outcome: outcome as ConsoleExecutionRiskOutcome, reasonCodes: raw.reason_codes as string[], evaluatedAt: string(raw.evaluated_at),
  };
}
function requireInput(input: ConsoleExecutionRiskInput) {
  if (!idPattern.test(input.toolsetVersionId) || !idPattern.test(input.toolVersionId) || !idPattern.test(input.connectionId) || !idemPattern.test(input.idempotencyKey)) throw new Error('Execution risk target 格式无效');
  if (typeof input.arguments !== 'object' || input.arguments === null || Array.isArray(input.arguments)) throw new Error('Tool arguments 必须是 JSON object');
}
function requireCSRF(value: string) { if (!value || value.length > 256) throw new Error('CSRF token unavailable'); }
function signalOf(signal?: AbortSignal) { const timeout = AbortSignal.timeout(5_000); return signal ? AbortSignal.any([signal, timeout]) : timeout; }
async function failure(response: Response) {
  try { const raw = object(await response.json()); const error = object(raw.error); return new MenderApiError(response.status, string(error.code), string(error.message)); }
  catch (error) { if (error instanceof MenderApiError) return error; return new MenderApiError(response.status, 'HTTP_ERROR', `Mender API 请求失败（HTTP ${response.status}）`); }
}

export function createConsoleExecutionRiskClient(baseUrl = '', fetcher: typeof fetch = fetch) {
  const base = baseUrl.replace(/\/$/, '');
  async function mutate(workspaceId: string, action: 'preview' | 'confirm', input: ConsoleExecutionRiskInput, csrf: string, signal?: AbortSignal) {
    if (!idPattern.test(workspaceId)) throw new Error('Workspace ID 格式无效');
    requireInput(input); requireCSRF(csrf);
    const response = await fetcher(`${base}/api/console/v1/workspaces/${encodeURIComponent(workspaceId)}/execution-risk/${action}`, {
      method: 'POST', signal: signalOf(signal), cache: 'no-store', credentials: 'same-origin', referrerPolicy: 'same-origin',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json', 'X-Mender-CSRF': csrf },
      body: JSON.stringify({ toolset_version_id: input.toolsetVersionId, tool_version_id: input.toolVersionId, connection_id: input.connectionId, idempotency_key: input.idempotencyKey, arguments: input.arguments }),
    });
    if (!response.ok) throw await failure(response);
    const raw = object(await response.json());
    return object(raw.data);
  }
  return {
    async preview(workspaceId: string, input: ConsoleExecutionRiskInput, csrf: string, signal?: AbortSignal): Promise<ConsoleExecutionRiskDecision> {
      const data = await mutate(workspaceId, 'preview', input, csrf, signal);
      return decision(data);
    },
    async confirm(workspaceId: string, input: ConsoleExecutionRiskInput, csrf: string, signal?: AbortSignal): Promise<ConsoleExecutionRiskConfirmation> {
      const data = await mutate(workspaceId, 'confirm', input, csrf, signal);
      safe(data);
      const parsed = decision(data.decision);
      const confirmationId = data.confirmation_id === null ? null : string(data.confirmation_id);
      const expiresAt = data.expires_at === null ? null : string(data.expires_at);
      if ((parsed.outcome === 'confirmation_required') !== (confirmationId !== null && expiresAt !== null)) throw new Error('服务返回了不一致的 execution confirmation');
      return { decision: parsed, confirmationId, expiresAt };
    },
  };
}
