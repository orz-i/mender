import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';
import { basename, join, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { parse } from 'yaml';
import { readJSON, walkFiles } from './lib/files.mjs';

const root = fileURLToPath(new URL('../', import.meta.url));
const workspace = parse(readFileSync(join(root, 'pnpm-workspace.yaml'), 'utf8'));
assert.deepEqual(workspace.packages, ['frontend/apps/*', 'frontend/packages/*']);
assert.equal(workspace.sharedWorkspaceLockfile, true);
assert.equal(workspace.strictPeerDependencies, true);
const directories = ['frontend/apps', 'frontend/packages'].flatMap((group) =>
  readdirSync(join(root, group), { withFileTypes: true }).filter((entry) => entry.isDirectory()).map((entry) => join(root, group, entry.name)),
);
const packages = directories.map((directory) => ({ directory, manifest: readJSON(join(directory, 'package.json')) }));
const names = packages.map(({ manifest }) => manifest.name);
assert.equal(new Set(names).size, names.length, 'Duplicate workspace package name');
for (const required of ['console', 'admin', 'ui', 'api-client', 'config']) {
  assert.ok(names.includes(`@mender/${required}`), `Missing @mender/${required}`);
}
for (const { manifest } of packages) {
  assert.match(manifest.name, /^@mender\//);
  assert.equal(manifest.private, true, `${manifest.name} must remain private`);
  assert.ok(manifest.scripts?.typecheck, `Missing typecheck in ${manifest.name}`);
  if (['@mender/console', '@mender/admin'].includes(manifest.name)) {
    for (const script of ['dev', 'build', 'preview']) assert.ok(manifest.scripts[script], `Missing ${script}`);
  }
  for (const group of ['dependencies', 'devDependencies', 'peerDependencies']) {
    for (const [name, version] of Object.entries(manifest[group] ?? {})) {
      if (name.startsWith('@mender/')) {
        assert.ok(names.includes(name), `Unknown workspace dependency ${name}`);
        assert.ok(version.startsWith('workspace:'), `${manifest.name}: ${name} must use workspace:`);
      }
    }
  }
}
const forbidden = new Set(['package-lock.json', 'npm-shrinkwrap.json', 'yarn.lock', 'bun.lock', 'bun.lockb']);
const files = walkFiles(root).filter((path) => !relative(root, path).replaceAll('\\', '/').startsWith('docs/archive/'));
assert.deepEqual(files.filter((path) => basename(path) === 'pnpm-lock.yaml'), [resolve(root, 'pnpm-lock.yaml')], 'Only the root pnpm lockfile is allowed');
assert.deepEqual(files.filter((path) => forbidden.has(basename(path))), [], 'Unexpected package manager lockfile');
console.log(`PASS: ${packages.length} private workspace packages, workspace: dependencies, required scripts and one lockfile`);
console.log('LIMIT: this manifest check is not a resolved Go / TypeScript architecture graph check.');
