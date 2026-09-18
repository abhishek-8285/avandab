import { SyncEngine } from '../src/services/syncEngine';
import { DB } from '../src/services/storage';
import { resetSQLiteMockState, getSQLiteMockState } from '../jest/setup';
import { useAuthStore } from '../src/stores/authStore';

const globalFetch = global.fetch;

describe('SyncEngine GPS flush', () => {
  beforeEach(async () => {
    resetSQLiteMockState();
    await useAuthStore.getState().setAuth('tok', {
      id: 'u_1',
      name: 'Raj',
      role: 'driver',
      email: 'r@x.com',
      driverId: 'drv_1',
    });
    await DB.logGPSLocation(18.5204, 73.8567, 11.2);
    await DB.logGPSLocation(18.5210, 73.8570, null);
  });

  afterEach(() => {
    global.fetch = globalFetch;
  });

  test('posts accuracy_m when present and marks synced ids', async () => {
    const fetchMock = jest.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ success: true, synced_ids: [1, 2] }),
    });
    global.fetch = fetchMock as any;

    const res = await SyncEngine.syncPendingLogs('drv_1');
    expect(res.error).toBeNull();
    expect(res.syncedCount).toBe(2);

    const body = JSON.parse(fetchMock.mock.calls[0][1].body);
    expect(body.driver_id).toBe('drv_1');
    expect(body.logs).toHaveLength(2);
    expect(body.logs[0]).toEqual({
      id: 1,
      latitude: 18.5204,
      longitude: 73.8567,
      timestamp: expect.any(String),
      accuracy_m: 11.2,
    });
    // null accuracy must be omitted, never sent as 0 or fake value
    expect(body.logs[1]).toEqual({
      id: 2,
      latitude: 18.521,
      longitude: 73.857,
      timestamp: expect.any(String),
    });

    expect(getSQLiteMockState().offline_gps_logs.every((l) => l.synced === 1)).toBe(true);
  });

  test('stale re-observations sync ONLY flagged is_stale; fresh rows omit the flag', async () => {
    await DB.logGPSLocation(19.09, 72.9, 10, { isStale: true });
    const fetchMock = jest.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ success: true, synced_ids: [1, 2, 3] }),
    });
    global.fetch = fetchMock as any;

    await SyncEngine.syncPendingLogs('drv_1');

    const body = JSON.parse(fetchMock.mock.calls[0][1].body);
    expect(body.logs).toHaveLength(3);
    expect(body.logs[0].is_stale).toBeUndefined(); // fresh — additive only
    expect(body.logs[1].is_stale).toBeUndefined();
    expect(body.logs[2]).toMatchObject({ latitude: 19.09, is_stale: true });
  });

  test('server failure retains unsynced logs and reports error', async () => {
    global.fetch = jest.fn().mockResolvedValue({
      ok: false,
      status: 500,
      json: async () => ({ error: 'boom' }),
    }) as any;

    const res = await SyncEngine.syncPendingLogs('drv_1');
    expect(res.syncedCount).toBe(0);
    expect(res.error).toContain('500');
    expect(getSQLiteMockState().offline_gps_logs.some((l) => l.synced === 0)).toBe(true);
  });
});
