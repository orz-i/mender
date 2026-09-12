import type { ConnectionSummary, WorkspaceOption } from '../domain/connection';

export class ConnectionLoginRequiredError extends Error {
  constructor() { super('Console login is required'); this.name = 'ConnectionLoginRequiredError'; }
}

export interface ConnectionGateway {
  workspaces(signal?: AbortSignal): Promise<WorkspaceOption[]>;
  list(workspaceId: string, signal?: AbortSignal): Promise<ConnectionSummary[]>;
  revoke(workspaceId: string, connectionId: string, signal?: AbortSignal): Promise<ConnectionSummary>;
}
