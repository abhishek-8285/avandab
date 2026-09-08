import { DB } from '../src/services/storage';
import { getSQLiteMockState, resetSQLiteMockState } from '../jest/setup';

describe('DB GPS log storage (accuracy support)', () => {
  beforeEach(() => {
    resetSQLiteMockState();
  });

  test('logGPSLocation persists accuracy alongside coordinates', async () => {
    await DB.logGPSLocation(18.5204, 73.8567, 12.5);

    const state = getSQLiteMockState();
    expect(state.offline_gps_logs).toHaveLength(1);
    expect(state.offline_gps_logs[0].latitude).toBe(18.5204);
    expect(state.offline_gps_logs[0].longitude).toBe(73.8567);
    expect(state.offline_gps_logs[0].accuracy).toBe(12.5);
  });

  test('logGPSLocation tolerates missing accuracy (stored as null)', async () => {
    await DB.logGPSLocation(19.076, 72.8777);

    const state = getSQLiteMockState();
    expect(state.offline_gps_logs[0].accuracy).toBeNull();
  });

  test('getUnsyncedGPSLogs exposes accuracy_m and filters synced rows', async () => {
    await DB.logGPSLocation(18.5204, 73.8567, 8.0);

    let logs = await DB.getUnsyncedGPSLogs();
    expect(logs).toHaveLength(1);
    expect(logs[0].accuracy_m).toBe(8.0);

    await DB.markLogsAsSynced([logs[0].id]);
    logs = await DB.getUnsyncedGPSLogs();
    expect(logs).toHaveLength(0);
  });
});

describe('DB trips + expenses cache', () => {
  beforeEach(() => {
    resetSQLiteMockState();
  });

  test('saveTrips/getTrips roundtrip', async () => {
    await DB.saveTrips([
      { id: 't1', tripNumber: 'TRP-1', driverName: 'A', vehiclePlate: 'MH-01', origin: 'Pune', destination: 'Mumbai', status: 'open', startTime: '2026-01-01' } as any,
    ]);
    const trips = await DB.getTrips();
    expect(trips).toHaveLength(1);
    expect(trips[0].tripNumber).toBe('TRP-1');
  });

  test('markLogsAsSynced clears only given ids; empty is a no-op', async () => {
    await DB.logGPSLocation(1, 2, 3);
    await DB.logGPSLocation(4, 5, 6);
    await DB.markLogsAsSynced([]);
    expect(await DB.getUnsyncedGPSLogs()).toHaveLength(2);
    const logs = await DB.getUnsyncedGPSLogs();
    await DB.markLogsAsSynced([logs[0].id]);
    expect(await DB.getUnsyncedGPSLogs()).toHaveLength(1);
  });

  test('offline expenses save/get/clear roundtrip', async () => {
    await DB.saveOfflineExpense({ trip_id: 't1', expense_type: 'fuel', amount: 500 });
    expect(await DB.getOfflineExpenses()).toHaveLength(1);
    expect(await DB.getPendingOfflineExpenses()).toHaveLength(1);
    const exps = await DB.getOfflineExpenses();
    await DB.clearOfflineExpense(exps[0].id);
    expect(await DB.getOfflineExpenses()).toHaveLength(0);
    await DB.saveOfflineExpense({ trip_id: 't1', expense_type: 'fuel', amount: 100 });
    await DB.saveOfflineExpense({ trip_id: 't1', expense_type: 'food', amount: 50 });
    const exps2 = await DB.getOfflineExpenses();
    await DB.clearOfflineExpenses(exps2.map((e) => e.id));
    expect(await DB.getOfflineExpenses()).toHaveLength(0);
    await DB.clearOfflineExpenses([]);
  });
});
