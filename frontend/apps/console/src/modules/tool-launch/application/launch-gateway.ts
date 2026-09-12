import type { LaunchOption, LaunchWorkspace } from '../domain/launch';

export class LaunchLoginRequiredError extends Error {}

export interface PreparedRunStart {
  delegationId: string;
  workspaceId: string;
  token: string;
  idempotencyKey: string;
  expiresAt: string;
  option: LaunchOption;
  maxChargeMicro: string;
}

export interface StartedRun {
  runId: string;
  replayed: boolean;
  requestId: string;
}

export interface LaunchGateway {
  workspaces(signal?: AbortSignal): Promise<LaunchWorkspace[]>;
  options(workspaceId: string, signal?: AbortSignal): Promise<LaunchOption[]>;
  prepare(workspaceId: string, option: LaunchOption, maxChargeMicro: string, signal?: AbortSignal): Promise<PreparedRunStart>;
  submit(prepared: PreparedRunStart, argumentsValue: Record<string, unknown>, signal?: AbortSignal): Promise<StartedRun>;
  revoke(prepared: PreparedRunStart, signal?: AbortSignal): Promise<void>;
}
