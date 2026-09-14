export type ExecutionGovernanceWorkspaceRole = 'owner' | 'admin' | 'developer' | 'viewer';
export type ExecutionRiskLevel = 'low' | 'medium' | 'high' | 'critical';
export type ExecutionPolicyState = 'draft' | 'active' | 'retired';
export type ExecutionOutcome = 'allow' | 'confirmation_required' | 'deny';
export type ExecutionConfirmationState = 'active' | 'consumed' | 'expired';
export type ExecutionSubjectKind = 'human' | 'machine';
export type ProviderCallbackDisposition = 'pending' | 'accepted' | 'quarantined';
export type ProviderCallbackObservationState = 'pending' | 'succeeded' | 'failed' | 'canceled';
export type ProviderCallbackReason = 'event_id_conflict' | 'attempt_binding_mismatch' | 'observation_before_submission' | 'out_of_order' | 'terminal_replay' | 'execution_conflict';

export interface ExecutionGovernanceWorkspace { id: string; role: ExecutionGovernanceWorkspaceRole }
export interface ExecutionPolicyRevision {
  id: string; revision: string; state: ExecutionPolicyState;
  maxUnconfirmedRiskLevel: ExecutionRiskLevel; maxMachineRiskLevel: ExecutionRiskLevel;
  denyUnsafeWrite: boolean; confirmationTtlSeconds: number;
  createdByUserId: string | null; createdAt: string; activatedByUserId: string | null; activatedAt: string | null; retiredAt: string | null;
}
export interface ExecutionPolicyDecision {
  sequence: string; policyRevisionId: string; policyRevision: string; subjectKind: ExecutionSubjectKind; subjectId: string;
  toolsetVersionId: string; toolVersionId: string; connectionId: string; argumentsHash: string; idempotencyKeyHash: string;
  riskLevel: ExecutionRiskLevel; outcome: ExecutionOutcome; reasonCodes: string[]; evaluatedAt: string;
}
export interface ExecutionConfirmation {
  id: string; userId: string; policyRevisionId: string; policyRevision: string; toolsetVersionId: string; toolVersionId: string; connectionId: string;
  argumentsHash: string; idempotencyKeyHash: string; riskLevel: ExecutionRiskLevel; state: ExecutionConfirmationState;
  persistedState: ExecutionConfirmationState; createdAt: string; expiresAt: string; consumedAt: string | null; expiredAt: string | null;
}
export interface ExecutionGovernanceSnapshot {
  activePolicy: ExecutionPolicyRevision | null; revisions: ExecutionPolicyRevision[]; decisions: ExecutionPolicyDecision[]; confirmations: ExecutionConfirmation[];
  nextBeforeDecisionSequence: string | null; nextConfirmationCursor: { createdAt: string; id: string } | null;
}
export interface ExecutionGovernanceFilter {
  toolVersionId?: string; subjectKind?: ExecutionSubjectKind; riskLevel?: ExecutionRiskLevel; outcome?: ExecutionOutcome;
  policyRevision?: string; confirmationState?: ExecutionConfirmationState; limit?: number;
}
export interface ExecutionPolicyInput {
  id: string; maxUnconfirmedRiskLevel: ExecutionRiskLevel; maxMachineRiskLevel: ExecutionRiskLevel; denyUnsafeWrite: boolean; confirmationTtlSeconds: number;
}
export interface ProviderCallbackInboxItem {
  receiptId: string; workspaceId: string; providerId: string; eventId: string; runId: string; observationId: string;
  observationState: ProviderCallbackObservationState; disposition: ProviderCallbackDisposition; reasonCode: ProviderCallbackReason | null;
  deliveryCount: number; duplicateDeliveryCount: number; receivedAt: string; lastReceivedAt: string; observedAt: string; processedAt: string | null;
}
export interface ProviderCallbackCursor { receivedAt: string; receiptId: string }
export interface ProviderCallbackInboxPage { items: ProviderCallbackInboxItem[]; nextCursor: ProviderCallbackCursor | null }
export interface ProviderCallbackInboxFilter {
  providerId?: string; disposition?: ProviderCallbackDisposition; reasonCode?: ProviderCallbackReason;
  beforeReceivedAt?: string; beforeReceiptId?: string; limit?: number;
}
