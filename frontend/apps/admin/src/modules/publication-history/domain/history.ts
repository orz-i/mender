export type HistoryWorkspaceRole = 'owner' | 'admin' | 'developer' | 'viewer';
export type PublicationAuditTargetKind = 'tool_version' | 'toolset';
export type PublicationAuditEventKind =
  | 'audit_baseline'
  | 'approval_submitted'
  | 'approval_approved'
  | 'approval_rejected'
  | 'approval_expired'
  | 'approval_consumed'
  | 'publication_committed'
  | 'publication_retired';

export interface HistoryWorkspace { id: string; role: HistoryWorkspaceRole }
export interface PublicationAuditEvent {
  sequence: string;
  approvalId: string | null;
  targetKind: PublicationAuditTargetKind;
  targetId: string;
  targetRevision: string;
  observedRevision: string | null;
  eventKind: PublicationAuditEventKind;
  actorUserId: string | null;
  occurredAt: string;
  reasonCode: string;
  note: string;
}
export interface PublicationHistoryFilter {
  targetKind?: PublicationAuditTargetKind;
  targetId?: string;
  approvalId?: string;
  eventKind?: PublicationAuditEventKind;
  beforeSequence?: string;
  limit?: number;
}
export interface PublicationHistoryPage { events: PublicationAuditEvent[]; nextBeforeSequence: string | null }

