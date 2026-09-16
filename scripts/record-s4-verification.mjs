// Execute one fixed verification and record its actual log. Does not approve
// tasks or gates and never changes implementation/acceptance fields.
import { spawn, spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

const commands = {
  tools: 'pnpm test:s4:tools',
  project: 'pnpm check',
  postgres: 'pnpm test:integration:docker --browser --recovery',
  dependencies: 'pnpm check:dependencies',
};
const [mode, flag, ...extra] = process.argv.slice(2);
const root = fileURLToPath(new URL('../', import.meta.url));
const hash = (value) => createHash('sha256').update(value).digest('hex');
const git = (args) => {
  const result = spawnSync('git', args, { cwd: root, encoding: 'utf8', shell: false, windowsHide: true, maxBuffer: 8 * 1024 * 1024 });
  if (result.status !== 0) throw new Error('Git evidence unavailable');
  return result.stdout;
};
const sourceIdentity = () => {
  const groups = ['backend', 'frontend', 'scripts', 'contracts', 'package.json', 'pnpm-lock.yaml', '.github/workflows/ci.yml'];
  const tracked = git(['ls-files', '-z', '--', ...groups]).split('\0').filter(Boolean);
  const added = git(['ls-files', '--others', '--exclude-standard', '-z', '--', ...groups]).split('\0').filter(Boolean);
  const files = [...new Set([...tracked, ...added])].sort().map((path) => ({ path, sha256: hash(readFileSync(new URL('../' + path, import.meta.url))) }));
  return { head: git(['rev-parse', 'HEAD']).trim(), source_sha256: hash(JSON.stringify(files)), files };
};
if (!Object.hasOwn(commands, mode) || flag !== '--record' || extra.length) {
  console.error('Usage: node scripts/record-s4-verification.mjs tools|project|postgres|dependencies --record');
  process.exitCode = 1;
} else {
  const before = sourceIdentity();
  const started = new Date();
  const command = commands[mode];
  const executable = process.platform === 'win32' ? 'pwsh' : 'pnpm';
  const args = process.platform === 'win32' ? ['-NoLogo', '-NoProfile', '-NonInteractive', '-Command', command] : command.split(' ').slice(1);
  const child = spawn(executable, args, { cwd: root, shell: false, windowsHide: true });
  let stdout = '', stderr = '', aborted = false;
  const stop = () => { aborted = true; child.kill('SIGTERM'); };
  const timer = setTimeout(stop, 600000);
  process.once('SIGINT', stop); process.once('SIGTERM', stop);
  const redact = (s) => s.replace(/postgres(?:ql)?:\/\/[^\s]+/gu, '[database-url-redacted]')
    .replace(/mender_live_[a-zA-Z0-9._-]+/gu, '[machine-key-redacted]');
  child.stdout.on('data', (b) => { stdout += b.toString(); if (stdout.length + stderr.length > 4 * 1024 * 1024) stop(); });
  child.stderr.on('data', (b) => { stderr += b.toString(); if (stdout.length + stderr.length > 4 * 1024 * 1024) stop(); });
  child.once('error', () => { aborted = true; });
  child.once('close', (code) => {
    clearTimeout(timer); process.removeListener('SIGINT', stop); process.removeListener('SIGTERM', stop);
    const finished = new Date();
    const after = sourceIdentity();
    const unchanged = before.head === after.head && before.source_sha256 === after.source_sha256;
    const exitCode = aborted || !unchanged ? 1 : code ?? 1;
    const log = `COMMAND: ${command}\nSTARTED: ${started.toISOString()}\nFINISHED: ${finished.toISOString()}\nSOURCE: ${before.head}\nSOURCE_SHA256: ${before.source_sha256}\n\nSTDOUT\n${redact(stdout)}\nSTDERR\n${redact(stderr)}\nSOURCE_UNCHANGED: ${unchanged}\nEXIT_CODE: ${exitCode}\n`;
    const dir = 'docs/engineering/verification/s4-closeout';
    mkdirSync(new URL('../' + dir + '/', import.meta.url), { recursive: true });
    const logPath = `${dir}/${mode}.txt`;
    writeFileSync(new URL('../' + logPath, import.meta.url), log);
    const receipt = { record_type: 'suite-verification', command, mode, result: exitCode === 0 ? 'passed' : 'failed',
      exit_code: exitCode, environment: `${process.platform}/${process.arch}; local Mender; isolated fixtures only`,
      head: before.head, source_sha256: before.source_sha256, source_unchanged: unchanged,
      started_at: started.toISOString(), finished_at: finished.toISOString(), duration_ms: finished - started,
      log: { path: logPath, sha256: hash(log) }, files: before.files,
      limitations: 'Actual automated execution only; does not attest external clients, production, business licenses or human approval.' };
    writeFileSync(new URL(`../${dir}/${mode}.json`, import.meta.url), JSON.stringify(receipt, null, 2) + '\n');
    process.stdout.write(redact(stdout)); process.stderr.write(redact(stderr));
    console.log(`Recorded ${mode}: ${receipt.result}; source unchanged=${unchanged}; ${finished - started}ms`);
    process.exitCode = exitCode;
  });
}
