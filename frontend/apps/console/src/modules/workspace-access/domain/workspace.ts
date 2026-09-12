export type WorkspaceRole = 'owner' | 'admin' | 'developer' | 'viewer';

export interface WorkspaceAccess {
  id: string;
  role: WorkspaceRole;
}

export interface HumanSessionOverview {
  userId: string;
  workspaces: WorkspaceAccess[];
}
