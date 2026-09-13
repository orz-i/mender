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

function object(value: unknown): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error('服务返回了无法识别的 Governance 响应');
  return value as Record<string, unknown>;
}
function string(value: unknown) { if (typeof value !== 'string' || value.length === 0) throw new Error('服务返回了无法识别的 Governance 响应'); return value; }
function optionalString(value: unknown) { if (value === null) return null; return string(value); }
function exactIntegerString(value: unknown) { const raw = string(value); if (!/^[0-9]+$/.test(raw)) throw new Error('服务返回了无法识别的 revision'); return raw; }
function approval(value: unknown): AdminPublicationApproval {
  const raw = object(value);
  for (const forbidden of ['credential_version_ref', 'secret', 'token', 'reserve_micro', 'limit_micro', 'consumed_micro', 'charged_micro']) if (forbidden in raw) throw new Error('服务返回了不应暴露给 Governance 的敏感字段');
  const kind = string(raw.target_kind); if (kind !== 'tool_version' && kind !== 'toolset') throw new Error('服务返回了无法识别的 Governance target');
  const state = string(raw.state); if (!['pending', 'approved', 'rejected', 'consumed', 'expired'].includes(state)) throw new Error('服务返回了无法识别的 Governance 状态');
  return { id: string(raw.id), targetKind: kind, targetId: string(raw.target_id), targetRevision: exactIntegerString(raw.target_revision), requesterUserId: string(raw.requester_user_id), state: state as AdminPublicationApprovalState, requestedAt: string(raw.requested_at), expiresAt: string(raw.expires_at), reviewerUserId: optionalString(raw.reviewer_user_id), reviewedAt: optionalString(raw.reviewed_at), decisionNote: typeof raw.decision_note === 'string' ? raw.decision_note : '', consumedAt: optionalString(raw.consumed_at) };
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
  };
}

async function decide(fetcher: typeof fetch, url: string, note: string, csrfToken: string, signal?: AbortSignal) {
  if (note.length > 1000) throw new Error('Review note is too long');
  const response = await request(fetcher, url, { method: 'POST', signal, headers: { 'X-Mender-CSRF': csrfToken, 'Content-Type': 'application/json' }, body: JSON.stringify({ note }) });
  return approval(object(await response.json()).data);
}
