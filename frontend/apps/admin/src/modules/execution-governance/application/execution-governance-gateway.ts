import type { ExecutionGovernanceFilter, ExecutionGovernanceSnapshot, ExecutionGovernanceWorkspace, ExecutionPolicyInput, ExecutionPolicyRevision } from '../domain/execution-governance';

export class ExecutionGovernanceLoginRequiredError extends Error {}

export interface ExecutionGovernanceGateway {
  workspaces(signal?: AbortSignal): Promise<ExecutionGovernanceWorkspace[]>;
  snapshot(workspaceId: string, filter?: ExecutionGovernanceFilter, signal?: AbortSignal): Promise<ExecutionGovernanceSnapshot>;
  create(workspaceId: string, input: ExecutionPolicyInput, signal?: AbortSignal): Promise<ExecutionPolicyRevision>;
  activate(workspaceId: string, policyId: string, signal?: AbortSignal): Promise<ExecutionPolicyRevision>;
}
