import { createAdminGovernanceClient, createConsoleIdentityClient, MenderApiError } from '@mender/api-client';
import { HistoryLoginRequiredError, type HistoryGateway } from '../application/history-gateway';

function mapError(error: unknown): never {
  if (error instanceof MenderApiError && error.status === 401) throw new HistoryLoginRequiredError();
  throw error;
}

export function createHistoryGateway(): HistoryGateway {
  const identity = createConsoleIdentityClient();
  const governance = createAdminGovernanceClient();
  return {
    async workspaces(signal) {
      try { return (await identity.listWorkspaces(signal)).map((item) => ({ id: item.workspaceId, role: item.role })); }
      catch (error) { return mapError(error); }
    },
    async list(workspaceId, filter, signal) {
      try { return await governance.history(workspaceId, filter, signal); }
      catch (error) { return mapError(error); }
    },
  };
}

