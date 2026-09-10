// This orchestrator only creates a named, labelled, loopback-only test container.
// `run` is injected so lifecycle failures can be tested without a Docker daemon.
export const postgresTestImage = 'postgres:18.6';
const label = 'com.mender.integration-run';

export function localDockerEndpoint(endpoint) {
  return /^unix:\/\/\//u.test(endpoint) || /^npipe:\/\/\/\/\.\/pipe\//u.test(endpoint);
}

export function loopbackPort(output) {
  const match = /^127\.0\.0\.1:(\d{1,5})$/u.exec(output.trim());
  const port = match ? Number(match[1]) : 0;
  if (port < 1 || port > 65535) throw new Error('Docker did not publish one loopback-only PostgreSQL port.');
  return port;
}

export async function postgresPreflight(run, endpointOverride) {
  const context = endpointOverride ? { code: 0, stdout: JSON.stringify(endpointOverride) }
    : await run('docker', ['context', 'inspect', '--format', '{{json .Endpoints.docker.Host}}']);
  let endpoint;
  try { endpoint = JSON.parse(context.stdout ?? ''); } catch { throw new Error('Cannot determine the active Docker endpoint; no resources created.'); }
  if (context.code !== 0 || typeof endpoint !== 'string' || !localDockerEndpoint(endpoint)) {
    throw new Error('Only a local Unix-socket or local Windows-pipe Docker endpoint is allowed.');
  }
  const version = await run('docker', ['version', '--format', '{{.Server.Version}}']);
  if (version.code !== 0) throw new Error('Docker daemon is unavailable. Start Docker yourself, then retry; no resources created.');
  const major = Number(/^(\d+)\./u.exec(version.stdout.trim())?.[1]);
  if (!Number.isInteger(major) || major < 28) throw new Error('Docker Engine 28+ is required for the loopback publishing safety baseline.');
}

export function containerArgs(name, nonce, pull) {
  return ['run', '--detach', '--rm', '--pull', pull ? 'missing' : 'never', '--name', name,
    '--label', `${label}=${nonce}`, '--publish', '127.0.0.1::5432', '--memory', '512m',
    '--cpus', '2', '--pids-limit', '256', '--tmpfs', '/var/lib/postgresql:rw,nosuid,size=268435456',
    '--env', 'POSTGRES_USER=postgres', '--env', 'POSTGRES_DB=postgres', '--env', 'POSTGRES_PASSWORD', postgresTestImage];
}

export async function withIsolatedPostgres({ run, runTests, sleep, nonce, password, endpointOverride, pull = false, signal, log = () => {} }) {
  if (!/^[a-f0-9]{32}$/u.test(nonce) || !/^[a-f0-9]{64}$/u.test(password)) throw new Error('Test resource identifiers must be freshly generated random hex.');
  await postgresPreflight(run, endpointOverride);
  const name = `mender-pg-test-${nonce}`;
  let attempted = false;
  let primaryError;
  let cleanupError;
  const checkAbort = () => { if (signal?.aborted) throw new Error('Test run canceled.'); };
  try {
    checkAbort();
    attempted = true;
    const created = await run('docker', containerArgs(name, nonce, pull), { env: { POSTGRES_PASSWORD: password }, signal, timeoutMs: 180000 });
    if (created.code !== 0) throw new Error(`Could not create isolated PostgreSQL container. Ensure ${postgresTestImage} is local, or explicitly use --pull.`);
    if (!/^[a-f0-9]{64}$/u.test(created.stdout.trim())) throw new Error('Docker returned an invalid container identity.');
    const owner = await run('docker', ['inspect', '--format', `{{ index .Config.Labels "${label}" }}`, name], { signal });
    if (owner.code !== 0 || owner.stdout.trim() !== nonce) throw new Error('Test container ownership verification failed.');
    const binding = await run('docker', ['port', name, '5432/tcp'], { signal });
    if (binding.code !== 0) throw new Error('Cannot read isolated PostgreSQL port.');
    const port = loopbackPort(binding.stdout);
    let ready = false;
    for (let n = 0; n < 30; n++) {
      checkAbort();
      const probe = await run('docker', ['exec', name, 'pg_isready', '-U', 'postgres', '-d', 'postgres', '-h', '127.0.0.1'], { signal });
      if (probe.code === 0) { ready = true; break; }
      await sleep(1000, signal);
    }
    if (!ready) throw new Error('Isolated PostgreSQL did not become ready within its bounded wait.');
    checkAbort();
    log('Isolated PostgreSQL is ready; executing the real migration/RLS/CAS/query/atomic-admission suite.');
    const code = await runTests({
      MENDER_TEST_DATABASE_URL: `postgres://postgres:${password}@127.0.0.1:${port}/postgres?sslmode=disable`,
      MENDER_TEST_ALLOW_CREATE_DATABASE: 'true',
    }, signal);
    checkAbort();
    if (code !== 0) throw new Error(`PostgreSQL integration suite failed (exit ${code}); this is not a passing verification.`);
  } catch (error) {
    primaryError = error;
  }
  // Cleanup follows both success and failure, without throwing from a finally
  // block or hiding the original failure. It gets a fresh, non-aborted timeout.
  if (attempted) {
      try {
        const owner = await run('docker', ['inspect', '--format', `{{ index .Config.Labels "${label}" }}`, name], { timeoutMs: 15000 });
        if (owner.code === 0) {
          if (owner.stdout.trim() !== nonce) throw new Error('Refusing to remove a container without this invocation\'s ownership label.');
          const result = await run('docker', ['rm', '--force', '--volumes', name], { timeoutMs: 20000 });
          if (result.code !== 0) throw new Error(`Could not remove owned test container ${name}.`);
          log('Owned test container removed.');
        } else {
          // Verify absence rather than treating every inspect error as "already removed".
          const listed = await run('docker', ['ps', '--all', '--filter', `name=^/${name}$`, '--format', '{{.Names}}'], { timeoutMs: 15000 });
          if (listed.code !== 0 || listed.stdout.trim() !== '') throw new Error(`Unable to confirm cleanup of ${name}.`);
        }
      } catch (error) {
        cleanupError = error;
      }
  }
  if (primaryError && cleanupError) throw new AggregateError([primaryError, cleanupError], `${primaryError.message} Cleanup also needs attention: ${cleanupError.message}`, { cause: primaryError });
  if (cleanupError) throw cleanupError;
  if (primaryError) throw primaryError;
}
