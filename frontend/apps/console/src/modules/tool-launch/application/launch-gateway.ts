import type { ExecutionRiskDecision, LaunchOption, LaunchWorkspace } from '../domain/launch';

export class LaunchLoginRequiredError extends Error {}

export interface PreparedRunStart {
  delegationId: string;
  workspaceId: string;
  token: string;
  idempotencyKey: string;
  expiresAt: string;
  option: LaunchOption;
  maxChargeMicro: string;
  argumentsHash: string;
}

export interface LaunchRiskReview {
  idempotencyKey: string;
  decision: ExecutionRiskDecision;
  confirmationId: string | null;
  confirmationExpiresAt: string | null;
}

export interface StartedRun {
  runId: string;
  replayed: boolean;
  requestId: string;
}

export interface LaunchGateway {
  workspaces(signal?: AbortSignal): Promise<LaunchWorkspace[]>;
  options(workspaceId: string, signal?: AbortSignal): Promise<LaunchOption[]>;
  preview(workspaceId: string, option: LaunchOption, argumentsValue: Record<string, unknown>, signal?: AbortSignal): Promise<LaunchRiskReview>;
  confirm(workspaceId: string, option: LaunchOption, argumentsValue: Record<string, unknown>, idempotencyKey: string, signal?: AbortSignal): Promise<LaunchRiskReview>;
  prepare(workspaceId: string, option: LaunchOption, maxChargeMicro: string, argumentsHash: string, idempotencyKey: string, signal?: AbortSignal): Promise<PreparedRunStart>;
  submit(prepared: PreparedRunStart, argumentsValue: Record<string, unknown>, signal?: AbortSignal): Promise<StartedRun>;
  revoke(prepared: PreparedRunStart, signal?: AbortSignal): Promise<void>;
}
