import { spawn } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { setTimeout as delay } from 'node:timers/promises';
import { fileURLToPath } from 'node:url';
import { postgresPreflight, withIsolatedPostgres } from './lib/postgres-test.mjs';

const root = fileURLToPath(new URL('../', import.meta.url));
const flags = new Set(process.argv.slice(2));
if ([...flags].some((flag) => !['--check', '--pull'].includes(flag))) {
  console.error('Usage: pnpm test:integration:docker [--check] [--pull]');
  process.exitCode = 1;
} else {
  const controller = new AbortController();
  const interrupt = () => controller.abort();
  process.once('SIGINT', interrupt);
  process.once('SIGTERM', interrupt);
  const password = randomBytes(32).toString('hex');
  const safe = (text) => text.replaceAll(password, '[test-secret-redacted]').replace(/postgres(?:ql)?:\/\/[^\s]+/gu, '[database-url-redacted]');

  // Bounded capture; no shell, command echo, full docker inspect or credential files.
  function run(executable, args, options = {}) {
    return new Promise((resolve, reject) => {
      const env = { ...process.env, ...options.env };
      const child = spawn(executable, args, { cwd: root, env, shell: false, windowsHide: true });
      let stdout = '', stderr = '', stopped = false;
      const stop = () => { stopped = true; child.kill('SIGTERM'); };
      const timer = setTimeout(stop, options.timeoutMs ?? 10000);
      const collect = (stream) => (chunk) => {
        if (stream === 'stdout') stdout += chunk.toString(); else stderr += chunk.toString();
        if (stdout.length + stderr.length > 1048576) stop();
      };
      child.stdout.on('data', collect('stdout'));
      child.stderr.on('data', collect('stderr'));
      options.signal?.addEventListener('abort', stop, { once: true });
      if (options.signal?.aborted) stop();
      child.on('error', () => { clearTimeout(timer); options.signal?.removeEventListener('abort', stop); reject(new Error(`Cannot execute ${executable === 'docker' ? 'Docker CLI' : 'local test command'}.`)); });
      child.on('close', (code) => {
        clearTimeout(timer); options.signal?.removeEventListener('abort', stop);
        resolve({ code: stopped ? 1 : code ?? 1, stdout: safe(stdout), stderr: safe(stderr) });
      });
    });
  }

  try {
    const endpointOverride = process.env.DOCKER_CONTEXT ? undefined : process.env.DOCKER_HOST;
    if (flags.has('--check')) {
      await postgresPreflight(run, endpointOverride);
      console.log('PASS: local Docker endpoint and supported daemon. No database was created or tested.');
    } else {
      await withIsolatedPostgres({ run, nonce: randomBytes(16).toString('hex'), password, endpointOverride,
        pull: flags.has('--pull'), signal: controller.signal, log: console.log,
        sleep: (ms, signal) => delay(ms, undefined, { signal }),
        runTests: async (testEnv, signal) => {
          const result = await run(process.execPath, ['scripts/backend.mjs', 'test', '-tags=integration', '-count=1', '-timeout=120s', './tests/integration'], {
            env: { MENDER_DATABASE_URL: '', MENDER_ADMIN_DATABASE_URL: '', MENDER_RUN_API_ENABLED: 'false', MENDER_RUN_READ_API_ENABLED: 'false', ...testEnv },
            signal, timeoutMs: 150000,
          });
          process.stdout.write(result.stdout); process.stderr.write(result.stderr);
          return result.code;
        },
      });
      console.log('PASS: real isolated PostgreSQL suite and resource cleanup.');
    }
  } catch (error) {
    console.error(safe(error.message));
    process.exitCode = 1;
  } finally {
    process.removeListener('SIGINT', interrupt);
    process.removeListener('SIGTERM', interrupt);
  }
}
