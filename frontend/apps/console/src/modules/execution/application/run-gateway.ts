import type { Run, RunArtifact, RunEvent, RunState } from '../domain/run';

export interface RunAccess {
  workspaceId: string;
  machineToken: string;
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
  list(access: RunAccess, options: { state?: RunState; cursor?: string; limit?: number }, signal?: AbortSignal): Promise<RunPage>;
  get(access: RunAccess, runId: string, signal?: AbortSignal): Promise<Run>;
  events(access: RunAccess, runId: string, signal?: AbortSignal): Promise<RunEvents>;
  artifacts(access: RunAccess, runId: string, signal?: AbortSignal): Promise<RunArtifact[]>;
  cancel(access: RunAccess, runId: string, reason: string, signal?: AbortSignal): Promise<Run>;
}
