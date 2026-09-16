import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { fileURLToPath } from 'node:url';
import { parse } from 'yaml';
import { renderProjectStatus, taskStatus } from './lib/project-status.mjs';
import { createEvidenceReader, evidenceHash, isSafeEvidencePath, validateProjectStatus } from './lib/project-status-validation.mjs';

const root = fileURLToPath(new URL('../', import.meta.url));
const actualRead = createEvidenceReader(root);
const baseline = JSON.parse(actualRead('docs/planning/project-data.json'));
const target = (data, id = 'S4-03') => data.tasks.find((task) => task.id === id);

function fixture() {
  const data = structuredClone(baseline);
  const files = new Map();
  const readEvidence = (path) => files.has(path) ? files.get(path) : actualRead(path);
  const add = (path, value) => {
    const body = Buffer.from(typeof value === 'string' ? value : JSON.stringify(value));
    files.set(path, body);
    return { path, sha256: evidenceHash(body) };
  };
  return { data, files, readEvidence, add, run: (generated) => validateProjectStatus(data, { readEvidence, generated }) };
}
function rejects(mutator, code) {
  const f = fixture(); mutator(f);
  assert.ok(f.run().some((error) => error.startsWith(`${code}:`)), f.run().join('\n'));
}
function makeAccepted(f, id = 'S1-01') {
  const task = target(f.data, id);
  const log = f.add('docs/planning/fixtures/verification-log.txt', 'test-only simulated command output\n');
  const receipt = f.add('docs/planning/fixtures/task-verification.json', {
    record_type: 'task-verification', task_id: id, command: 'fixture command',
    environment: 'in-memory test fixture (not a real acceptance record)',
    head: baseline.execution_tracking.reviewed_head, finished_at: '2026-09-16T08:00:00Z',
    result: 'passed', exit_code: 0, log,
  });
  const acceptance = f.add('docs/planning/fixtures/task-acceptance.json', {
    record_type: 'task-acceptance', task_id: id, decision: 'accepted',
    coverage: 'task-full-scope', approved_by: 'test-fixture-reviewer', approved_at: '2026-09-16T08:30:00Z',
    scope: 'hypothetical full original task; no repository state is changed', verification_receipt: receipt,
  });
  Object.assign(task.execution, { implementation: 'implemented', verification: 'passed', acceptance: 'accepted',
    remaining: [], verification_receipt: receipt, acceptance_record: acceptance });
  task.status = taskStatus(task.execution); task.progress = 1;
}

