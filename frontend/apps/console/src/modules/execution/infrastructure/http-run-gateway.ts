import { createRunsClient } from '@mender/api-client';
import type { RunGateway } from '../application/run-gateway';
import type { Run, RunArtifact, RunEvent } from '../domain/run';

function run(value: Awaited<ReturnType<ReturnType<typeof createRunsClient>['getRun']>>): Run {
  return {
    id: value.runId,
    workspaceId: value.workspaceId,
    state: value.executionState,
    version: value.version,
    createdAt: value.createdAt,
    updatedAt: value.updatedAt,
  };
}

function event(value: Awaited<ReturnType<ReturnType<typeof createRunsClient>['listRunEvents']>>['items'][number]): RunEvent {
  return {
    version: value.version,
    state: value.executionState,
    occurredAt: value.occurredAt,
    subjectId: value.subjectId,
    reason: value.reason,
  };
}

function artifact(value: Awaited<ReturnType<ReturnType<typeof createRunsClient>['listRunArtifacts']>>['items'][number]): RunArtifact {
  return {
    id: value.artifactId,
    kind: value.kind,
    mediaType: value.mediaType,
    sizeBytes: value.sizeBytes,
    createdAt: value.createdAt,
  };
}

export function createRunGateway(): RunGateway {
  const client = createRunsClient();
  return {
    async list(access, options, signal) {
      const page = await client.listRuns({
        workspaceId: access.workspaceId,
        token: access.machineToken,
        limit: options.limit,
        cursor: options.cursor,
        state: options.state,
        signal,
      });
      return { items: page.items.map(run), nextCursor: page.nextCursor };
    },
    async get(access, runId, signal) {
      return run(await client.getRun({ workspaceId: access.workspaceId, token: access.machineToken, runId, signal }));
    },
    async events(access, runId, signal) {
      const page = await client.listRunEvents({ workspaceId: access.workspaceId, token: access.machineToken, runId, signal });
      return { items: page.items.map(event), nextCursor: page.nextCursor, throughVersion: page.throughVersion };
    },
    async artifacts(access, runId, signal) {
      const page = await client.listRunArtifacts({ workspaceId: access.workspaceId, token: access.machineToken, runId, signal });
      return page.items.map(artifact);
    },
    async cancel(access, runId, reason, signal) {
      return run(await client.cancelRun({ workspaceId: access.workspaceId, token: access.machineToken, runId, reason, signal }));
    },
  };
}
