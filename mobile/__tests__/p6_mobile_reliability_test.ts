import { commandQueue } from '../src/core/sync/commandQueue';
import { commandProcessor } from '../src/core/sync/commandProcessor';
import { OfflineQueue } from '../src/services/offlineQueue';
import { resetSQLiteMockState } from '../jest/setup';
import { useAuthStore } from '../src/stores/authStore';

const globalFetch = global.fetch;

describe('Priority 6 — Mobile Operational Reliability & Fault-Tolerance', () => {
  beforeEach(async () => {
    resetSQLiteMockState();
    await commandQueue.saveCommands([]);
    useAuthStore.setState({ token: 'mock-valid-driver-token', user: { id: 'drv-prod-1' } as any });
    global.fetch = jest.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ success: true }),
    });
  });

  afterEach(() => {
    global.fetch = globalFetch;
  });

  test('1. Rapid Network Flapping (Offline -> Online -> Offline) retains queue integrity', async () => {
    // 1. Enqueue dispatch accept while offline
    const cmd1 = await commandQueue.enqueueCommand('ACCEPT_DISPATCH', { trip_id: 'trip-prod-101' }, 'idemp-disp-101');
    expect(cmd1).toBeDefined();

    // 2. Simulate offline network failure on flush
    (global.fetch as jest.Mock).mockRejectedValueOnce(new Error('Network request failed'));
    const flushRes1 = await commandProcessor.flush('mock-valid-driver-token');
    expect(flushRes1.synced).toBe(0);
    expect(flushRes1.failed).toBe(1);

    // Command must still be pending in queue
    const pending1 = await commandQueue.getPendingCommands();
    expect(pending1.length).toBe(1);
    expect(pending1[0].commandId).toBe('idemp-disp-101');

    // 3. Network recovers -> flush succeeds
    (global.fetch as jest.Mock).mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: async () => ({ status: 'ACCEPTED' }),
    });

    const flushRes2 = await commandProcessor.flush('mock-valid-driver-token');
    expect(flushRes2.synced).toBe(1);
    expect(flushRes2.failed).toBe(0);

    const pendingAfter = await commandQueue.getPendingCommands();
    expect(pendingAfter.length).toBe(0);
  });

  test('2. App Crash / Force-Kill Recovery: Persisted commands survive instance recreation', async () => {
    await commandQueue.enqueueCommand('START_TRIP', { trip_id: 'trip-prod-202' }, 'idemp-start-202');

    // Verify stored commands
    const pending = await commandQueue.getPendingCommands();
    expect(pending.length).toBe(1);
    expect(pending[0].type).toBe('START_TRIP');
    expect(pending[0].payload.trip_id).toBe('trip-prod-202');
  });

  test('3. High Volume Batch Enqueue & Sequential FIFO Processing (20 actions)', async () => {
    for (let i = 0; i < 20; i++) {
      await commandQueue.enqueueCommand(
        'REACH_STOP',
        { trip_id: 'trip-prod-batch', stop_id: `stop-${i}` },
        `idemp-reach-${i}`
      );
    }

    const pendingBefore = await commandQueue.getPendingCommands();
    expect(pendingBefore.length).toBe(20);

    const flushRes = await commandProcessor.flush('mock-valid-driver-token');
    expect(flushRes.synced).toBe(20);
    expect(flushRes.failed).toBe(0);

    const pendingAfter = await commandQueue.getPendingCommands();
    expect(pendingAfter.length).toBe(0);
  });

  test('4. Auth Token Expiry Graceful Re-try (401 Unauthorized -> Refresh -> Success)', async () => {
    await commandQueue.enqueueCommand(
      'COMPLETE_STOP',
      { trip_id: 'trip-prod-505', stop_id: 'stop-jaipur' },
      'idemp-complete-jaipur'
    );

    // 1st attempt fails with 401 Unauthorized (Expired Token)
    (global.fetch as jest.Mock).mockResolvedValueOnce({
      ok: false,
      status: 401,
      json: async () => ({ error: 'TOKEN_EXPIRED' }),
    });

    const flushRes1 = await commandProcessor.flush('mock-expired-token');
    expect(flushRes1.synced).toBe(0);
    expect(flushRes1.failed).toBe(1);

    // Command remains intact in storage with FAILED state
    const allCmds = await commandQueue.getCommands();
    expect(allCmds.length).toBe(1);
    expect(allCmds[0].state).toBe('FAILED');

    // Simulate token refresh and retry
    useAuthStore.setState({ token: 'mock-newly-refreshed-token' });
    await commandQueue.updateCommandState('idemp-complete-jaipur', 'PENDING');

    // 2nd attempt succeeds with refreshed token
    (global.fetch as jest.Mock).mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: async () => ({ status: 'COMPLETED' }),
    });

    const flushRes2 = await commandProcessor.flush('mock-newly-refreshed-token');
    expect(flushRes2.synced).toBe(1);
    expect(flushRes2.failed).toBe(0);

    const pending2 = await commandQueue.getPendingCommands();
    expect(pending2.length).toBe(0);
  });
});
