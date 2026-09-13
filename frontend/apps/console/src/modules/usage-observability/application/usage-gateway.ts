import type { UsageSnapshot, UsageWorkspace } from '../domain/usage';

export class UsageLoginRequiredError extends Error {
  constructor() {
    super('Console login required');
    this.name = 'UsageLoginRequiredError';
  }
}

export interface UsageGateway {
  workspaces(signal?: AbortSignal): Promise<UsageWorkspace[]>;
  snapshot(workspaceId: string, signal?: AbortSignal): Promise<UsageSnapshot>;
}
