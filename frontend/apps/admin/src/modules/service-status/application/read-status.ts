import type { ServiceStatus } from '../domain/service-status';

// The consuming module owns this port. The transport is assembled by app/.
export type ReadStatus = (signal?: AbortSignal) => Promise<ServiceStatus>;
