import { createConsoleConnectionsClient, createConsoleIdentityClient, MenderApiError } from '@mender/api-client';
import { ConnectionLoginRequiredError, type ConnectionGateway } from '../application/connection-gateway';

function readCSRFCookie() {
  const prefix = 'mender_csrf=';
  for (const part of document.cookie.split(';')) {
    const value = part.trim();
    if (value.startsWith(prefix)) return decodeURIComponent(value.slice(prefix.length));
  }
  return '';
}

function mapError(error: unknown): never {
  if (error instanceof MenderApiError && error.status === 401) throw new ConnectionLoginRequiredError();
  throw error;
}

export function createConnectionGateway(): ConnectionGateway {
  const identity = createConsoleIdentityClient();
  const connections = createConsoleConnectionsClient();
  return {
    async workspaces(signal) {
      try { return (await identity.listWorkspaces(signal)).map((item) => ({ id: item.workspaceId, role: item.role })); }
      catch (error) { return mapError(error); }
    },
    async list(workspaceId, signal) {
      try { return (await connections.list(workspaceId, signal)).map((item) => ({ id: item.connectionId, providerId: item.providerId, state: item.state, revision: item.revision, createdAt: item.createdAt, expiresAt: item.expiresAt })); }
      catch (error) { return mapError(error); }
    },
    async revoke(workspaceId, connectionId, signal) {
      const csrf = readCSRFCookie();
      if (!csrf) throw new Error('CSRF token unavailable');
      try {
        const item = await connections.revoke(workspaceId, connectionId, csrf, signal);
        return { id: item.connectionId, providerId: item.providerId, state: item.state, revision: item.revision, createdAt: item.createdAt, expiresAt: item.expiresAt };
      } catch (error) { return mapError(error); }
    },
  };
}
