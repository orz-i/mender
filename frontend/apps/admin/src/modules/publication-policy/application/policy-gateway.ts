import type { PolicyWorkspace, PublicationPolicyInput, PublicationPolicyRevision, PublicationPolicySnapshot } from '../domain/policy';

export class PolicyLoginRequiredError extends Error {}

export interface PolicyGateway {
  workspaces(signal?: AbortSignal): Promise<PolicyWorkspace[]>;
  snapshot(workspaceId: string, signal?: AbortSignal): Promise<PublicationPolicySnapshot>;
  create(workspaceId: string, input: PublicationPolicyInput, signal?: AbortSignal): Promise<PublicationPolicyRevision>;
  activate(workspaceId: string, policyId: string, signal?: AbortSignal): Promise<PublicationPolicyRevision>;
}
