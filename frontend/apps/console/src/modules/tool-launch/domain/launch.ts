export type LaunchWorkspaceRole = 'owner' | 'admin' | 'developer' | 'viewer';

export interface LaunchWorkspace { id: string; role: LaunchWorkspaceRole }

export interface LaunchOption {
  toolsetVersionId: string;
  toolId: string;
  toolVersion: string;
  toolVersionId: string;
  title: string;
  description: string;
  inputSchema: Record<string, unknown>;
  sideEffect: 'read_only' | 'write';
  idempotency: 'safe_read' | 'idempotent' | 'unsafe';
  connectionId: string;
  providerId: string;
  currency: string;
  reserveMicro: string;
}

export type ExecutionRiskLevel = 'low' | 'medium' | 'high' | 'critical';
export type ExecutionRiskOutcome = 'allow' | 'confirmation_required' | 'deny';

export interface ExecutionRiskDecision {
  sequence: string;
  policyRevisionId: string;
  policyRevision: string;
  toolsetVersionId: string;
  toolVersionId: string;
  connectionId: string;
  argumentsHash: string;
  riskLevel: ExecutionRiskLevel;
  outcome: ExecutionRiskOutcome;
  reasonCodes: string[];
  evaluatedAt: string;
}

export function launchOptionKey(option: LaunchOption) {
  return `${option.toolsetVersionId}/${option.toolVersionId}/${option.connectionId}`;
}
