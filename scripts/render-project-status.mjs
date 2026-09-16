import { readFileSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { renderProjectStatus } from './lib/project-status.mjs';

const args = process.argv.slice(2);
if (args.length > 1 || (args.length === 1 && args[0] !== '--write')) {
  throw new Error('Usage: node scripts/render-project-status.mjs [--write]');
}
const data = JSON.parse(readFileSync(new URL('../docs/planning/project-data.json', import.meta.url), 'utf8'));
const content = renderProjectStatus(data);
if (args[0] === '--write') {
  writeFileSync(new URL('../docs/planning/current-status.md', import.meta.url), content, 'utf8');
  console.log(`Wrote ${fileURLToPath(new URL('../docs/planning/current-status.md', import.meta.url))}`);
} else {
  process.stdout.write(content);
}
