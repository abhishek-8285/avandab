// Red-green ownership: mobile session-ownership defects.
// Proves account-switch isolation:
// - partition cache/queues by account identity
// - capture owner at enqueue
// - flush only owner work
// - retain (never delete) unsynced work on logout
import { DB } from '../src/services/storage';
import { OfflineQueue } from '../src/services/offlineQueue';
import { useAuthStore } from '../src/stores/authStore';
import { resetSQLiteMockState, getSQLiteMockState } from '../jest/setup';

const globalFetch = global.fetch;

const userA = { id: 'u_A', name: 'Driver A', role: 'driver', email: 'a@x.com', driverId: 'drv_A' };
const userB = { id: 'u_B', name: 'Driver B', role: 'driver', email: 'b@x.com', driverId: 'drv_B' };

async function loginAs(u: typeof userA, token: string) {
  await useAuthStore.getState().setAuth(token, u as any);
}

describe('session ownership: trip cache partitioned by account', () => {
  beforeEach(async () => {
    resetSQLiteMockState();
    await useAuthStore.getState().logout();
  });
  afterEach(() => { global.fetch = globalFetch; });

  test('account B must not see account A trips via shared cache fallback', async () => {
    await loginAs(userA, 'tokA');
    await DB.saveTrips([
      { id: 'trip-A1', tripNumber: 'TRP-A1', driverName: 'A', vehiclePlate: 'MH-01', origin: 'Pune', destination: 'Mumbai', status: 'PENDING', startTime: '2026-01-01' } as any,
    ]);
    await loginAs(userB, 'tokB');
    await DB.saveTrips([
      { id: 'trip-B1', tripNumber: 'TRP-B1', driverName: 'B', vehiclePlate: 'MH-02', origin: 'Nashik', destination: 'Thane', status: 'PENDING', startTime: '2026-01-02' } as any,
    ]);

    await loginAs(userA, 'tokA');
    const tripsA = await DB.getTrips();
    expect(tripsA.map((t) => t.id)).toEqual(['trip-A1']);

    await loginAs(userB, 'tokB');
    const tripsB = await DB.getTrips();
    expect(tripsB.map((t) => t.id)).toEqual(['trip-B1']);
  });
});

describe('session ownership: offline queues capture owner, flush only owner', () => {
  beforeEach(async () => {
    resetSQLiteMockState();
    await OfflineQueue.init();
    await useAuthStore.getState().logout();
  });
  afterEach(() => { global.fetch = globalFetch; });

  test('POD queue partitioned; flush as B leaves A queued', async () => {
    await loginAs(userA, 'tokA');
    await OfflineQueue.enqueuePOD('trip_A_pod', { consignee_name: 'A Receiver' });
    await loginAs(userB, 'tokB');
    await OfflineQueue.enqueuePOD('trip_B_pod', { consignee_name: 'B Receiver' });

    // B's view must not include A's work
    const pendingB = await OfflineQueue.pendingPODs();
    expect(pendingB.map((p) => p.trip_id).sort()).toEqual(['trip_B_pod']);

    global.fetch = jest.fn().mockImplementation(async (url: string) => {
      if (url.includes('/deliver-pod') || url.includes('/pod')) return { ok: true, json: async () => ({}) };
      if (url.includes('/telemetry/sync')) return { ok: true, json: async () => ({ success: true, synced_ids: [] }) };
      return { ok: true, json: async () => ({}) };
    }) as any;

    const res = await OfflineQueue.flush();
    expect(res.podsFlushed).toBe(1);

    // A's work retained (never deleted, never flushed under B)
    const state = getSQLiteMockState();
    expect(state.queued_pods.map((p) => p.trip_id)).toEqual(['trip_A_pod']);

    // A can still flush own work after switching back
    await loginAs(userA, 'tokA');
    const resA = await OfflineQueue.flush();
    expect(resA.podsFlushed).toBe(1);
    expect(await OfflineQueue.pendingPODs()).toHaveLength(0);
  });

  test('GPS queue flushed under correct owner only (no cross-driver attribution)', async () => {
    await loginAs(userA, 'tokA');
    await OfflineQueue.enqueueGPS({ driver_id: 'drv_A', latitude: 18.5, longitude: 73.8, timestamp: '2026-08-20T10:00:00Z' });
    await loginAs(userB, 'tokB');
    await OfflineQueue.enqueueGPS({ driver_id: 'drv_B', latitude: 19.0, longitude: 72.8, timestamp: '2026-08-20T11:00:00Z' });

    let sentBody: any = null;
    global.fetch = jest.fn().mockImplementation(async (url: string, opts: any) => {
      if (url.includes('/telemetry/sync')) {
        sentBody = JSON.parse(opts.body);
        // Server acks whatever it receives
        return { ok: true, json: async () => ({ success: true, synced_ids: sentBody.logs.map((l: any) => l.id) }) };
      }
      return { ok: true, json: async () => ({}) };
    }) as any;

    const res = await OfflineQueue.flush();
    expect(res.gpsFlushed).toBe(1);
    // Must attribute to current session only, never A's fix under B's id
    expect(sentBody.driver_id).toBe('drv_B');
    expect(sentBody.logs).toHaveLength(1);
    expect(sentBody.logs[0].latitude).toBe(19.0);

    // B's view drained; A's work retained in store (never deleted)
    expect(await OfflineQueue.pendingGPS()).toHaveLength(0);
    expect(getSQLiteMockState().queued_gps).toHaveLength(1);
    expect(getSQLiteMockState().queued_gps[0].latitude).toBe(18.5);
    // Owner A resumes and still sees own fix
    await loginAs(userA, 'tokA');
    const remaining = await OfflineQueue.pendingGPS();
    expect(remaining).toHaveLength(1);
    expect(remaining[0].latitude).toBe(18.5);
  });

  test('expense queue partitioned; flush as B leaves A queued', async () => {
    await loginAs(userA, 'tokA');
    await OfflineQueue.enqueueExpense({ trip_id: 'tA', expense_type: 'fuel', amount: 500 });
    await loginAs(userB, 'tokB');
    await OfflineQueue.enqueueExpense({ trip_id: 'tB', expense_type: 'toll', amount: 60 });

    const pendingB = await OfflineQueue.pendingExpenses();
    expect(pendingB.map((e) => e.trip_id)).toEqual(['tB']);

    global.fetch = jest.fn().mockImplementation(async (url: string) => {
      if (url.includes('/kharcha/expense')) return { ok: true, json: async () => ({}) };
      return { ok: true, json: async () => ({}) };
    }) as any;
    const res = await OfflineQueue.flush();
    expect(res.expensesFlushed).toBe(1);
    await loginAs(userA, 'tokA');
    expect((await OfflineQueue.pendingExpenses()).map((e) => e.trip_id)).toEqual(['tA']);
  });
});

