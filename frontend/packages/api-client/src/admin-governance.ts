import { MenderApiError } from './runs.ts';

export type AdminPublicationApprovalState = 'pending' | 'approved' | 'rejected' | 'consumed' | 'expired';
export interface AdminPublicationApproval {
  id: string;
  targetKind: 'tool_version' | 'toolset';
  targetId: string;
  targetRevision: string;
  requesterUserId: string;
  state: AdminPublicationApprovalState;
  requestedAt: string;
  expiresAt: string;
  reviewerUserId: string | null;
  reviewedAt: string | null;
  decisionNote: string;
  consumedAt: string | null;
}
function bool(value: unknown) { if (typeof value !== 'boolean') throw new Error('服务返回了无法识别的 Governance policy'); return value; }
function policyRevision(value: unknown): AdminPublicationPolicyRevision {
  const raw = object(value); safeProjection(raw);
  const state = string(raw.state); if (!['draft', 'active', 'retired'].includes(state)) throw new Error('服务返回了无法识别的 policy state');
  const risk = string(raw.max_risk_level); if (!['low', 'medium', 'high', 'critical'].includes(risk)) throw new Error('服务返回了无法识别的 policy risk');
  return { id: string(raw.id), revision: exactPositiveIntegerString(raw.revision), state: state as AdminPublicationPolicyRevision['state'], maxRiskLevel: risk as AdminPublicationRiskLevel, denyUnsafeWrite: bool(raw.deny_unsafe_write), denyMcpUnsafeWrite: bool(raw.deny_mcp_unsafe_write), createdByUserId: optionalString(raw.created_by_user_id), createdAt: string(raw.created_at), activatedByUserId: optionalString(raw.activated_by_user_id), activatedAt: optionalString(raw.activated_at), retiredAt: optionalString(raw.retired_at) };
}
function policyDecision(value: unknown): AdminPublicationPolicyDecision {
  const raw = object(value); safeProjection(raw);
  const kind = string(raw.target_kind); if (kind !== 'tool_version' && kind !== 'toolset') throw new Error('服务返回了无法识别的 policy target');
  const risk = string(raw.risk_level); if (!['low', 'medium', 'high', 'critical'].includes(risk)) throw new Error('服务返回了无法识别的 policy risk');
  const outcome = string(raw.outcome); if (outcome !== 'allow' && outcome !== 'deny') throw new Error('服务返回了无法识别的 policy outcome');
  if (!Array.isArray(raw.reason_codes) || raw.reason_codes.length === 0 || raw.reason_codes.some((item) => typeof item !== 'string' || item.length === 0)) throw new Error('服务返回了无法识别的 policy reasons');
  return { sequence: exactPositiveIntegerString(raw.sequence), policyRevisionId: string(raw.policy_revision_id), policyRevision: exactPositiveIntegerString(raw.policy_revision), targetKind: kind, targetId: string(raw.target_id), targetRevision: exactPositiveIntegerString(raw.target_revision), riskLevel: risk as AdminPublicationRiskLevel, outcome, reasonCodes: raw.reason_codes as string[], evaluatedAt: string(raw.evaluated_at) };
}

export type AdminPublicationRiskLevel = 'low' | 'medium' | 'high' | 'critical';
export interface AdminPublicationPolicyRevision {
  id: string; revision: string; state: 'draft' | 'active' | 'retired'; maxRiskLevel: AdminPublicationRiskLevel;
  denyUnsafeWrite: boolean; denyMcpUnsafeWrite: boolean; createdByUserId: string | null; createdAt: string;
  activatedByUserId: string | null; activatedAt: string | null; retiredAt: string | null;
}
export interface AdminPublicationPolicyDecision {
  sequence: string; policyRevisionId: string; policyRevision: string; targetKind: 'tool_version' | 'toolset'; targetId: string;
  targetRevision: string; riskLevel: AdminPublicationRiskLevel; outcome: 'allow' | 'deny'; reasonCodes: string[]; evaluatedAt: string;
}
export interface AdminPublicationPolicySnapshot { revisions: AdminPublicationPolicyRevision[]; decisions: AdminPublicationPolicyDecision[] }
export interface AdminPublicationPolicyInput { id: string; maxRiskLevel: AdminPublicationRiskLevel; denyUnsafeWrite: boolean; denyMcpUnsafeWrite: boolean }

export type AdminPublicationAuditEventKind =
  | 'audit_baseline'
  | 'approval_submitted'
  | 'approval_approved'
  | 'approval_rejected'
  | 'approval_expired'
  | 'approval_consumed'
  | 'publication_committed'
  | 'publication_retired';

export interface AdminPublicationAuditEvent {
  sequence: string;
  approvalId: string | null;
  targetKind: 'tool_version' | 'toolset';
  targetId: string;
  targetRevision: string;
  observedRevision: string | null;
  eventKind: AdminPublicationAuditEventKind;
  actorUserId: string | null;
  occurredAt: string;
  reasonCode: string;
  note: string;
}

