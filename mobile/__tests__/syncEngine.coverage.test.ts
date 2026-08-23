// Coverage strengthening for SyncEngine (was 51% lines / 28.8% branches).
// Drives the NetInfo watcher lifecycle, auto-sync timer, sync-store status
// transitions, and every syncPendingLogs response-shape branch.
import NetInfo from '@react-native-community/netinfo';
import { SyncEngine, startNetworkWatcher, stopNetworkWatcher } from '../src/services/syncEngine';
import { OfflineQueue } from '../src/services/offlineQueue';
import { DB } from '../src/services/storage';
import { applyNetInfoState, useSyncStore } from '../src/stores/syncStore';
import { useAuthStore } from '../src/stores/authStore';
import { getSQLiteMockState, resetSQLiteMockState } from '../jest/setup';

jest.mock('@react-native-community/netinfo', () => ({
  __esModule: true,
  default: {
    addEventListener: jest.fn(() => jest.fn()),
    fetch: jest.fn().mockResolvedValue({ isConnected: true }),
    configure: jest.fn(),
    useNetInfo: jest.fn(),
    refresh: jest.fn(),
  },
}));

const NetInfoMock = NetInfo as unknown as {
  addEventListener: jest.Mock;
};

const globalFetchSaved = global.fetch;

// Drain promise chains spawned by fire-and-forget watcher callbacks.
const flushPromises = async (): Promise<void> => {
  for (let i = 0; i < 5; i++) {
    await new Promise<void>((resolve) => setImmediate(resolve));
  }
};

function everyGpsLogSynced(): boolean {
  return getSQLiteMockState().offline_gps_logs.every((l) => l.synced === 1);
}

const authSession = {
  id: 'u_1',
  name: 'Rajesh Kumar',
  role: 'driver' as const,
  email: 'driver@avandab.com',
  driverId: 'drv_1',
};

function netHandler(): (state: { isConnected: boolean | null }) => void {
  return NetInfoMock.addEventListener.mock.calls[0][0];
}

describe('network watcher', () => {
  let flushSpy: jest.SpyInstance;
  let logSpy: jest.SpyInstance;
  let warnSpy: jest.SpyInstance;

  beforeEach(async () => {
    resetSQLiteMockState();
    stopNetworkWatcher();
    NetInfoMock.addEventListener.mockClear();
    useSyncStore.setState({ status: 'online_synced', pendingCount: 0 });
    await useAuthStore.getState().setAuth('tok', authSession);
    flushSpy = jest
      .spyOn(OfflineQueue, 'flush')
      .mockResolvedValue({ podsFlushed: 1, gpsFlushed: 2, expensesFlushed: 3 });
    logSpy = jest.spyOn(console, 'log').mockImplementation(() => {});
    warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => {});
  });

  afterEach(() => {
    stopNetworkWatcher();
    global.fetch = globalFetchSaved;
    jest.restoreAllMocks();
    jest.useRealTimers();
  });

  test('registers once, flips offline_saved on drop, flushes everything on reconnect', async () => {
    const syncLogsSpy = jest
      .spyOn(SyncEngine, 'syncPendingLogs')
      .mockResolvedValue({ syncedCount: 0, error: null });
    const flushQueuesSpy = jest
      .spyOn(SyncEngine, 'flushOfflineQueues')
      .mockResolvedValue({ podsFlushed: 1, expensesFlushed: 3, gpsFlushed: 2 });

    startNetworkWatcher();
    startNetworkWatcher(); // second call must not double-register
    expect(NetInfoMock.addEventListener).toHaveBeenCalledTimes(1);

    const handler = netHandler();

    handler({ isConnected: false });
    expect(useSyncStore.getState().status).toBe('offline_saved');
    expect(flushSpy).not.toHaveBeenCalled();

    handler({ isConnected: true });
    await flushPromises();

    expect(flushSpy).toHaveBeenCalledTimes(1);
    expect(syncLogsSpy).toHaveBeenCalledWith('drv_1');
    expect(flushQueuesSpy).toHaveBeenCalledTimes(1);
    expect(logSpy).toHaveBeenCalledWith(
      expect.stringContaining('Flushed 1 PODs, 3 Expenses, 2 GPS logs')
    );

    // Staying online emits no further reconnect work.
    handler({ isConnected: true });
    await flushPromises();
    expect(flushSpy).toHaveBeenCalledTimes(1);
  });

  test('flush rejection on reconnect warns and keeps the watcher alive', async () => {
    flushSpy.mockRejectedValue(new Error('queue locked'));

    startNetworkWatcher();
    const handler = netHandler();
    handler({ isConnected: false });
    handler({ isConnected: true });
    await flushPromises();

    expect(warnSpy).toHaveBeenCalledWith('[OfflineQueue] Flush failed:', expect.any(Error));
    expect(NetInfoMock.addEventListener).toHaveBeenCalledTimes(1);
  });

  test('stopNetworkWatcher detaches so later events do nothing', async () => {
    startNetworkWatcher();
    const unsubscribe = NetInfoMock.addEventListener.mock.results[0].value as jest.Mock;

    stopNetworkWatcher();
    expect(unsubscribe).toHaveBeenCalledTimes(1);

    stopNetworkWatcher(); // double-stop must be a safe no-op
    expect(unsubscribe).toHaveBeenCalledTimes(1);
  });

  test('reconnect without a signed-in driver skips engine-level sync calls', async () => {
    const syncLogsSpy = jest
      .spyOn(SyncEngine, 'syncPendingLogs')
      .mockResolvedValue({ syncedCount: 0, error: null });
    await useAuthStore.getState().logout();

    startNetworkWatcher();
    const handler = netHandler();
    handler({ isConnected: false });
    handler({ isConnected: true });
    await flushPromises();

    // OfflineQueue.flush still runs (it self-guards on token), but the
    // driver-scoped engine paths are skipped.
    expect(flushSpy).toHaveBeenCalled();
    expect(syncLogsSpy).not.toHaveBeenCalled();
  });

  test('applyNetInfoState leaves status untouched while online', () => {
    useSyncStore.setState({ status: 'online_synced' });
    applyNetInfoState(true);
    expect(useSyncStore.getState().status).toBe('online_synced');
  });
});

