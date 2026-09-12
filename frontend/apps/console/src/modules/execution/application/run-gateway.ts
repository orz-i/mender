import type { Run, RunArtifact, RunArtifactDetail, RunEvent, RunState } from '../domain/run';

export interface RunAccess {
  workspaceId: string;
  delegatedToken: string;
  delegationId: string;
  expiresAt: string;
  canCancel: boolean;
}

export interface RunPage {
  items: Run[];
  nextCursor: string | null;
}

export interface RunEvents {
  items: RunEvent[];
  nextCursor: string | null;
  throughVersion: string;
}

export interface RunGateway {
  connect(workspaceId: string, signal?: AbortSignal): Promise<RunAccess>;
  disconnect(access: RunAccess, signal?: AbortSignal): Promise<void>;
  list(access: RunAccess, options: { state?: RunState; cursor?: string; limit?: number }, signal?: AbortSignal): Promise<RunPage>;
  get(access: RunAccess, runId: string, signal?: AbortSignal): Promise<Run>;
  events(access: RunAccess, runId: string, options: { cursor?: string; limit?: number; expectedThroughVersion?: string }, signal?: AbortSignal): Promise<RunEvents>;
  artifacts(access: RunAccess, runId: string, signal?: AbortSignal): Promise<RunArtifact[]>;
  artifact(access: RunAccess, runId: string, artifactId: string, signal?: AbortSignal): Promise<RunArtifactDetail>;
  cancel(access: RunAccess, runId: string, reason: string, signal?: AbortSignal): Promise<Run>;
}
