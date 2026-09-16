import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { createEvidenceReader, validateProjectStatus } from './lib/project-status-validation.mjs';

const root = fileURLToPath(new URL('../', import.meta.url));
const data = JSON.parse(readFileSync(new URL('../docs/planning/project-data.json', import.meta.url), 'utf8'));
const generated = readFileSync(new URL('../docs/planning/current-status.md', import.meta.url), 'utf8');
const errors = validateProjectStatus(data, { readEvidence: createEvidenceReader(root), generated });
if (errors.length > 0) throw new Error(`Project status drift:\n${errors.join('\n')}`);
console.log(`PASS: ${data.tasks.length} canonical tasks, implementation/verification/acceptance separation, evidence digests and generated status`);
console.log('LIMIT: structural checks do not execute business tests or attest human approval, external clients, live payments or remote merge protection.');