describe('auto-sync timer', () => {
  beforeEach(() => {
    resetSQLiteMockState();
    jest.useFakeTimers();
  });

  afterEach(() => {
    SyncEngine.stopAutoSync();
    jest.restoreAllMocks();
    jest.useRealTimers();
  });

  test('interval drives both sync paths; restart guard prevents duplicate timers', async () => {
    const syncLogsSpy = jest
      .spyOn(SyncEngine, 'syncPendingLogs')
      .mockResolvedValue({ syncedCount: 0, error: null });
    const flushQueuesSpy = jest
      .spyOn(SyncEngine, 'flushOfflineQueues')
      .mockResolvedValue({ podsFlushed: 0, expensesFlushed: 0, gpsFlushed: 0 });

    SyncEngine.startAutoSync('drv_1', 1000);
    SyncEngine.startAutoSync('drv_1', 1000); // guarded

    await jest.advanceTimersByTimeAsync(1000);
    expect(syncLogsSpy).toHaveBeenCalledTimes(1); // not doubled by the second start
    expect(flushQueuesSpy).toHaveBeenCalledTimes(1);

    await jest.advanceTimersByTimeAsync(2000);
    expect(syncLogsSpy).toHaveBeenCalledTimes(3);

    SyncEngine.stopAutoSync();
    await jest.advanceTimersByTimeAsync(10000);
    expect(syncLogsSpy).toHaveBeenCalledTimes(3);
  });
});

describe('flushOfflineQueues status transitions', () => {
  beforeEach(() => {
    resetSQLiteMockState();
    useSyncStore.setState({ status: 'online_synced', pendingCount: 0 });
    jest.spyOn(OfflineQueue, 'pendingPODs').mockResolvedValue([{ id: 1 } as never]);
    jest.spyOn(OfflineQueue, 'pendingExpenses').mockResolvedValue([]);
    jest.spyOn(OfflineQueue, 'pendingGPS').mockResolvedValue([]);
  });

  afterEach(() => {
    jest.restoreAllMocks();
  });

  test('flushed > 0 marks the bar synced and publishes the pending count', async () => {
    jest
      .spyOn(OfflineQueue, 'flush')
      .mockResolvedValue({ podsFlushed: 1, gpsFlushed: 0, expensesFlushed: 0 });

    const result = await SyncEngine.flushOfflineQueues();

    expect(result.podsFlushed).toBe(1);
    expect(useSyncStore.getState().status).toBe('online_synced');
    expect(useSyncStore.getState().lastSyncAt).not.toBeNull();
    expect(useSyncStore.getState().pendingCount).toBe(1);
  });

  test('nothing flushed restores the previous status instead of lying about freshness', async () => {
    useSyncStore.setState({ status: 'offline_saved' });
    jest
      .spyOn(OfflineQueue, 'flush')
      .mockResolvedValue({ podsFlushed: 0, gpsFlushed: 0, expensesFlushed: 0 });

    await SyncEngine.flushOfflineQueues();

    expect(useSyncStore.getState().status).toBe('offline_saved');
    expect(useSyncStore.getState().pendingCount).toBe(1); // badge reflects the 1 queued POD
  });

  test('flush crash sets error status but still refreshes the badge count', async () => {
    jest.spyOn(OfflineQueue, 'flush').mockRejectedValue(new Error('db gone'));

    const result = await SyncEngine.flushOfflineQueues();

    expect(result).toEqual({ podsFlushed: 0, expensesFlushed: 0, gpsFlushed: 0 });
    expect(useSyncStore.getState().status).toBe('error');
    expect(useSyncStore.getState().pendingCount).toBe(1);
  });
});