describe('session ownership: logout retains (never deletes) unsynced work', () => {
  beforeEach(async () => {
    resetSQLiteMockState();
    await OfflineQueue.init();
    await useAuthStore.getState().logout();
  });
  afterEach(() => { global.fetch = globalFetch; });

  test('logout keeps queues; next account isolated; owner can resume', async () => {
    await loginAs(userA, 'tokA');
    await OfflineQueue.enqueuePOD('trip_retain', { consignee_name: 'Retain Me' });
    await OfflineQueue.enqueueGPS({ driver_id: 'drv_A', latitude: 18.5, longitude: 73.8, timestamp: '2026-08-20T10:00:00Z' });
    await DB.saveTrips([
      { id: 'trip-retain', tripNumber: 'TRP-R', driverName: 'A', vehiclePlate: 'MH-01', origin: 'Pune', destination: 'Mumbai', status: 'PENDING', startTime: '2026-01-01' } as any,
    ]);

    await useAuthStore.getState().logout();

    // Retained, never deleted on logout
    const raw = getSQLiteMockState();
    expect(raw.queued_pods.map((p) => p.trip_id)).toContain('trip_retain');
    expect(raw.queued_gps).toHaveLength(1);
    expect(raw.trips.map((t) => t.id)).toContain('trip-retain');

    // Next account sees none of it
    await loginAs(userB, 'tokB');
    expect(await OfflineQueue.pendingPODs()).toHaveLength(0);
    expect(await OfflineQueue.pendingGPS()).toHaveLength(0);
    expect(await DB.getTrips()).toHaveLength(0);

    // Owner returns and resumes own work
    await loginAs(userA, 'tokA');
    expect((await OfflineQueue.pendingPODs()).map((p) => p.trip_id)).toEqual(['trip_retain']);
    expect((await DB.getTrips()).map((t) => t.id)).toEqual(['trip-retain']);
  });

  test('storage GPS logs partitioned by account', async () => {
    await loginAs(userA, 'tokA');
    await DB.logGPSLocation(18.5, 73.8, 5);
    await loginAs(userB, 'tokB');
    await DB.logGPSLocation(19.0, 72.8, 6);

    await loginAs(userA, 'tokA');
    const logsA = await DB.getUnsyncedGPSLogs();
    expect(logsA).toHaveLength(1);
    expect(logsA[0].latitude).toBe(18.5);

    await loginAs(userB, 'tokB');
    const logsB = await DB.getUnsyncedGPSLogs();
    expect(logsB).toHaveLength(1);
    expect(logsB[0].latitude).toBe(19.0);
  });
});
