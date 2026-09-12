export type LaunchWorkspaceRole = 'owner' | 'admin' | 'developer' | 'viewer';

export interface LaunchWorkspace { id: string; role: LaunchWorkspaceRole }

export interface LaunchOption {
  toolsetVersionId: string;
  toolId: string;
  toolVersion: string;
  toolVersionId: string;
  title: string;
  description: string;
  inputSchema: Record<string, unknown>;
  sideEffect: 'read_only' | 'write';
  idempotency: 'safe_read' | 'idempotent' | 'unsafe';
  connectionId: string;
  providerId: string;
  currency: string;
  reserveMicro: string;
}

export function canStartRuns(role: LaunchWorkspaceRole) {
  return role === 'owner' || role === 'admin' || role === 'developer';
}

export function launchOptionKey(option: LaunchOption) {
  return `${option.toolsetVersionId}/${option.toolVersionId}/${option.connectionId}`;
}
