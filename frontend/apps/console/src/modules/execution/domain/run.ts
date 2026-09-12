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

export function isTerminal(state: RunState) {
  return state === 'succeeded' || state === 'failed' || state === 'canceled' || state === 'timed_out';
}

export function canRequestCancellation(state: RunState) {
  return !isTerminal(state) && state !== 'cancel_requested';
}
