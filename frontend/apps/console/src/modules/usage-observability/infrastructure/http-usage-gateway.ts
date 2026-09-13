import { createConsoleIdentityClient, createConsoleUsageClient, MenderApiError } from '@mender/api-client';
import { UsageLoginRequiredError, type UsageGateway } from '../application/usage-gateway';
import type { BudgetPeriod, UsageEntry } from '../domain/usage';

function mapError(error: unknown): never {
  if (error instanceof MenderApiError && error.status === 401) throw new UsageLoginRequiredError();
  throw error;
}

function budget(value: Awaited<ReturnType<ReturnType<typeof createConsoleUsageClient>['get']>>['budgetPeriods'][number]): BudgetPeriod {
  return { ...value };
}

function usage(value: Awaited<ReturnType<ReturnType<typeof createConsoleUsageClient>['get']>>['usageEntries'][number]): UsageEntry {
  return { ...value };
}

export function createUsageGateway(): UsageGateway {
  const identity = createConsoleIdentityClient();
  const usageClient = createConsoleUsageClient();
  return {
    async workspaces(signal) {
      try {
        return (await identity.listWorkspaces(signal)).map((item) => ({ id: item.workspaceId, role: item.role }));
      } catch (error) { return mapError(error); }
    },
    async snapshot(workspaceId, signal) {
      try {
        const result = await usageClient.get(workspaceId, signal);
        return { budgetPeriods: result.budgetPeriods.map(budget), usageEntries: result.usageEntries.map(usage) };
      } catch (error) { return mapError(error); }
    },
  };
}
