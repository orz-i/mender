import type { HumanSessionOverview } from '../domain/workspace';

export class ConsoleUnauthenticatedError extends Error {
  constructor() {
    super('Console login is required');
    this.name = 'ConsoleUnauthenticatedError';
  }
}

export interface WorkspaceGateway {
  load(signal?: AbortSignal): Promise<HumanSessionOverview>;
  logout(signal?: AbortSignal): Promise<void>;
}
