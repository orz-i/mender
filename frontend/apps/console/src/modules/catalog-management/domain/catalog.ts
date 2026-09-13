export type WorkspaceRole = 'owner' | 'admin' | 'developer' | 'viewer';
export type CatalogState = 'draft' | 'published' | 'retired';
export type SideEffect = 'read_only' | 'write';
export type Idempotency = 'safe_read' | 'idempotent' | 'unsafe';

export interface CatalogWorkspace { id: string; role: WorkspaceRole }
export interface CatalogToolVersion {
  toolVersionId: string; toolId: string; version: string; providerId: string; priceVersionId: string; deploymentRevision: string;
  title: string; description: string; inputSchema: Record<string, unknown>; outputSchema: Record<string, unknown>;
  sideEffect: SideEffect; idempotency: Idempotency; mcpPublishable: boolean; revision: string; state: CatalogState;
  createdAt: string; updatedAt: string; publishedAt: string | null; retiredAt: string | null;
}
export interface CatalogToolVersionInput {
  toolVersionId: string; toolId: string; version: string; providerId: string; priceVersionId: string; deploymentRevision: string;
  title: string; description: string; inputSchema: Record<string, unknown>; outputSchema: Record<string, unknown>;
  sideEffect: SideEffect; idempotency: Idempotency; mcpPublishable: boolean;
}
export interface CatalogBinding {
  toolId: string; toolVersionLabel: string; toolVersionId: string; budgetId: string; connectionId: string;
  mcpName: string; mcpExposed: boolean; state: CatalogState; publishedAt: string | null;
}
export interface CatalogBindingInput { toolId: string; toolVersionLabel: string; budgetId: string; connectionId: string; mcpName: string; mcpExposed: boolean }
export interface CatalogToolset { id: string; revision: string; state: CatalogState; createdAt: string; updatedAt: string; publishedAt: string | null; retiredAt: string | null; bindings: CatalogBinding[] }
export interface CatalogConnectionOption { connectionId: string; providerId: string; state: string; revision: string; createdAt: string; expiresAt: string }
export interface CatalogPriceOption { id: string; toolVersionId: string; currency: string; reserveMicro: string; startsAt: string; endsAt: string; active: boolean }
export interface CatalogBudgetOption { budgetId: string; periodId: string; currency: string; startsAt: string; endsAt: string; active: boolean }
export type PublicationApprovalState = 'pending' | 'approved' | 'rejected' | 'consumed' | 'expired';
export interface PublicationApproval { id: string; targetKind: 'tool_version' | 'toolset'; targetId: string; targetRevision: string; requesterUserId: string; state: PublicationApprovalState; requestedAt: string; expiresAt: string; reviewerUserId: string | null; reviewedAt: string | null; decisionNote: string; consumedAt: string | null }
export interface PublicationPolicyDecision { sequence: string; policyRevisionId: string; policyRevision: string; targetKind: 'tool_version' | 'toolset'; targetId: string; targetRevision: string; riskLevel: 'low' | 'medium' | 'high' | 'critical'; outcome: 'allow' | 'deny'; reasonCodes: string[]; evaluatedAt: string }
export interface PublicationSubmission { approval: PublicationApproval | null; policyDecision: PublicationPolicyDecision }
export interface CatalogSnapshot { toolVersions: CatalogToolVersion[]; toolsets: CatalogToolset[]; connections: CatalogConnectionOption[]; priceVersions: CatalogPriceOption[]; budgetPeriods: CatalogBudgetOption[]; publicationApprovals: PublicationApproval[] }
export interface CatalogIssue { code: string; targetId: string }
export interface CatalogPreflight { ready: boolean; issues: CatalogIssue[] }

export function latestApproval(approvals: PublicationApproval[], kind: PublicationApproval['targetKind'], id: string) {
  return approvals.find((item) => item.targetKind === kind && item.targetId === id) ?? null;
}

export function draftToolInput(tool?: CatalogToolVersion): CatalogToolVersionInput {
  if (tool) return {
    toolVersionId: tool.toolVersionId, toolId: tool.toolId, version: tool.version, providerId: tool.providerId,
    priceVersionId: tool.priceVersionId, deploymentRevision: tool.deploymentRevision, title: tool.title, description: tool.description,
    inputSchema: tool.inputSchema, outputSchema: tool.outputSchema, sideEffect: tool.sideEffect, idempotency: tool.idempotency, mcpPublishable: tool.mcpPublishable,
  };
  return { toolVersionId: '', toolId: '', version: '', providerId: '', priceVersionId: '', deploymentRevision: '', title: '', description: '', inputSchema: { type: 'object' }, outputSchema: { type: 'object' }, sideEffect: 'read_only', idempotency: 'safe_read', mcpPublishable: false };
}
