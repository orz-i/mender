import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { execFileSync } from 'node:child_process';

const versions = JSON.parse(readFileSync(new URL('../toolchain.versions.json', import.meta.url), 'utf8'));
const manifest = JSON.parse(readFileSync(new URL('../package.json', import.meta.url), 'utf8'));
assert.equal(process.versions.node, versions.node_exact_version, 'Use the Node version in .node-version');
assert.equal(manifest.packageManager, `pnpm@${versions.pnpm_exact_version}`);
assert.equal(manifest.engines.node, versions.node_exact_version);
assert.equal(manifest.engines.pnpm, versions.pnpm_exact_version);
assert.equal(readFileSync(new URL('../.node-version', import.meta.url), 'utf8').trim(), versions.node_exact_version);
const agent = process.env.npm_config_user_agent ?? '';
assert.ok(agent.startsWith(`pnpm/${versions.pnpm_exact_version} `), `Run with pnpm ${versions.pnpm_exact_version}`);
if (process.argv.includes('--go')) {
  const version = execFileSync('go', ['env', 'GOVERSION'], { encoding: 'utf8', env: { ...process.env, GOTOOLCHAIN: 'local' } }).trim();
  assert.equal(version, `go${versions.go_exact_version}`, 'Go version differs from the initialization baseline');
  assert.match(readFileSync(new URL('../backend/go.mod', import.meta.url), 'utf8'), new RegExp(`^go ${versions.go_exact_version.replaceAll('.', '\\.')}\\s*$`, 'm'));
}
console.log('PASS: pinned Node / pnpm toolchain' + (process.argv.includes('--go') ? ' / Go' : ''));
