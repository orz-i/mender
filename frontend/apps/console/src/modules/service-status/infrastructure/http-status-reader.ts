import { createHealthClient } from '@mender/api-client';
import type { ReadStatus } from '../application/read-status';

export function createStatusReader(): ReadStatus {
  const client = createHealthClient();
  return async (signal) => {
    const health = await client.getHealth(signal);
    return { name: health.service };
  };
}
