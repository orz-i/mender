import type { HistoryWorkspace, PublicationHistoryFilter, PublicationHistoryPage } from '../domain/history';

export class HistoryLoginRequiredError extends Error {}

export interface HistoryGateway {
  workspaces(signal?: AbortSignal): Promise<HistoryWorkspace[]>;
  list(workspaceId: string, filter: PublicationHistoryFilter, signal?: AbortSignal): Promise<PublicationHistoryPage>;
}

