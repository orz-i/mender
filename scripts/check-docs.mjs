import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { existsSync, readFileSync } from 'node:fs';
import { dirname, isAbsolute, join, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { readJSON, walkFiles } from './lib/files.mjs';

const root = fileURLToPath(new URL('../', import.meta.url));
const archive = join(root, 'docs/archive/design-delivery-v1.1');
const manifest = readJSON(join(archive, 'manifest.json'));
const original = join(archive, manifest.original_directory);
const hash = (path) => createHash('sha256').update(readFileSync(path)).digest('hex');
const checksums = join(original, 'SHA256SUMS.txt');
assert.equal(hash(checksums), manifest.checksum_file_sha256, 'Original checksum list has changed');
const entries = readFileSync(checksums, 'utf8').trim().split(/\r?\n/);
for (const line of entries) {
  const [, expected, name] = line.match(/^([a-f0-9]{64})\s+\*?(.+)$/) ?? [];
  assert.ok(expected && name, `Invalid checksum record: ${line}`);
  const path = resolve(original, name);
  const within = relative(original, path);
  assert.ok(!within.startsWith('..') && !isAbsolute(within), 'Checksum path escapes original package');
  assert.equal(hash(path), expected, `Original file has changed: ${name}`);
}
assert.equal(entries.length, manifest.verified_files);
assert.equal(walkFiles(original).length, entries.length + 1, 'Unexpected file in original package');

const data = readJSON(join(root, 'docs/planning/project-data.json'));
const counts = { tasks: 92, requirements: 40, tests: 48, adrs: 20, risks: 18, launch_checks: 30, contexts: 8, governance_rules: 16, sources: 35 };
for (const [key, count] of Object.entries(counts)) assert.equal(data[key].length, count, `Baseline ${key} count`);
const taskIds = new Set(data.tasks.map((task) => task.id));
const requirementIds = new Set(data.requirements.map((row) => row[0]));
const testIds = new Set(data.tests.map((row) => row[0]));
assert.equal(taskIds.size, data.tasks.length, 'Duplicate task IDs');
for (const task of data.tasks) {
  for (const dependency of task.deps.split(';').filter(Boolean)) assert.ok(taskIds.has(dependency), `${task.id}: ${dependency}`);
  for (const requirement of task.req.split(';').filter(Boolean)) assert.ok(requirementIds.has(requirement), `${task.id}: ${requirement}`);
}
for (const rule of data.governance_rules) {
  for (const id of rule[3].split(';')) assert.ok(taskIds.has(id), `Unknown governance task ${id}`);
  for (const id of rule[4].split(';')) assert.ok(testIds.has(id), `Unknown governance test ${id}`);
}

let checkedLinks = 0;
for (const path of walkFiles(root)) {
  const name = relative(root, path).replaceAll('\\', '/');
  if (!name.endsWith('.md') || name.startsWith('docs/archive/')) continue;
  // Fenced snippets can contain example paths; only actual Markdown links are checked.
  const text = readFileSync(path, 'utf8').replace(/^```[^\n]*\n[\s\S]*?^```\s*$/gm, '');
  for (const match of text.matchAll(/!?\[[^\]]*\]\(([^)]+)\)/g)) {
    const target = match[1].replace(/^<|>$/g, '').split(/\s+"/)[0].split('#')[0];
    if (!target || /^(?:[a-z]+:|\/\/)/i.test(target)) continue;
    assert.ok(existsSync(resolve(dirname(path), decodeURIComponent(target))), `Broken link in ${name}: ${target}`);
    checkedLinks++;
  }
}
console.log(`PASS: ${entries.length} original SHA-256 checksums, baseline counts / task references and ${checkedLinks} local Markdown links`);
console.log('LIMIT: local link targets are checked; external URLs, Markdown anchors and Office/PDF layouts are not revalidated.');
