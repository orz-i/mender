export type ConnectionState = 'active' | 'expired' | 'revoked' | 'error';
export type WorkspaceRole = 'owner' | 'admin' | 'developer' | 'viewer';

export interface ConnectionSummary {
  id: string;
  providerId: string;
  state: ConnectionState;
  revision: number;
  createdAt: string;
  expiresAt: string;
}

export interface WorkspaceOption { id: string; role: WorkspaceRole }

export function canManageConnections(role: WorkspaceRole) { return role === 'owner' || role === 'admin'; }
