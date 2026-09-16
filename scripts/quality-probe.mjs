import { spawn } from 'node:child_process';
import { readFileSync, statSync } from 'node:fs';
import { setTimeout as sleep } from 'node:timers/promises';
import { runProbeJob, renderQualityReport, validateProbeConfig } from './lib/quality-probe.mjs';

const controller = new AbortController();
const stop = () => controller.abort();
process.once('SIGINT', stop); process.once('SIGTERM', stop);
function probe(config, signal) {
  return new Promise((resolve) => {
    const child = spawn(process.execPath, ['scripts/backend.mjs', 'run', './cmd/mender', '--endpoint', config.endpoint,
      '--key-file', config.key_file, '--baseline', config.baseline, '--timeout', '15s', 'health'],
    { cwd: new URL('../', import.meta.url), shell: false, windowsHide: true });
    let stdout = '', stderr = '', stopped = false;
    const cancel = () => { stopped = true; child.kill('SIGTERM'); };
    const timer = setTimeout(cancel, 60000);
    for (const [stream, type] of [[child.stdout, 'out'], [child.stderr, 'err']]) stream.on('data', (chunk) => {
      if (type === 'out') stdout += chunk.toString(); else stderr += chunk.toString();
      if (stdout.length + stderr.length > 16384) cancel();
    });
    signal?.addEventListener('abort', cancel, { once: true });
    if (signal?.aborted) cancel();
    child.on('error', () => { clearTimeout(timer); signal?.removeEventListener('abort', cancel); resolve({ code: 1, stdout: '', stderr: '' }); });
    child.on('close', (code) => { clearTimeout(timer); signal?.removeEventListener('abort', cancel);
      // go run may append its own nonzero-exit trailer, never arbitrary server content.
      resolve({ code: stopped ? 1 : code ?? 1, stdout,
        stderr: /^TOOL_CONTRACT_DRIFT_REVIEW_REQUIRED\r?\nexit status 1\s*$/u.test(stderr) ? 'TOOL_CONTRACT_DRIFT_REVIEW_REQUIRED' : '' });
    });
  });
}
try {
  const [command, path, ...extra] = process.argv.slice(2);
  if (extra.length || !['run', 'report'].includes(command) || !path || !statSync(path).isFile() || statSync(path).size > 128 * 1024) throw new Error('INVALID_QUALITY_INPUT');
  const data = JSON.parse(readFileSync(path, 'utf8'));
  if (command === 'report') process.stdout.write(renderQualityReport(data));
  else {
    validateProbeConfig(data);
    const report = await runProbeJob(data, { probe, sleep: (ms, signal) => sleep(ms, undefined, { signal }), now: () => new Date(), signal: controller.signal });
    process.stdout.write(`${JSON.stringify(report, null, 2)}\n`);
    if (report.status !== 'passed') process.exitCode = 1;
  }
} catch { console.error('QUALITY_JOB_FAILED'); process.exitCode = 1; }
finally { process.removeListener('SIGINT', stop); process.removeListener('SIGTERM', stop); }
