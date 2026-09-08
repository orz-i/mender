import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';

export function readJSON(path) {
  return JSON.parse(readFileSync(path, 'utf8'));
}

export function walkFiles(root, ignored = new Set(['.git', 'node_modules', 'dist', '.tmp', '.cache', '.anchor'])) {
  return readdirSync(root, { withFileTypes: true }).flatMap((entry) => {
    if (ignored.has(entry.name)) return [];
    const path = join(root, entry.name);
    return entry.isDirectory() ? walkFiles(path, ignored) : entry.isFile() ? [path] : [];
  });
}
