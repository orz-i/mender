export type RunState =
  | 'queued'
  | 'running'
  | 'waiting_input'
  | 'cancel_requested'
  | 'reconciling'
  | 'succeeded'
  | 'failed'
  | 'canceled'
  | 'timed_out';

export interface Run {
  id: string;
  workspaceId: string;
  state: RunState;
  version: string;
  createdAt: string;
  updatedAt: string;
}

export interface RunAgentInput {
  inputRequestId: string;
  state: 'pending' | 'sending' | 'unknown' | 'submitted';
  prompt: string;
  inputSchema: Record<string, unknown>;
  requestedAt: string;
  updatedAt: string;
}

export interface RunEvent {
  version: string;
  state: RunState;
  occurredAt: string;
  subjectId: string;
  reason: string;
}

export interface RunArtifact {
  id: string;
  kind: string;
  mediaType: string;
  sizeBytes: number;
  createdAt: string;
}

export interface RunArtifactDetail extends RunArtifact {
  content: unknown;
}

export interface RunArtifactObjectStatus {
  state: 'not_materialized' | 'available' | 'expired';
  expiresAt: string | null;
}

export interface RunArtifactObjectContent {
  artifactId: string;
  content: unknown;
  capabilityExpiresAt: string;
}

export interface RunQuotaCost {
  runId: string;
  budgetId: string;
  periodId: string;
  currency: string;
  quotaState: 'held' | 'released' | 'settled';
  reservedMicro: string;
  chargedMicro: string | null;
  releasedMicro: string;
  outcome: 'succeeded' | 'failed' | 'canceled' | null;
  createdAt: string;
  finalizedAt: string | null;
}

export function isTerminal(state: RunState) {
  return state === 'succeeded' || state === 'failed' || state === 'canceled' || state === 'timed_out';
}

export function canRequestCancellation(state: RunState) {
  return !isTerminal(state) && state !== 'cancel_requested';
}
