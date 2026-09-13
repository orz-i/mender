import type { PublicationReview, ReviewWorkspace } from '../domain/review';

export class ReviewLoginRequiredError extends Error {}

export interface ReviewGateway {
  workspaces(signal?: AbortSignal): Promise<ReviewWorkspace[]>;
  list(workspaceId: string, signal?: AbortSignal): Promise<PublicationReview[]>;
  approve(workspaceId: string, approvalId: string, note: string, signal?: AbortSignal): Promise<PublicationReview>;
  reject(workspaceId: string, approvalId: string, note: string, signal?: AbortSignal): Promise<PublicationReview>;
}