export interface AdminPublicationHistoryFilter {
  targetKind?: 'tool_version' | 'toolset';
  targetId?: string;
  approvalId?: string;
  eventKind?: AdminPublicationAuditEventKind;
  beforeSequence?: string;
  limit?: number;
}

export interface AdminPublicationHistoryPage {
  events: AdminPublicationAuditEvent[];
  nextBeforeSequence: string | null;
}

function object(value: unknown): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error('服务返回了无法识别的 Governance 响应');
  return value as Record<string, unknown>;
}
function string(value: unknown) { if (typeof value !== 'string' || value.length === 0) throw new Error('服务返回了无法识别的 Governance 响应'); return value; }
function optionalString(value: unknown) { if (value === null) return null; return string(value); }
function exactIntegerString(value: unknown) { const raw = string(value); if (!/^[0-9]+$/.test(raw)) throw new Error('服务返回了无法识别的 revision'); return raw; }
function exactPositiveIntegerString(value: unknown) { const raw = string(value); if (!/^[1-9][0-9]*$/.test(raw)) throw new Error('服务返回了无法识别的 exact integer'); return raw; }
const forbiddenGovernanceFields = ['credential_version_ref', 'secret', 'token', 'arguments', 'artifact', 'artifact_body', 'reserve_micro', 'limit_micro', 'consumed_micro', 'reserved_micro', 'available_micro', 'charge_micro', 'charged_micro', 'payment', 'invoice', 'revenue'];
function safeProjection(raw: Record<string, unknown>) {
  for (const forbidden of forbiddenGovernanceFields) if (forbidden in raw) throw new Error('服务返回了不应暴露给 Governance 的敏感字段');
}
function approval(value: unknown): AdminPublicationApproval {
  const raw = object(value);
  safeProjection(raw);
  const kind = string(raw.target_kind); if (kind !== 'tool_version' && kind !== 'toolset') throw new Error('服务返回了无法识别的 Governance target');
  const state = string(raw.state); if (!['pending', 'approved', 'rejected', 'consumed', 'expired'].includes(state)) throw new Error('服务返回了无法识别的 Governance 状态');
  return { id: string(raw.id), targetKind: kind, targetId: string(raw.target_id), targetRevision: exactIntegerString(raw.target_revision), requesterUserId: string(raw.requester_user_id), state: state as AdminPublicationApprovalState, requestedAt: string(raw.requested_at), expiresAt: string(raw.expires_at), reviewerUserId: optionalString(raw.reviewer_user_id), reviewedAt: optionalString(raw.reviewed_at), decisionNote: typeof raw.decision_note === 'string' ? raw.decision_note : '', consumedAt: optionalString(raw.consumed_at) };
}
function auditEvent(value: unknown): AdminPublicationAuditEvent {
  const raw = object(value);
  safeProjection(raw);
  const kind = string(raw.target_kind);
  if (kind !== 'tool_version' && kind !== 'toolset') throw new Error('服务返回了无法识别的 Governance target');
  const eventKind = string(raw.event_kind);
  const allowed: AdminPublicationAuditEventKind[] = ['audit_baseline', 'approval_submitted', 'approval_approved', 'approval_rejected', 'approval_expired', 'approval_consumed', 'publication_committed', 'publication_retired'];
  if (!allowed.includes(eventKind as AdminPublicationAuditEventKind)) throw new Error('服务返回了无法识别的 Governance audit event');
  return {
    sequence: exactPositiveIntegerString(raw.sequence), approvalId: optionalString(raw.approval_id), targetKind: kind, targetId: string(raw.target_id),
    targetRevision: exactPositiveIntegerString(raw.target_revision), observedRevision: raw.observed_revision === null ? null : exactPositiveIntegerString(raw.observed_revision),
    eventKind: eventKind as AdminPublicationAuditEventKind, actorUserId: optionalString(raw.actor_user_id), occurredAt: string(raw.occurred_at),
    reasonCode: typeof raw.reason_code === 'string' ? raw.reason_code : '', note: typeof raw.note === 'string' ? raw.note : '',
  };
}
async function failure(response: Response) {
  try { const raw = object(await response.json()); const error = object(raw.error); return new MenderApiError(response.status, string(error.code), string(error.message)); }
  catch (error) { if (error instanceof MenderApiError) return error; return new MenderApiError(response.status, 'HTTP_ERROR', `Mender API 请求失败（HTTP ${response.status}）`); }
}
async function request(fetcher: typeof fetch, url: string, init: RequestInit = {}) {
  const timeout = AbortSignal.timeout(5_000); const signal = init.signal ? AbortSignal.any([init.signal, timeout]) : timeout;
  const response = await fetcher(url, { ...init, signal, cache: 'no-store', credentials: 'same-origin', referrerPolicy: 'same-origin', headers: { Accept: 'application/json', ...init.headers } });
  if (!response.ok) throw await failure(response); return response;
}
function csrf(value: string) { if (!value || value.length > 256) throw new Error('Governance mutation requires a valid CSRF token'); return value; }

