import { createConsoleIdentityClient, MenderApiError } from '@mender/api-client';
import { ConsoleUnauthenticatedError, type WorkspaceGateway } from '../application/workspace-gateway';

function readCSRFCookie() {
  const prefix = 'mender_csrf=';
  for (const part of document.cookie.split(';')) {
    const value = part.trim();
    if (value.startsWith(prefix)) return decodeURIComponent(value.slice(prefix.length));
  }
  return '';
}

export function createWorkspaceGateway(): WorkspaceGateway {
  const client = createConsoleIdentityClient();
  const mapError = (error: unknown): never => {
    if (error instanceof MenderApiError && error.status === 401) throw new ConsoleUnauthenticatedError();
    throw error;
  };
  return {
    async load(signal) {
      try {
        const [session, workspaces] = await Promise.all([client.getSession(signal), client.listWorkspaces(signal)]);
        return { userId: session.userId, workspaces: workspaces.map((item) => ({ id: item.workspaceId, role: item.role })) };
      } catch (error) {
        return mapError(error);
      }
    },
    async logout(signal) {
      const csrf = readCSRFCookie();
      if (!csrf) throw new Error('CSRF token unavailable');
      try {
        await client.logout(csrf, signal);
      } catch (error) {
        mapError(error);
      }
    },
  };
}