test('current 92-task plan, historical snapshots and generated page are coherent', () => {
  assert.deepEqual(fixture().run(actualRead('docs/planning/current-status.md').toString('utf8')), []);
  assert.equal(baseline.tasks.length, 92);
  assert.ok(baseline.tasks.every((task) => task.execution.acceptance !== 'accepted' && task.progress === null));
  for (const task of baseline.tasks) assert.deepEqual(task.baseline_execution, { status: '未开始', progress: 0, evidence: '' });
});
test('original scope can eventually be accepted with distinct execution and acceptance receipts', () => {
  const f = fixture(); makeAccepted(f); assert.deepEqual(f.run(), []);
});
test('missing task and duplicate task cannot silently shrink the denominator', () => {
  rejects(({ data }) => { data.tasks.pop(); }, 'TASK_SET');
  rejects(({ data }) => { data.tasks[1] = structuredClone(data.tasks[0]); }, 'TASK_SET');
});
test('new S4-19 or S4-E is not an original delivery task', () => {
  for (const id of ['S4-19', 'S4-E']) rejects(({ data }) => { target(data).id = id; }, 'TASK_SET');
});
test('scope, estimate and dependency changes require baseline review', () => {
  rejects(({ data }) => { target(data).effort += 1; }, 'PLAN_DRIFT');
  rejects(({ data }) => { target(data).acceptance = 'only a backend test is sufficient'; }, 'PLAN_DRIFT');
});
test('canonical alias mapping cannot be reassigned or expanded', () => {
  rejects(({ data }) => { data.execution_tracking.canonical_aliases['S4-A'] = 'S4-10'; }, 'ALIAS_DRIFT');
  rejects(({ data }) => { data.execution_tracking.canonical_aliases['S4-E'] = 'S4-03'; }, 'ALIAS_DRIFT');
});
test('scoped-complete work cannot be reset to an unstarted task', () => {
  rejects(({ data }) => {
    const task = target(data); Object.assign(task.execution, { implementation: 'not_started', verification: 'not_run' });
    task.status = taskStatus(task.execution);
  }, 'STALE_NOT_STARTED');
});
test('implementation and top-level projections cannot contradict each other', () => {
  rejects(({ data }) => { target(data).status = '已完成'; }, 'STATUS_PROJECTION');
  rejects(({ data }) => { target(data).evidence = ''; }, 'EVIDENCE_PROJECTION');
});
test('unaccepted work has null progress rather than invented zero or 100 percent', () => {
  for (const progress of [0, 0.5, 1]) rejects(({ data }) => { target(data).progress = progress; }, 'UNKNOWN_PROGRESS');
});
test('scoped implementation cannot be promoted to complete without full acceptance', () => {
  rejects(({ data }) => { target(data).execution.acceptance = 'accepted'; }, 'FULL_ACCEPTANCE');
});
test('source fixture names and COMPLETE evidence are not fresh test receipts', () => {
  rejects(({ data }) => { const e = target(data).execution; e.verification = 'passed'; e.verification_receipt = null; }, 'EVIDENCE_REF');
  rejects(({ data }) => {
    const e = target(data).execution; e.verification = 'passed'; e.verification_receipt = e.evidence[0];
  }, 'VERIFICATION_RECEIPT');
});
test('a full acceptance record must be task-scoped and tied to execution evidence', () => {
  rejects((f) => {
    makeAccepted(f); target(f.data, 'S1-01').execution.acceptance_record = f.add('docs/planning/fixtures/wrong-task.json', {
      record_type: 'task-acceptance', task_id: 'S4-06', decision: 'accepted', coverage: 'task-full-scope',
    });
  }, 'ACCEPTANCE_RECEIPT');
});
test('G3 internal decision cannot approve arbitrary original tasks', () => {
  rejects(({ data }) => {
    const e = target(data).execution; e.acceptance = 'scoped_go';
    e.acceptance_record = target(data, 'S3-18').execution.acceptance_record;
  }, 'SCOPED_DECISION');
});
test('unreviewed work cannot claim verified or accepted execution', () => {
  rejects(({ data }) => {
    const task = target(data, 'S4-08');
    task.execution.implementation = 'not_reviewed';
    task.execution.verification = 'passed';
    task.status = taskStatus(task.execution);
  }, 'UNREVIEWED_CLAIM');
});
test('remaining UI, operational and external acceptance conditions cannot be erased', () => {
  rejects(({ data }) => { target(data, 'S4-10').execution.remaining = []; }, 'MISSING_REMAINING');
  rejects(({ data }) => { target(data, 'S4-06').execution.remaining = []; }, 'MISSING_REMAINING');
});
test('evidence drift and missing files fail instead of auto-refreshing their hashes', () => {
  rejects(({ data, files }) => { files.set(target(data).execution.evidence[0].path, Buffer.from('{}')); }, 'EVIDENCE_DRIFT');
  rejects(({ data }) => { target(data).execution.evidence[0].path = 'docs/planning/file-that-does-not-exist.txt'; }, 'EVIDENCE_READ');
});
test('canonical S4 evidence cannot be dropped while retaining a completion claim', () => {
  rejects(({ data }) => {
    const t = target(data); t.execution.evidence = []; t.evidence = '';
  }, 'S4_BINDING');
});
test('malformed evidence and duplicate references yield actionable errors', () => {
  rejects(({ data }) => { target(data).execution.evidence = [null]; }, 'EVIDENCE_REF');
  rejects(({ data }) => { target(data).execution.evidence.push(target(data).execution.evidence[0]); }, 'DUPLICATE_EVIDENCE');
});
test('path validation blocks traversal, absolute paths, alternate streams and aliases', () => {
  for (const path of ['../x', '/x', 'C:/x', 'docs/../x', 'docs//x', 'docs/./x', 'docs/x:stream', 'docs\\x', 'docs/x.', 'docs/x ', 'docs/\u0000x', 'docs/\nx']) {
    assert.equal(isSafeEvidencePath(path), false, path);
    rejects(({ data }) => { target(data).execution.evidence[0].path = path; }, 'EVIDENCE_REF');
  }
  assert.equal(isSafeEvidencePath('docs/engineering/alpha-closure.json'), true);
  assert.throws(() => actualRead('docs/planning'), /evidence must be a file/);
});
test('invalid date and missing execution axes cannot silently pass', () => {
  for (const date of ['2026-99-10', '2026-02-30', 'yesterday']) {
    rejects(({ data }) => { data.execution_tracking.reviewed_at = date; }, 'REVIEW_DATE');
  }
  rejects(({ data }) => { target(data).execution.verification = 'COMPLETE'; }, 'TASK_ENUM');
  assert.ok(validateProjectStatus({ tasks: [] }).some((error) => error.startsWith('STATUS_SCHEMA:')));
});
test('generated Markdown is reproducible and hand-written inflation fails', () => {
  const f = fixture(); const output = renderProjectStatus(f.data);
  assert.equal(output, renderProjectStatus(f.data));
  assert.deepEqual(f.run(output), []);
  assert.ok(f.run(output + '\nS4 全部完成\n').some((error) => error.startsWith('GENERATED_DRIFT:')));
});
test('next tasks and gate set remain within the original plan', () => {
  rejects(({ data }) => { data.execution_tracking.next_task_ids.push('S4-Z'); }, 'NEXT_TASKS');
  rejects(({ data }) => { data.execution_tracking.gates.pop(); }, 'GATE_IDS');
});
test('backend slice completion cannot serve as a G4 release decision', () => {
  rejects(({ data }) => {
    const gate = data.execution_tracking.gates.find((item) => item.id === 'G4');
    gate.state = 'go'; gate.evidence = target(data).execution.evidence[0];
  }, 'GATE_RECEIPT');
});
test('acceptance of one task cannot grant full phase acceptance', () => {
  rejects((f) => {
    makeAccepted(f, 'S4-03');
    const gate = f.data.execution_tracking.gates.find((item) => item.id === 'G4');
    gate.state = 'go'; gate.evidence = f.add('docs/planning/fixtures/gate.json', {
      record_type: 'gate-acceptance', gate: 'G4', decision: 'go', task_ids: ['S4-03'],
      approved_by: 'fixture-reviewer', approved_at: '2026-09-16T09:00:00Z',
    });
  }, 'GATE_RECEIPT');
});
test('local CI success does not assert remote merge protection or live payment readiness', () => {
  rejects(({ data }) => { data.execution_tracking.external_conditions[0].state = 'verified'; }, 'EVIDENCE_REF');
  rejects(({ data }) => {
    const item = data.execution_tracking.external_conditions[1]; item.state = 'verified';
    item.evidence = target(data, 'S4-06').execution.evidence[0];
  }, 'EXTERNAL_RECEIPT');
  rejects(({ data }) => { data.execution_tracking.external_conditions = []; }, 'EXTERNAL_IDS');
});
test('tests do not mutate the repository plan or create real approval receipts', () => {
  assert.deepEqual(JSON.parse(readFileSync(new URL('../docs/planning/project-data.json', import.meta.url))), baseline);
});
test('status guard and negative tests stay wired into the real aggregate CI command', () => {
  const pkg = JSON.parse(actualRead('package.json'));
  assert.equal(pkg.scripts['check:status'], 'node scripts/check-project-status.mjs');
  assert.ok(pkg.scripts.check.split('&&').map((part) => part.trim()).includes('pnpm check:status'));
  assert.ok(pkg.scripts.test.split(/\s+/).includes('scripts/project-status.test.mjs'));
  const workflow = parse(actualRead('.github/workflows/ci.yml').toString('utf8'));
  assert.ok(workflow.jobs.check.steps.some((step) => step.run === 'pnpm check'));
  const integration = workflow.jobs.check.steps.find((step) => ['pnpm test:integration', 'pnpm test:integration:browser'].includes(step.run));
  assert.ok(integration, 'CI must execute real PostgreSQL rather than only evidence checks');
  assert.equal(integration.env.MENDER_TEST_ALLOW_CREATE_DATABASE, 'true');
  if (integration.run === 'pnpm test:integration:browser') {
    assert.equal(integration.env.MENDER_S4_BROWSER, 'true');
    assert.match(pkg.scripts['test:integration:browser'], /-tags=integration.*-count=1.*-timeout=300s.*\.\/tests\/integration/u);
    assert.ok(workflow.jobs.check.steps.some((step) => step.run === 'pnpm exec playwright install --with-deps chromium'));
  }
});
