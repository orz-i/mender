import { spawn } from 'node:child_process';

// No shell: arguments and signals go directly to the Go toolchain.
const child = spawn('go', process.argv.slice(2), {
  cwd: new URL('../backend/', import.meta.url),
  stdio: 'inherit',
  env: { ...process.env, GOTOOLCHAIN: 'local' },
});
child.on('error', (error) => {
  console.error(error.message);
  process.exitCode = 1;
});
child.on('exit', (code) => { process.exitCode = code ?? 1; });
for (const signal of ['SIGINT', 'SIGTERM']) {
  process.on(signal, () => { child.kill(signal); });
}
