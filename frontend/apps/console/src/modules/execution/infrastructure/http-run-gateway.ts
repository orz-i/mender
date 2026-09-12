import { createConsoleIdentityClient, createConsoleRunDelegationClient, createConsoleRunsClient, type ConsoleWorkspaceRecord, type RunDelegationScope } from '@mender/api-client';
import type { RunGateway } from '../application/run-gateway';
import type { Run, RunArtifact, RunArtifactDetail, RunEvent } from '../domain/run';

function run(value: Awaited<ReturnType<ReturnType<typeof createConsoleRunsClient>['getRun']>>): Run {
  return {
    id: value.runId,
    workspaceId: value.workspaceId,
    state: value.executionState,
    version: value.version,
    createdAt: value.createdAt,
    updatedAt: value.updatedAt,
  };
}

function artifactDetail(value: Awaited<ReturnType<ReturnType<typeof createConsoleRunsClient>['getRunArtifact']>>): RunArtifactDetail {
  return {
    ...artifact(value),
    content: value.content,
  };
}

function event(value: Awaited<ReturnType<ReturnType<typeof createConsoleRunsClient>['listRunEvents']>>['items'][number]): RunEvent {
  return {
    version: value.version,
    state: value.executionState,
    occurredAt: value.occurredAt,
    subjectId: value.subjectId,
    reason: value.reason,
  };
}

function artifact(value: Awaited<ReturnType<ReturnType<typeof createConsoleRunsClient>['listRunArtifacts']>>['items'][number]): RunArtifact {
  return {
    id: value.artifactId,
    kind: value.kind,
    mediaType: value.mediaType,
    sizeBytes: value.sizeBytes,
    createdAt: value.createdAt,
  };
}

function readCSRFCookie() {
  const prefix = 'mender_csrf=';
  for (const part of document.cookie.split(';')) {
    const value = part.trim();
    if (value.startsWith(prefix)) return decodeURIComponent(value.slice(prefix.length));
  }
  return '';
}

function scopesFor(role: ConsoleWorkspaceRecord['role']): RunDelegationScope[] {
  return role === 'viewer' ? ['run:read'] : ['run:read', 'run:cancel'];
}

export function createRunGateway(): RunGateway {
  const client = createConsoleRunsClient();
  const identity = createConsoleIdentityClient();
  const delegations = createConsoleRunDelegationClient();
  return {
    async connect(workspaceId, signal) {
      const workspaces = await identity.listWorkspaces(signal);
      const workspace = workspaces.find((item) => item.workspaceId === workspaceId);
      if (!workspace) throw new Error('当前登录用户无权访问该 Workspace');
      const csrf = readCSRFCookie();
      if (!csrf) throw new Error('CSRF token unavailable');
      const delegation = await delegations.issue(workspaceId, scopesFor(workspace.role), csrf, signal);
      return {
        workspaceId,
        delegatedToken: delegation.token,
        delegationId: delegation.delegationId,
        expiresAt: delegation.expiresAt,
        canCancel: delegation.scopes.includes('run:cancel'),
      };
    },
    async disconnect(access, signal) {
      const csrf = readCSRFCookie();
      if (!csrf) throw new Error('CSRF token unavailable');
      await delegations.revoke(access.workspaceId, access.delegationId, csrf, signal);
    },
    async list(access, options, signal) {
      const page = await client.listRuns({
        workspaceId: access.workspaceId,
        token: access.delegatedToken,
        limit: options.limit,
        cursor: options.cursor,
        state: options.state,
        signal,
      });
      return { items: page.items.map(run), nextCursor: page.nextCursor };
    },
    async get(access, runId, signal) {
      return run(await client.getRun({ workspaceId: access.workspaceId, token: access.delegatedToken, runId, signal }));
    },
    async events(access, runId, options, signal) {
      const page = await client.listRunEvents({
        workspaceId: access.workspaceId,
        token: access.delegatedToken,
        runId,
        cursor: options.cursor,
        limit: options.limit,
        expectedThroughVersion: options.expectedThroughVersion,
        signal,
      });
      return { items: page.items.map(event), nextCursor: page.nextCursor, throughVersion: page.throughVersion };
    },
    async artifacts(access, runId, signal) {
      const page = await client.listRunArtifacts({ workspaceId: access.workspaceId, token: access.delegatedToken, runId, signal });
      return page.items.map(artifact);
    },
    async artifact(access, runId, artifactId, signal) {
      return artifactDetail(await client.getRunArtifact({ workspaceId: access.workspaceId, token: access.delegatedToken, runId, artifactId, signal }));
    },
    async cancel(access, runId, reason, signal) {
      if (!access.canCancel) throw new Error('当前 Workspace 角色没有 Run 取消权限');
      return run(await client.cancelRun({ workspaceId: access.workspaceId, token: access.delegatedToken, runId, reason, signal }));
    },
  };
}
