export type PolicyWorkspaceRole = 'owner' | 'admin' | 'developer' | 'viewer';
export type PublicationRiskLevel = 'low' | 'medium' | 'high' | 'critical';
export type PublicationPolicyState = 'draft' | 'active' | 'retired';

export interface PolicyWorkspace { id: string; role: PolicyWorkspaceRole }
export interface PublicationPolicyRevision {
  id: string; revision: string; state: PublicationPolicyState; maxRiskLevel: PublicationRiskLevel;
  denyUnsafeWrite: boolean; denyMcpUnsafeWrite: boolean; createdByUserId: string | null; createdAt: string;
  activatedByUserId: string | null; activatedAt: string | null; retiredAt: string | null;
}
export interface PublicationPolicyDecision {
  sequence: string; policyRevisionId: string; policyRevision: string; targetKind: 'tool_version' | 'toolset'; targetId: string;
  targetRevision: string; riskLevel: PublicationRiskLevel; outcome: 'allow' | 'deny'; reasonCodes: string[]; evaluatedAt: string;
}
export interface PublicationPolicySnapshot { revisions: PublicationPolicyRevision[]; decisions: PublicationPolicyDecision[] }
export interface PublicationPolicyInput { id: string; maxRiskLevel: PublicationRiskLevel; denyUnsafeWrite: boolean; denyMcpUnsafeWrite: boolean }
