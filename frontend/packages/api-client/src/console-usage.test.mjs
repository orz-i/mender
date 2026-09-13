import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createConsoleUsageClient, MenderApiError } from './index.ts';

const response = () => ({ data: {
  budget_periods: [{
    budget_id: 'budget_a', period_id: 'period_a', currency: 'USD', starts_at: '2026-09-12T00:00:00Z', ends_at: '2026-10-12T00:00:00Z', active: true,
    limit_micro: '9007199254740993', consumed_micro: '125000', reserved_micro: '75000', available_micro: '9007199254540993', revision: '4',
  }],
  usage_entries: [{
    run_id: 'run_a', budget_id: 'budget_a', period_id: 'period_a', currency: 'USD', quota_state: 'settled', reserved_micro: '200000', charged_micro: '125000', released_micro: '75000', outcome: 'succeeded',
    created_at: '2026-09-12T00:00:00Z', finalized_at: '2026-09-12T00:01:00Z',
  }],
} });

test('Console Usage uses Browser Session transport and preserves exact micro strings beyond Number safe range', async () => {
  const client = createConsoleUsageClient('', async (url, init) => {
    assert.equal(url, '/api/console/v1/workspaces/ws_a/usage');
    assert.equal(init.credentials, 'same-origin');
    assert.equal(new Headers(init.headers).get('Authorization'), null);
    assert.equal(init.cache, 'no-store');
    return Response.json(response());
  });
  const value = await client.get('ws_a');
  assert.equal(value.budgetPeriods[0].limitMicro, '9007199254740993');
  assert.equal(typeof value.budgetPeriods[0].limitMicro, 'string');
  assert.equal(value.usageEntries[0].releasedMicro, '75000');
});

test('Console Usage fails closed on amount drift or internal-field leakage', async () => {
  const drift = response();
  drift.data.budget_periods[0].available_micro = '1';
  const driftClient = createConsoleUsageClient('', async () => Response.json(drift));
  await assert.rejects(driftClient.get('ws_a'), /不一致/);

  const leaked = response();
  leaked.data.usage_entries[0].reservation_id = 'internal';
  const leakedClient = createConsoleUsageClient('', async () => Response.json(leaked));
  await assert.rejects(leakedClient.get('ws_a'), /不应暴露/);
});

test('Console Usage surfaces session authorization failure without inventing an identity', async () => {
  const client = createConsoleUsageClient('', async () => Response.json({ error: { code: 'UNAUTHENTICATED', message: 'Login is required.' } }, { status: 401 }));
  await assert.rejects(client.get('ws_a'), (error) => {
    assert.ok(error instanceof MenderApiError);
    assert.equal(error.status, 401);
    return true;
  });
});
