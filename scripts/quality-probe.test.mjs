import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { renderQualityReport, runProbeJob, validateProbeConfig } from './lib/quality-probe.mjs';

const config = { endpoint: 'http://127.0.0.1:3000/mcp/v1/workspaces/ws', key_file: '.tmp/key', baseline: '.tmp/baseline.json', samples: 3, interval_ms: 5000 };
const healthy = { code: 0, stdout: JSON.stringify({ status: 'reachable', contract_sha256: 'a'.repeat(64), tool_count: 2, business_tools_called: 0, baseline_compared: true }), stderr: '' };
const now = () => new Date('2026-09-16T00:00:00.000Z');
test('bounded schedule keeps immutable baseline and never asks for a tools/call', async () => {
  let calls = 0, sleeps = 0;
  const before = JSON.stringify(config);
  const report = await runProbeJob(config, { probe: async (c) => { assert.equal(c.baseline, config.baseline); calls++; return healthy; }, sleep: async (ms) => { assert.equal(ms, 5000); sleeps++; }, now });
  assert.equal(calls, 3); assert.equal(sleeps, 2); assert.equal(report.status, 'passed');
  assert.equal(JSON.stringify(config), before); assert.equal(report.business_tools_called, 0);
  assert.match(renderQualityReport(report), /<table>/u);
});
test('drift and unavailability fail the whole job without copying secrets', async () => {
  const replies = [healthy, { code: 1, stderr: 'TOOL_CONTRACT_DRIFT_REVIEW_REQUIRED' }, { code: 1, stderr: 'sensitive upstream detail' }];
  const report = await runProbeJob(config, { probe: async () => replies.shift(), sleep: async () => {}, now });
  assert.equal(report.status, 'failed'); assert.equal(report.samples[1].state, 'contract-drift');
  assert.equal(report.samples[2].state, 'unavailable'); assert.ok(!JSON.stringify(report).includes('sensitive'));
});
test('invalid success output, lost baseline and side-effect claims never pass', async () => {
  for (const row of [{ ...JSON.parse(healthy.stdout), business_tools_called: 1 }, { ...JSON.parse(healthy.stdout), baseline_compared: false }, { ...JSON.parse(healthy.stdout), token: 'SECRET' }, { status: 'reachable' }]) {
    const report = await runProbeJob({ ...config, samples: 1 }, { probe: async () => ({ code: 0, stdout: JSON.stringify(row) }), sleep: async () => {}, now });
    assert.equal(report.status, 'failed'); assert.ok(!JSON.stringify(report).includes('SECRET'));
  }
});
test('invalid counts, transport and hidden configuration are rejected', () => {
  for (const extra of [{ samples: 0 }, { samples: 61 }, { interval_ms: 1 }, { endpoint: 'http://example.test/mcp/v1/workspaces/ws' }, { endpoint: config.endpoint + '?token=secret' }, { execute: true }]) assert.throws(() => validateProbeConfig({ ...config, ...extra }));
});
test('HTML report refuses markup and inconsistent success rather than escaping arbitrary payloads', async () => {
  const report = await runProbeJob({ ...config, samples: 1 }, { probe: async () => healthy, sleep: async () => {}, now });
  const bad = structuredClone(report); bad.samples[0].at = '<script>bad()</script>';
  assert.throws(() => renderQualityReport(bad));
  assert.throws(() => renderQualityReport({ ...report, status: 'failed' }));
});
test('actual runner is wired only to bounded CLI health with required baseline', () => {
  const source = readFileSync(new URL('./quality-probe.mjs', import.meta.url), 'utf8');
  assert.match(source, /'--baseline', config.baseline/u); assert.match(source, /'15s', 'health'/u);
  assert.doesNotMatch(source, /'--execute'|'tools\/call'/u);
});
