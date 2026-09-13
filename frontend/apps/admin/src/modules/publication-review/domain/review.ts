export type ReviewWorkspaceRole = 'owner' | 'admin' | 'developer' | 'viewer';
export type PublicationReviewState = 'pending' | 'approved' | 'rejected' | 'consumed' | 'expired';

export interface ReviewWorkspace { id: string; role: ReviewWorkspaceRole }
export interface PublicationReview {
  id: string;
  targetKind: 'tool_version' | 'toolset';
  targetId: string;
  targetRevision: string;
  requesterUserId: string;
  state: PublicationReviewState;
  requestedAt: string;
  expiresAt: string;
  reviewerUserId: string | null;
  reviewedAt: string | null;
  decisionNote: string;
  consumedAt: string | null;
}
