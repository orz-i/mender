export interface HealthResponse {
  service: 'mender-api';
  status: 'ok';
  stage: string;
}

export function createHealthClient(baseUrl = '', fetcher: typeof fetch = fetch) {
  return {
    async getHealth(signal?: AbortSignal): Promise<HealthResponse> {
      const timeout = AbortSignal.timeout(5_000);
      const response = await fetcher(`${baseUrl}/healthz`, {
        headers: { Accept: 'application/json' },
        signal: signal ? AbortSignal.any([signal, timeout]) : timeout,
      });
      if (!response.ok) {
        throw new Error(`服务暂时不可用（HTTP ${response.status}）`);
      }
      const body: unknown = await response.json();
      if (
        typeof body !== 'object' || body === null ||
        !('service' in body) || body.service !== 'mender-api' ||
        !('status' in body) || body.status !== 'ok' ||
        !('stage' in body) || typeof body.stage !== 'string'
      ) {
        throw new Error('服务返回了无法识别的响应');
      }
      return { service: body.service, status: body.status, stage: body.stage };
    },
  };
}
