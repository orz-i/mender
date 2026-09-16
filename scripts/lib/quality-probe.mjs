// Operator-side, bounded read-only scheduling. This is not a Worker executor,
// an upstream functional invocation, or an automatic publication approval.
const fail = (ok, message) => { if (!ok) throw new Error(message); };
const exact = (value, keys) => value && typeof value === 'object' && !Array.isArray(value)
  && Object.keys(value).sort().join('|') === [...keys].sort().join('|');

export function validateProbeConfig(config) {
  fail(exact(config, ['endpoint', 'key_file', 'baseline', 'samples', 'interval_ms']), 'INVALID_PROBE_CONFIG');
  const url = new URL(config.endpoint);
  fail(url.username === '' && url.password === '' && url.search === '' && url.hash === ''
    && /^\/mcp\/v1\/workspaces\/[A-Za-z0-9_-]{1,128}(\/toolsets\/[A-Za-z0-9_-]{1,128})?$/u.test(url.pathname)
    && (url.protocol === 'https:' || url.protocol === 'http:' && ['localhost', '127.0.0.1', '[::1]'].includes(url.hostname)), 'INVALID_PROBE_ENDPOINT');
  for (const path of [config.key_file, config.baseline]) fail(typeof path === 'string' && path.trim() !== ''
    && path.length <= 1024 && ![...path].some((c) => c.codePointAt(0) < 32), 'INVALID_PROBE_FILE');
  fail(Number.isInteger(config.samples) && config.samples >= 1 && config.samples <= 60, 'INVALID_SAMPLE_COUNT');
  fail(Number.isInteger(config.interval_ms) && config.interval_ms >= 5000 && config.interval_ms <= 60000, 'INVALID_PROBE_INTERVAL');
}

export async function runProbeJob(config, { probe, sleep, now, signal }) {
  validateProbeConfig(config);
  const samples = [];
  for (let index = 0; index < config.samples; index++) {
    if (signal?.aborted) throw new Error('PROBE_ABORTED');
    const start = now();
    let result;
    try { result = await probe(config, signal); } catch { result = { code: 1, stdout: '', stderr: '' }; }
    const sample = { at: start.toISOString(), elapsed_ms: Math.max(0, now().getTime() - start.getTime()),
      state: 'unavailable', tool_count: null, contract_sha256: null };
    if (result.code === 0) {
      try {
        const row = JSON.parse(result.stdout);
        if (exact(row, ['status', 'contract_sha256', 'tool_count', 'business_tools_called', 'baseline_compared'])
          && row.status === 'reachable' && row.business_tools_called === 0 && row.baseline_compared === true
          && Number.isInteger(row.tool_count) && row.tool_count >= 0 && row.tool_count <= 2000
          && /^[a-f0-9]{64}$/u.test(row.contract_sha256)) {
          sample.state = 'unchanged'; sample.tool_count = row.tool_count; sample.contract_sha256 = row.contract_sha256;
        }
      } catch { /* Untrusted output is never copied into the report. */ }
    } else if (result.stderr?.trim() === 'TOOL_CONTRACT_DRIFT_REVIEW_REQUIRED') sample.state = 'contract-drift';
    samples.push(sample);
    if (index + 1 < config.samples) await sleep(config.interval_ms, signal);
  }
  return { schema_version: 1, probe_kind: 'mcp-discovery-only', business_tools_called: 0,
    status: samples.every((s) => s.state === 'unchanged') ? 'passed' : 'failed', samples };
}

export function renderQualityReport(report) {
  fail(exact(report, ['schema_version', 'probe_kind', 'business_tools_called', 'status', 'samples'])
    && report.schema_version === 1 && report.probe_kind === 'mcp-discovery-only' && report.business_tools_called === 0
    && Array.isArray(report.samples) && report.samples.length >= 1 && report.samples.length <= 60, 'INVALID_QUALITY_REPORT');
  const rows = report.samples.map((row) => {
    fail(exact(row, ['at', 'elapsed_ms', 'state', 'tool_count', 'contract_sha256'])
      && typeof row.at === 'string' && /^\d{4}-\d{2}-\d{2}T[\d:.]+Z$/u.test(row.at) && Number.isFinite(Date.parse(row.at))
      && Number.isFinite(row.elapsed_ms) && row.elapsed_ms >= 0 && row.elapsed_ms <= 120000
      && ['unchanged', 'contract-drift', 'unavailable'].includes(row.state)
      && (row.tool_count === null || Number.isInteger(row.tool_count) && row.tool_count >= 0 && row.tool_count <= 2000)
      && (row.contract_sha256 === null || /^[a-f0-9]{64}$/u.test(row.contract_sha256)), 'INVALID_QUALITY_SAMPLE');
    fail(row.state !== 'unchanged' || row.tool_count !== null && row.contract_sha256 !== null, 'MISSING_QUALITY_FACTS');
    return `<tr><td>${row.at}</td><td>${row.state}</td><td>${row.elapsed_ms}</td><td>${row.tool_count ?? '—'}</td><td>${row.contract_sha256 ?? '—'}</td></tr>`;
  });
  fail(report.status === (report.samples.every((s) => s.state === 'unchanged') ? 'passed' : 'failed'), 'QUALITY_STATUS_MISMATCH');
  return `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'"><title>Mender 只读发布质量报告</title><style>body{font:16px system-ui;margin:32px;color:#203444}table{border-collapse:collapse;max-width:100%}td,th{border:1px solid #ddd;padding:10px;text-align:left;overflow-wrap:anywhere}p{max-width:75ch}td:last-child{font-family:monospace;max-width:20ch}</style><h1>Mender 只读发布质量报告</h1><p>结果：${report.status}；样本：${rows.length}。仅认证 MCP 发现端点和已审合同快照，不试调用业务工具，不自动更新基线，不代表上游任务功能、发布批准或生产就绪。</p><table><thead><tr><th>UTC 时间</th><th>状态</th><th>耗时 ms</th><th>工具数</th><th>合同 SHA-256</th></tr></thead><tbody>${rows.join('')}</tbody></table></html>\n`;
}