export function createAdminGovernanceClient(baseUrl = '', fetcher: typeof fetch = fetch) {
  const base = baseUrl.replace(/\/$/, '');
  const root = (workspaceId: string) => `${base}/api/admin/v1/workspaces/${encodeURIComponent(workspaceId)}/publication-approvals`;
  const historyRoot = (workspaceId: string) => `${base}/api/admin/v1/workspaces/${encodeURIComponent(workspaceId)}/publication-history`;
  const policyRoot = (workspaceId: string) => `${base}/api/admin/v1/workspaces/${encodeURIComponent(workspaceId)}/publication-policy`;
  return {
    async list(workspaceId: string, signal?: AbortSignal): Promise<AdminPublicationApproval[]> {
      if (!workspaceId) throw new Error('Workspace ID is required');
      const response = await request(fetcher, root(workspaceId), { signal }); const raw = object(await response.json());
      if (!Array.isArray(raw.data)) throw new Error('服务返回了无法识别的 Governance approval 列表'); return raw.data.map(approval);
    },
    async approve(workspaceId: string, approvalId: string, note: string, csrfToken: string, signal?: AbortSignal) {
      return decide(fetcher, `${root(workspaceId)}/${encodeURIComponent(approvalId)}/approve`, note, csrf(csrfToken), signal);
    },
    async reject(workspaceId: string, approvalId: string, note: string, csrfToken: string, signal?: AbortSignal) {
      return decide(fetcher, `${root(workspaceId)}/${encodeURIComponent(approvalId)}/reject`, note, csrf(csrfToken), signal);
    },
    async history(workspaceId: string, filter: AdminPublicationHistoryFilter = {}, signal?: AbortSignal): Promise<AdminPublicationHistoryPage> {
      if (!workspaceId) throw new Error('Workspace ID is required');
      if (filter.limit !== undefined && (!Number.isInteger(filter.limit) || filter.limit < 1 || filter.limit > 100)) throw new Error('History limit must be an integer between 1 and 100');
      if (filter.beforeSequence !== undefined && !/^[1-9][0-9]*$/.test(filter.beforeSequence)) throw new Error('History cursor must be a positive decimal string');
      const params = new URLSearchParams();
      if (filter.targetKind) params.set('target_kind', filter.targetKind);
      if (filter.targetId) params.set('target_id', filter.targetId);
      if (filter.approvalId) params.set('approval_id', filter.approvalId);
      if (filter.eventKind) params.set('event_kind', filter.eventKind);
      if (filter.beforeSequence) params.set('before_sequence', filter.beforeSequence);
      if (filter.limit !== undefined) params.set('limit', String(filter.limit));
      const suffix = params.size > 0 ? `?${params.toString()}` : '';
      const response = await request(fetcher, `${historyRoot(workspaceId)}${suffix}`, { signal });
      const raw = object(await response.json()); const data = object(raw.data);
      if (!Array.isArray(data.events)) throw new Error('服务返回了无法识别的 Governance history');
      const next = data.next_before_sequence === null ? null : exactPositiveIntegerString(data.next_before_sequence);
      return { events: data.events.map(auditEvent), nextBeforeSequence: next };
    },
    async policy(workspaceId: string, signal?: AbortSignal): Promise<AdminPublicationPolicySnapshot> {
      if (!workspaceId) throw new Error('Workspace ID is required');
      const response = await request(fetcher, policyRoot(workspaceId), { signal }); const raw = object(await response.json()); const data = object(raw.data);
      if (!Array.isArray(data.revisions) || !Array.isArray(data.decisions)) throw new Error('服务返回了无法识别的 Governance policy snapshot');
      return { revisions: data.revisions.map(policyRevision), decisions: data.decisions.map(policyDecision) };
    },
    async createPolicy(workspaceId: string, input: AdminPublicationPolicyInput, csrfToken: string, signal?: AbortSignal) {
      const response = await request(fetcher, `${policyRoot(workspaceId)}/revisions`, { method: 'POST', signal, headers: { 'X-Mender-CSRF': csrf(csrfToken), 'Content-Type': 'application/json' }, body: JSON.stringify({ id: input.id, max_risk_level: input.maxRiskLevel, deny_unsafe_write: input.denyUnsafeWrite, deny_mcp_unsafe_write: input.denyMcpUnsafeWrite }) });
      return policyRevision(object(await response.json()).data);
    },
    async activatePolicy(workspaceId: string, policyId: string, csrfToken: string, signal?: AbortSignal) {
      const response = await request(fetcher, `${policyRoot(workspaceId)}/revisions/${encodeURIComponent(policyId)}/activate`, { method: 'POST', signal, headers: { 'X-Mender-CSRF': csrf(csrfToken) } });
      return policyRevision(object(await response.json()).data);
    },
  };
}

async function decide(fetcher: typeof fetch, url: string, note: string, csrfToken: string, signal?: AbortSignal) {
  if (note.length > 1000) throw new Error('Review note is too long');
  const response = await request(fetcher, url, { method: 'POST', signal, headers: { 'X-Mender-CSRF': csrfToken, 'Content-Type': 'application/json' }, body: JSON.stringify({ note }) });
  return approval(object(await response.json()).data);
}
