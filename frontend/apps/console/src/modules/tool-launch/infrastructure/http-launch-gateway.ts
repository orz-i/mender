import { createConsoleIdentityClient, createConsoleLaunchClient, createConsoleStartRunClient, MenderApiError } from '@mender/api-client';
import { LaunchLoginRequiredError, type LaunchGateway } from '../application/launch-gateway';
import type { LaunchOption } from '../domain/launch';

function readCSRFCookie() {
  const prefix = 'mender_csrf=';
  for (const part of document.cookie.split(';')) {
    const value = part.trim();
    if (value.startsWith(prefix)) return decodeURIComponent(value.slice(prefix.length));
  }
  return '';
}

function mapError(error: unknown): never {
  if (error instanceof MenderApiError && error.status === 401) throw new LaunchLoginRequiredError();
  throw error;
}

function option(value: Awaited<ReturnType<ReturnType<typeof createConsoleLaunchClient>['list']>>[number]): LaunchOption {
  return {
    toolsetVersionId: value.toolsetVersionId, toolId: value.toolId, toolVersion: value.toolVersion, toolVersionId: value.toolVersionId,
    title: value.title, description: value.description, inputSchema: value.inputSchema, sideEffect: value.sideEffect, idempotency: value.idempotency,
    connectionId: value.connectionId, providerId: value.providerId, currency: value.currency, reserveMicro: value.reserveMicro,
  };
}

function idempotencyKey() {
  if (!globalThis.crypto?.randomUUID) throw new Error('安全随机数不可用');
  return `human-${globalThis.crypto.randomUUID()}`;
}

export function createLaunchGateway(): LaunchGateway {
  const identity = createConsoleIdentityClient();
  const catalog = createConsoleLaunchClient();
  const starter = createConsoleStartRunClient();
  return {
    async workspaces(signal) {
      try {
        return (await identity.listWorkspaces(signal)).map((item) => ({ id: item.workspaceId, role: item.role }));
      } catch (error) { return mapError(error); }
    },
    async options(workspaceId, signal) {
      try { return (await catalog.list(workspaceId, signal)).map(option); }
      catch (error) { return mapError(error); }
    },
    async prepare(workspaceId, selected, maxChargeMicro, signal) {
      const csrf = readCSRFCookie();
      if (!csrf) throw new Error('CSRF token unavailable');
      const key = idempotencyKey();
      try {
        const issued = await starter.issueDelegation(workspaceId, {
          toolsetVersionId: selected.toolsetVersionId, toolId: selected.toolId, toolVersion: selected.toolVersion, toolVersionId: selected.toolVersionId,
          connectionId: selected.connectionId, currency: selected.currency, maxChargeMicro, idempotencyKey: key,
        }, csrf, signal);
        return { delegationId: issued.delegationId, workspaceId, token: issued.token, idempotencyKey: key, expiresAt: issued.expiresAt, option: selected, maxChargeMicro };
      } catch (error) { return mapError(error); }
    },
    async submit(prepared, argumentsValue, signal) {
      try {
        return await starter.startRun(prepared.workspaceId, {
          delegationId: prepared.delegationId, workspaceId: prepared.workspaceId, token: prepared.token, expiresAt: prepared.expiresAt,
          toolsetVersionId: prepared.option.toolsetVersionId, toolId: prepared.option.toolId, toolVersion: prepared.option.toolVersion, toolVersionId: prepared.option.toolVersionId,
          connectionId: prepared.option.connectionId, currency: prepared.option.currency, maxChargeMicro: prepared.maxChargeMicro, idempotencyKey: prepared.idempotencyKey,
        }, argumentsValue, signal);
      } catch (error) { return mapError(error); }
    },
    async revoke(prepared, signal) {
      const csrf = readCSRFCookie();
      if (!csrf) throw new Error('CSRF token unavailable');
      try { await starter.revokeDelegation(prepared.workspaceId, prepared.delegationId, csrf, signal); }
      catch (error) { mapError(error); }
    },
  };
}
