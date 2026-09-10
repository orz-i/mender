import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { parse } from 'yaml';
import { containerArgs, localDockerEndpoint, loopbackPort, postgresPreflight, postgresTestImage, withIsolatedPostgres } from './lib/postgres-test.mjs';

const nonce = 'a'.repeat(32), password = 'b'.repeat(64);
function fixture(change = {}) {
  const calls = [];
  let inspections = 0;
  const run = async (bin, args, options = {}) => {
    calls.push({ bin, args, options });
    if (args[0] === 'context') return { code: 0, stdout: JSON.stringify(change.endpoint ?? 'npipe:////./pipe/docker_engine') };
    if (args[0] === 'version') return { code: change.daemonDown ? 1 : 0, stdout: change.version ?? '28.4.0' };
    if (args[0] === 'run') return { code: change.createFail ? 1 : 0, stdout: 'c'.repeat(64) };
    if (args[0] === 'inspect') { inspections++; return { code: change.inspectFail ? 1 : 0, stdout: inspections > 1 && change.wrongOwner ? 'foreign' : nonce }; }
    if (args[0] === 'port') return { code: 0, stdout: change.port ?? '127.0.0.1:55432' };
    if (args[0] === 'exec') return { code: change.neverReady ? 1 : 0, stdout: '' };
    if (args[0] === 'rm') return { code: change.cleanupFail ? 1 : 0, stdout: '' };
    if (args[0] === 'ps') return { code: 0, stdout: change.createFail ? '' : `mender-pg-test-${nonce}` };
    throw new Error('Unexpected Docker command');
  };
  const options = { run, nonce, password, sleep: async () => {},
    runTests: async (env) => { calls.push({ bin: 'tests', env }); return change.testExit ?? 0; },
  };
  return { calls, options };
}

test('isolated runner only accepts local engine and loopback port mappings', () => {
  for (const p of ['unix:///var/run/docker.sock', 'npipe:////./pipe/docker_engine']) assert.equal(localDockerEndpoint(p), true);
  for (const p of ['tcp://127.0.0.1:2375', 'ssh://remote', 'npipe:////server/pipe/docker', '']) assert.equal(localDockerEndpoint(p), false);
  assert.equal(loopbackPort('127.0.0.1:55432\n'), 55432);
  for (const p of ['0.0.0.0:5432', '[::]:5432', '127.0.0.1:0', '127.0.0.1:65536', '127.0.0.1:5432\n0.0.0.0:5432']) assert.throws(() => loopbackPort(p));
});

test('cancellation is a failure and cleanup does not reuse the aborted signal', async () => {
  const f = fixture(); const controller = new AbortController();
  f.options.signal = controller.signal;
  f.options.runTests = async () => { controller.abort(); return 0; };
  await assert.rejects(withIsolatedPostgres(f.options), /canceled/u);
  const cleanup = f.calls.find((c) => c.args?.[0] === 'rm');
  assert.ok(cleanup);
  assert.equal(cleanup.options.signal, undefined);
});

test('preflight failure never creates containers or runs fake-success tests', async () => {
  for (const change of [{ daemonDown: true }, { endpoint: 'ssh://remote' }, { version: '27.0.0' }]) {
    const f = fixture(change);
    await assert.rejects(withIsolatedPostgres(f.options));
    assert.ok(!f.calls.some((c) => c.args?.[0] === 'run' || c.bin === 'tests'));
  }
  const f = fixture(); await postgresPreflight(f.options.run, 'unix:///var/run/docker.sock');
  assert.equal(f.calls.some((c) => c.args[0] === 'context'), false);
});

test('successful real-suite invocation cleans only its owned resource and uses ephemeral credentials', async () => {
  const f = fixture(); await withIsolatedPostgres(f.options);
  const create = f.calls.find((c) => c.args?.[0] === 'run');
  assert.ok(!create.args.some((a) => a.includes(password)));
  assert.equal(create.options.env.POSTGRES_PASSWORD, password);
  assert.ok(create.args.includes('127.0.0.1::5432'));
  assert.ok(create.args.includes('never'));
  assert.equal(f.calls.filter((c) => c.bin === 'tests').length, 1);
  const tests = f.calls.find((c) => c.bin === 'tests');
  assert.ok(tests.env.MENDER_TEST_DATABASE_URL.includes('@127.0.0.1:55432/'));
  assert.equal(tests.env.MENDER_TEST_ALLOW_CREATE_DATABASE, 'true');
  const cleanup = f.calls.find((c) => c.args?.[0] === 'rm');
  assert.equal(cleanup.args.at(-1), `mender-pg-test-${nonce}`);
});

test('test failures, readiness failures and unsafe bindings still clean owned resources', async () => {
  for (const change of [{ testExit: 1 }, { neverReady: true }, { port: '0.0.0.0:5432' }]) {
    const f = fixture(change); await assert.rejects(withIsolatedPostgres(f.options));
    assert.equal(f.calls.filter((c) => c.args?.[0] === 'rm').length, 1);
  }
});

test('cleanup failures are failures; foreign ownership is never removed', async () => {
  const foreign = fixture({ wrongOwner: true }); await assert.rejects(withIsolatedPostgres(foreign.options), /ownership label/u);
  assert.equal(foreign.calls.some((c) => c.args?.[0] === 'rm'), false);
  const fail = fixture({ cleanupFail: true }); await assert.rejects(withIsolatedPostgres(fail.options), /remove/u);
  const absent = fixture({ createFail: true, inspectFail: true }); await assert.rejects(withIsolatedPostgres(absent.options));
  assert.equal(absent.calls.some((c) => c.args?.[0] === 'rm'), false);
});

test('Docker runner and CI keep the same existing PostgreSQL image baseline', () => {
  const ci = parse(readFileSync(new URL('../.github/workflows/ci.yml', import.meta.url), 'utf8'));
  assert.equal(ci.jobs.check.services.postgres.image, postgresTestImage);
  const args = containerArgs('mender-test-example', nonce, true);
  assert.ok(args.includes('missing'));
  assert.equal(args.at(-1), postgresTestImage);
});
