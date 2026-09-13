export type UsageWorkspaceRole = 'owner' | 'admin' | 'developer' | 'viewer';
export type QuotaState = 'held' | 'released' | 'settled';
export type QuotaOutcome = 'succeeded' | 'failed' | 'canceled';

export interface UsageWorkspace {
  id: string;
  role: UsageWorkspaceRole;
}

export interface BudgetPeriod {
  budgetId: string;
  periodId: string;
  currency: string;
  startsAt: string;
  endsAt: string;
  active: boolean;
  limitMicro: string;
  consumedMicro: string;
  reservedMicro: string;
  availableMicro: string;
  revision: string;
}

export interface UsageEntry {
  runId: string;
  budgetId: string;
  periodId: string;
  currency: string;
  quotaState: QuotaState;
  reservedMicro: string;
  chargedMicro: string | null;
  releasedMicro: string;
  outcome: QuotaOutcome | null;
  createdAt: string;
  finalizedAt: string | null;
}

export interface UsageSnapshot {
  budgetPeriods: BudgetPeriod[];
  usageEntries: UsageEntry[];
}
