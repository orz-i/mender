import { createHash } from 'node:crypto';
import { readFileSync, realpathSync, statSync } from 'node:fs';
import { isAbsolute, relative, resolve, sep } from 'node:path';
import {
  acceptanceLabels, canonicalAliases, implementationLabels, planFingerprint,
  renderProjectStatus, taskStatus, verificationLabels,
} from './project-status.mjs';

export const statusEvidenceBindings = {
  'S4-01': 'docs/engineering/s4a-plugin-publication-evidence.json',
  'S4-02': 'docs/engineering/s4c-dangerous-operation-jit-evidence.json',
  'S4-03': 'docs/engineering/s403-commerce-billing-evidence.json',
  'S4-04': 'docs/engineering/s4b-release-governance-evidence.json',
  'S4-05': 'docs/engineering/s4d-platform-admin-evidence.json',
  'S4-06': 'docs/engineering/s406-sandbox-payment-evidence.json',
};
const phaseCounts = { S0: 10, S1: 15, S2: 18, S3: 18, S4: 18, S5: 13 };
const shaPattern = /^[a-f0-9]{64}$/;
const headPattern = /^[a-f0-9]{40}$/;
const text = (value) => typeof value === 'string' && value.trim().length > 0;
const object = (value) => value !== null && typeof value === 'object' && !Array.isArray(value);
const timestamp = (value) => text(value) && /^\d{4}-\d{2}-\d{2}T/.test(value) && Number.isFinite(Date.parse(value));
const date = (value) => {
  if (typeof value !== 'string' || !/^\d{4}-\d{2}-\d{2}$/.test(value)) return false;
  const parsed = new Date(`${value}T00:00:00Z`);
  return Number.isFinite(parsed.getTime()) && parsed.toISOString().slice(0, 10) === value;
};
export const evidenceHash = (value) => createHash('sha256').update(value).digest('hex');

export function isSafeEvidencePath(value) {
  return text(value) && value.length <= 500 && !/[\\:]/.test(value)
    && ![...value].some((character) => character.codePointAt(0) < 32)
    && !value.startsWith('/') && !value.split('/').some((part) => !part || part === '.' || part === '..')
    && value.split('/').every((part) => !/[. ]$/.test(part));
}

// Resolve links before reading so an apparently local reference cannot escape the checkout.
export function createEvidenceReader(root) {
  const actualRoot = realpathSync(root);
  return (path) => {
    if (!isSafeEvidencePath(path)) throw new Error('unsafe repository evidence path');
    const actual = realpathSync(resolve(actualRoot, path));
    const local = relative(actualRoot, actual);
    if (local === '..' || local.startsWith(`..${sep}`) || isAbsolute(local)) {
      throw new Error('evidence resolves outside repository');
    }
    const stat = statSync(actual);
    if (!stat.isFile() || stat.size > 5 * 1024 * 1024) throw new Error('evidence must be a file no larger than 5 MiB');
    return readFileSync(actual);
  };
}

