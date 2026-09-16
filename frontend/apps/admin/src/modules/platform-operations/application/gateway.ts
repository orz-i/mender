import type { Result } from '../domain/workflow';
export interface Gateway { session(signal: AbortSignal): Promise<{userId: string}>; execute(command: string, values: Record<string,string>, signal: AbortSignal): Promise<Result> }