describe('syncPendingLogs branch matrix', () => {
  beforeEach(() => {
    resetSQLiteMockState();
    useSyncStore.setState({ status: 'online_synced', pendingCount: 0 });
  });

  afterEach(() => {
    global.fetch = globalFetchSaved;
    jest.restoreAllMocks();
  });

  test('concurrent invocation is rejected while a cycle is in flight', async () => {
    await DB.logGPSLocation(18.5, 73.8, 5);

    let release!: (v: unknown) => void;
    global.fetch = jest.fn().mockImplementation(
      () =>
        new Promise((resolve) => {
          release = resolve;
        })
    ) as any;

    const first = SyncEngine.syncPendingLogs('drv_1');
    const second = await SyncEngine.syncPendingLogs('drv_1');
    await flushPromises(); // let the in-flight cycle reach its hanging fetch
    expect(second).toEqual({ syncedCount: 0, error: 'Sync already in progress' });
    expect(typeof release).toBe('function');

    release({
      ok: true,
      json: async () => ({ success: true, synced_ids: [1] }),
    });
    const result = await first;
    expect(result.syncedCount).toBe(1);
  });

  test('no unsynced logs → delegates to queue flush and reports clean state', async () => {
    const flushQueuesSpy = jest
      .spyOn(SyncEngine, 'flushOfflineQueues')
      .mockResolvedValue({ podsFlushed: 0, expensesFlushed: 0, gpsFlushed: 0 });

    const result = await SyncEngine.syncPendingLogs('drv_1');

    expect(result).toEqual({ syncedCount: 0, error: null });
    expect(flushQueuesSpy).toHaveBeenCalledTimes(1);
  });

  test('success WITHOUT synced_ids marks the whole batch as synced', async () => {
    await DB.logGPSLocation(18.5, 73.8, 5);
    global.fetch = jest.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ success: true }),
    }) as any;

    const result = await SyncEngine.syncPendingLogs('drv_1');

    expect(result).toEqual({ syncedCount: 1, error: null });
    expect(everyGpsLogSynced()).toBe(true);
  });

  test('synced_ids without success flag still marks rows synced', async () => {
    await DB.logGPSLocation(18.5, 73.8, 5);
    global.fetch = jest.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ synced_ids: [1] }),
    }) as any;

    const result = await SyncEngine.syncPendingLogs('drv_1');

    expect(result).toEqual({ syncedCount: 1, error: null });
    expect(everyGpsLogSynced()).toBe(true);
  });

  test('unrecognized payload shape reports an error and keeps logs unsynced', async () => {
    await DB.logGPSLocation(18.5, 73.8, 5);
    global.fetch = jest.fn().mockResolvedValue({
      ok: true,
      json: async () => ({}),
    }) as any;

    const result = await SyncEngine.syncPendingLogs('drv_1');

    expect(result.syncedCount).toBe(0);
    expect(result.error).toBe('Unexpected server response');
    expect(everyGpsLogSynced()).toBe(false);
  });

  test('transport failure mid-loop records the message and retains logs', async () => {
    await DB.logGPSLocation(18.5, 73.8, 5);
    global.fetch = jest.fn().mockRejectedValue(new Error('socket hang up')) as any;

    const result = await SyncEngine.syncPendingLogs('drv_1');

    expect(result.error).toBe('socket hang up');
    expect(everyGpsLogSynced()).toBe(false);
  });

  test('batches larger than 20 are chunked across multiple POSTs', async () => {
    for (let i = 0; i < 21; i++) {
      await DB.logGPSLocation(18 + i * 0.01, 73 + i * 0.01, i % 2 === 0 ? 4 : null);
    }
    const fetchMock = jest.fn().mockImplementation(async (_url: string, init?: any) => ({
      ok: true,
      json: async () => {
        const body = JSON.parse(init.body);
        return { success: true, synced_ids: body.logs.map((l: any) => l.id) };
      },
    }));
    global.fetch = fetchMock as any;

    const result = await SyncEngine.syncPendingLogs('drv_1');

    expect(fetchMock).toHaveBeenCalledTimes(2); // 20 + 1
    expect(result.syncedCount).toBe(21);
    expect(result.error).toBeNull();
    expect(everyGpsLogSynced()).toBe(true);
  });

  test('DB-layer failure hits the outer guard and reports the underlying error', async () => {
    const dbSpy = jest
      .spyOn(DB, 'getUnsyncedGPSLogs')
      .mockRejectedValue(new Error('database disk I/O error'));
    const logSpy = jest.spyOn(console, 'log').mockImplementation(() => {});

    const result = await SyncEngine.syncPendingLogs('drv_1');

    expect(dbSpy).toHaveBeenCalled();
    expect(result).toEqual({ syncedCount: 0, error: 'database disk I/O error' });
    expect(logSpy).toHaveBeenCalledWith(
      expect.stringContaining('[SYNC ENGINE WARNING]'),
      expect.anything()
    );
  });
});