// Structural verification only: receipts remain reviewable records, not cryptographic approval.
export function validateProjectStatus(data, { readEvidence, generated } = {}) {
  const errors = [];
  const fail = (code, message) => errors.push(`${code}: ${message}`);
  const check = (condition, code, message) => { if (!condition) fail(code, message); };
  if (!object(data) || !Array.isArray(data.tasks) || !object(data.execution_tracking)) {
    return ['STATUS_SCHEMA: tasks and execution_tracking are required'];
  }
  const tracking = data.execution_tracking;
  const cache = new Map();
  function readRef(ref, owner) {
    if (!object(ref) || !isSafeEvidencePath(ref.path) || !shaPattern.test(ref.sha256 ?? '')) {
      fail('EVIDENCE_REF', `${owner} requires a contained path and SHA-256`); return null;
    }
    try {
      if (!cache.has(ref.path)) cache.set(ref.path, readEvidence(ref.path));
      const bytes = cache.get(ref.path);
      if (evidenceHash(bytes) !== ref.sha256) {
        fail('EVIDENCE_DRIFT', `${owner}: ${ref.path} changed; review scope before updating digest`); return null;
      }
      return Buffer.from(bytes).toString('utf8');
    } catch (error) {
      fail('EVIDENCE_READ', `${owner}: ${ref.path}: ${error.message}`); return null;
    }
  }
  function readJsonRef(ref, owner) {
    const content = readRef(ref, owner);
    if (content === null) return null;
    try {
      const value = JSON.parse(content);
      if (!object(value)) throw new Error('expected JSON object');
      return value;
    } catch (error) { fail('EVIDENCE_JSON', `${owner}: ${error.message}`); return null; }
  }
  function validateRunReceipt(task) {
    const receipt = readJsonRef(task.execution.verification_receipt, `${task.id} verification receipt`);
    if (!receipt) return;
    check(receipt.record_type === 'task-verification' && receipt.task_id === task.id
      && receipt.result === 'passed' && receipt.exit_code === 0 && text(receipt.command)
      && text(receipt.environment) && headPattern.test(receipt.head ?? '') && timestamp(receipt.finished_at),
    'VERIFICATION_RECEIPT', `${task.id} needs task, command, environment, HEAD, execution time and successful result`);
    readRef(receipt.log, `${task.id} verification log`);
  }

  check(tracking.schema_version === 1, 'TRACKING_SCHEMA', 'unsupported execution tracking version');
  check(date(tracking.reviewed_at),
  'REVIEW_DATE', 'review date must be a real ISO date');
  check(headPattern.test(tracking.reviewed_head ?? ''), 'REVIEW_HEAD', 'reviewed local HEAD is required');
  check(text(tracking.review_basis), 'REVIEW_BASIS', 'state whether evidence was rerun or read historically');
  check(Object.hasOwn(phaseCounts, tracking.current_phase), 'CURRENT_PHASE', 'use original S0–S5 only');
  check(tracking.progress_policy === 'null-until-full-acceptance', 'PROGRESS_POLICY', 'unknown progress must not become numeric zero');
  check(planFingerprint(data.tasks) === tracking.plan_fingerprint, 'PLAN_DRIFT', 'original planning fields changed without reviewed baseline update');
  check(object(tracking.canonical_aliases) && Object.keys(tracking.canonical_aliases).length === Object.keys(canonicalAliases).length
    && Object.entries(canonicalAliases).every(([alias, id]) => tracking.canonical_aliases[alias] === id),
  'ALIAS_DRIFT', 'historical S4 aliases must map to original task IDs');

  const expectedIds = Object.entries(phaseCounts).flatMap(([phase, count]) =>
    Array.from({ length: count }, (_, index) => `${phase}-${String(index + 1).padStart(2, '0')}`));
  const ids = data.tasks.map((task) => task?.id);
  check(ids.length === expectedIds.length && new Set(ids).size === ids.length
    && expectedIds.every((id) => ids.includes(id)), 'TASK_SET', 'retain exactly the original 92 unique tasks; do not invent S4 stages');
  for (const task of data.tasks) {
    if (!object(task) || !object(task.execution)) { fail('TASK_SCHEMA', 'task execution is required'); continue; }
    const e = task.execution;
    check(task.phase === task.id?.split('-')[0], 'TASK_PHASE', `${task.id} phase does not match canonical ID`);
    check(object(task.baseline_execution) && Object.hasOwn(task.baseline_execution, 'status')
      && Object.hasOwn(task.baseline_execution, 'progress') && Object.hasOwn(task.baseline_execution, 'evidence'),
    'BASELINE_HISTORY', `${task.id} must preserve the initial execution snapshot`);
    const validEnums = Object.hasOwn(implementationLabels, e.implementation)
      && Object.hasOwn(verificationLabels, e.verification) && Object.hasOwn(acceptanceLabels, e.acceptance);
    check(validEnums, 'TASK_ENUM', `${task.id} has unknown implementation/verification/acceptance`);
    check(text(e.scope), 'TASK_SCOPE', `${task.id} needs explicit delivered/reviewed scope`);
    const remainingValid = Array.isArray(e.remaining) && e.remaining.every(text);
    check(remainingValid, 'TASK_REMAINING', `${task.id} remaining conditions must be text items`);
    check(Array.isArray(e.evidence), 'TASK_EVIDENCE', `${task.id} evidence must be an array`);
    if (!validEnums || !remainingValid || !Array.isArray(e.evidence)) continue;
    if (e.evidence.some((ref) => !object(ref))) { fail('EVIDENCE_REF', `${task.id} malformed reference`); continue; }
    check(new Set(e.evidence.map((ref) => ref?.path)).size === e.evidence.length, 'DUPLICATE_EVIDENCE', `${task.id} repeats a source`);
    for (const ref of e.evidence) readRef(ref, task.id);
    check(task.status === taskStatus(e), 'STATUS_PROJECTION', `${task.id} status diverges from execution`);
    check(task.evidence === e.evidence.map((ref) => ref.path).join(';'), 'EVIDENCE_PROJECTION', `${task.id} top-level evidence diverges`);
    if (e.acceptance === 'accepted') {
      check(e.implementation === 'implemented' && e.verification === 'passed'
        && e.remaining.length === 0 && task.progress === 1, 'FULL_ACCEPTANCE', `${task.id} full acceptance requires full scope, passed verification, no remaining items and progress=1`);
      const record = readJsonRef(e.acceptance_record, `${task.id} acceptance`);
      if (record) check(record.record_type === 'task-acceptance' && record.task_id === task.id
        && record.decision === 'accepted' && record.coverage === 'task-full-scope'
        && text(record.approved_by) && timestamp(record.approved_at) && text(record.scope)
        && record.verification_receipt?.path === e.verification_receipt?.path
        && record.verification_receipt?.sha256 === e.verification_receipt?.sha256,
      'ACCEPTANCE_RECEIPT', `${task.id} needs an independent full-scope decision bound to its verification receipt`);
    } else {
      check(task.progress === null, 'UNKNOWN_PROGRESS', `${task.id} is not fully accepted; progress must be null`);
      check(e.remaining.length > 0, 'MISSING_REMAINING', `${task.id} needs explicit pending conditions`);
      if (e.acceptance === 'scoped_go') {
        const decision = readJsonRef(e.acceptance_record, `${task.id} scoped decision`);
        if (decision) check((task.id === 'S3-18' && decision.decision?.g3_gate === 'scoped-go')
          || (decision.task_id === task.id && decision.decision === 'scoped-go'
            && text(decision.approved_by) && timestamp(decision.approved_at)),
        'SCOPED_DECISION', `${task.id} cannot label an unrelated or full decision scoped-go`);
      } else check(e.acceptance_record === null, 'PENDING_ACCEPTANCE_RECORD', `${task.id} is pending; do not attach an approved decision`);
    }
    if (e.verification === 'passed') validateRunReceipt(task);
    else check(e.verification_receipt === null, 'RECORDED_NOT_RERUN', `${task.id} historical/unrun status must not imply a successful fresh receipt`);
    if (['partial', 'scoped_implemented', 'implemented'].includes(e.implementation)) {
      check(e.evidence.length > 0, 'IMPLEMENTATION_EVIDENCE', `${task.id} implementation needs evidence`);
    } else {
      check(['not_reviewed', 'not_run'].includes(e.verification) && e.acceptance === 'pending',
      'UNREVIEWED_CLAIM', `${task.id} cannot pass verification or acceptance while unreviewed/unstarted`);
    }
    if (e.verification === 'recorded') check(e.evidence.length > 0, 'RECORDED_EVIDENCE', `${task.id} recorded verification needs sources`);
    if (Object.hasOwn(statusEvidenceBindings, task.id)) {
      const binding = statusEvidenceBindings[task.id];
      const ref = e.evidence.find((item) => item.path === binding);
      check(Boolean(ref), 'S4_BINDING', `${task.id} must retain its existing scoped work-package evidence`);
      if (ref) {
        const record = readJsonRef(ref, `${task.id} work-package`);
        if (record?.status === 'complete') check(!['not_reviewed', 'not_started'].includes(e.implementation),
          'STALE_NOT_STARTED', `${task.id} cannot remain unstarted while its scoped work package is complete`);
      }
    }
  }
  check(Array.isArray(tracking.next_task_ids) && tracking.next_task_ids.length > 0
    && new Set(tracking.next_task_ids).size === tracking.next_task_ids.length
    && tracking.next_task_ids.every((id) => ids.includes(id)), 'NEXT_TASKS', 'next work must reference original tasks without duplicate/extra stages');
  check(Array.isArray(tracking.gates), 'GATES', 'gate records required');
  if (Array.isArray(tracking.gates)) {
    check(tracking.gates.length === 3 && ['G3', 'G4', 'G5'].every((id) => tracking.gates.filter((gate) => gate.id === id).length === 1), 'GATE_IDS', 'retain G3/G4/G5 records');
    for (const gate of tracking.gates) {
      if (gate.state === 'pending') {
        check(gate.evidence === null, 'PENDING_GATE', `${gate.id} remains pending`); continue;
      }
      const record = readJsonRef(gate.evidence, `${gate.id} decision`);
      if (gate.id === 'G3' && gate.state === 'scoped-go') {
        if (record) check(record.decision?.g3_gate === gate.state, 'G3_SCOPE', 'G3 state must match the scoped decision');
      } else {
        check(gate.state === 'go', 'GATE_STATE', `${gate.id} needs full phase acceptance or an explicitly reviewed scoped schema extension`);
        const phaseTaskIds = data.tasks.filter((task) => task.phase === `S${gate.id.slice(1)}`).map((task) => task.id);
        if (record) check(record.record_type === 'gate-acceptance' && record.gate === gate.id
          && record.decision === gate.state && text(record.approved_by) && timestamp(record.approved_at)
          && Array.isArray(record.task_ids) && record.task_ids.length > 0
          && phaseTaskIds.length > 0 && phaseTaskIds.every((id) => record.task_ids.includes(id))
          && record.task_ids.every((id) => data.tasks.some((task) => task.id === id && task.execution?.acceptance === 'accepted')),
        'GATE_RECEIPT', `${gate.id} must have a decision referencing accepted original tasks, not a backend slice`);
      }
    }
  }
  check(Array.isArray(tracking.external_conditions), 'EXTERNAL_CONDITIONS', 'external limitations cannot disappear');
  if (Array.isArray(tracking.external_conditions)) {
    for (const id of ['remote-merge-protection', 'external-acceptance-and-live-payments']) {
      check(tracking.external_conditions.filter((item) => item.id === id).length === 1, 'EXTERNAL_IDS', `retain ${id} exactly once`);
    }
    for (const item of tracking.external_conditions) {
      check(text(item.note), 'EXTERNAL_NOTE', `${item.id} needs its boundary`);
      if (['pending', 'not-verified'].includes(item.state)) continue;
      const record = readJsonRef(item.evidence, `${item.id} external verification`);
      check(item.state === 'verified', 'EXTERNAL_STATE', 'unsupported external status');
      if (record) check(record.record_type === 'external-control-verification' && record.control_id === item.id
        && record.result === 'verified' && text(record.external_reference) && text(record.verified_by)
        && timestamp(record.verified_at), 'EXTERNAL_RECEIPT', `${item.id} needs an external observation, not local CI success`);
    }
  }
  if (errors.length === 0 && typeof generated === 'string') {
    check(generated === renderProjectStatus(data), 'GENERATED_DRIFT', 'run pnpm status:render after reviewing the source status');
  }
  return errors;
}
