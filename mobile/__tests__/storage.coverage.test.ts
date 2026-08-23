// Coverage strengthening for Storage (was 44% lines / 23.8% branches).
// Exercises the AsyncStorage key-value layer and the full SQLite expense/trip
// cache surface against the in-memory sqlite mock.
import AsyncStorage from '@react-native-async-storage/async-storage';
import { DB, Storage } from '../src/services/storage';
import type { Trip } from '../src/types/api';
import { getSQLiteMockState, resetSQLiteMockState } from '../jest/setup';

const store = AsyncStorage as unknown as {
  getItem: jest.Mock;
  setItem: jest.Mock;
  removeItem: jest.Mock;
};

function tripFixture(overrides: Partial<Trip> = {}): Trip {
  return {
    id: 'trip-1',
    tripNumber: 'TRP-1001',
    driverName: 'Rajesh Kumar',
    vehiclePlate: 'MH12AB1234',
    origin: 'Pune',
    destination: 'Mumbai',
    status: 'PENDING',
    startTime: '2026-08-20T08:00:00Z',
    ...overrides,
  };
}

describe('Storage (AsyncStorage key-value layer)', () => {
  beforeEach(() => {
    resetSQLiteMockState();
  });

  test('saveOfflineTrips serializes trips under the stable offline key', async () => {
    const trips = [tripFixture(), tripFixture({ id: 'trip-2', status: 'IN_TRANSIT' })];

    await Storage.saveOfflineTrips(trips);

    expect(store.setItem).toHaveBeenCalledWith(
      '@avandab_offline_trips',
      JSON.stringify(trips)
    );
  });

  test('getOfflineTrips returns [] when nothing is cached', async () => {
    store.getItem.mockResolvedValueOnce(null);

    await expect(Storage.getOfflineTrips()).resolves.toEqual([]);
  });

  test('getOfflineTrips parses the cached JSON payload', async () => {
    const cached = [tripFixture({ id: 'trip-9', destination: 'Nashik' })];
    store.getItem.mockResolvedValueOnce(JSON.stringify(cached));

    const result = await Storage.getOfflineTrips();

    expect(result).toEqual(cached);
    expect(result[0].destination).toBe('Nashik');
  });
});

describe('DB trip cache', () => {
  beforeEach(() => {
    resetSQLiteMockState();
  });

  test('saveTrips persists every trip and getTrips reads them back', async () => {
    const trips = [tripFixture(), tripFixture({ id: 'trip-2', tripNumber: 'TRP-1002' })];

    await DB.saveTrips(trips);
    const rows = await DB.getTrips();

    expect(rows.map((t) => t.id)).toEqual(['trip-1', 'trip-2']);
    expect(rows[0]).toMatchObject({ tripNumber: 'TRP-1001', vehiclePlate: 'MH12AB1234' });
  });

  test('saveTrips upserts on duplicate id instead of duplicating rows', async () => {
    await DB.saveTrips([tripFixture()]);
    await DB.saveTrips([tripFixture({ status: 'COMPLETED' })]);

    const rows = await DB.getTrips();
    expect(rows).toHaveLength(1);
    expect(rows[0].status).toBe('COMPLETED');
  });
});

describe('DB GPS helpers edge cases', () => {
  beforeEach(() => {
    resetSQLiteMockState();
  });

  test('markLogsAsSynced with an empty id list is a no-op that still resolves', async () => {
    await expect(DB.markLogsAsSynced([])).resolves.toBeUndefined();
    expect(getSQLiteMockState().offline_gps_logs).toHaveLength(0);
  });
});

describe('DB offline-expense cache', () => {
  beforeEach(() => {
    resetSQLiteMockState();
  });

  test('saveOfflineExpense persists full row; getters expose it in insertion order', async () => {
    await DB.saveOfflineExpense({
      trip_id: 'trip-1',
      expense_type: 'fuel',
      amount: 2500,
      receipt_uri: 'file:///receipt.jpg',
      notes: 'HPCL pump',
      latitude: 18.5204,
      longitude: 73.8567,
    });
    await DB.saveOfflineExpense({
      trip_id: 'trip-1',
      expense_type: 'toll',
      amount: 180,
    });

    const all = await DB.getOfflineExpenses();
    expect(all).toHaveLength(2);
    expect(all[0]).toMatchObject({
      trip_id: 'trip-1',
      expense_type: 'fuel',
      amount: 2500,
      receipt_uri: 'file:///receipt.jpg',
      notes: 'HPCL pump',
      latitude: 18.5204,
      longitude: 73.8567,
    });
    // Optional fields absent at insert become explicit nulls.
    expect(all[1].receipt_uri).toBeNull();
    expect(all[1].latitude).toBeNull();

    const pending = await DB.getPendingOfflineExpenses();
    expect(pending.map((e) => e.expense_type)).toEqual(['fuel', 'toll']);
  });

  test('clearOfflineExpense removes a single row by id', async () => {
    await DB.saveOfflineExpense({ trip_id: 't1', expense_type: 'fuel', amount: 100 });
    await DB.saveOfflineExpense({ trip_id: 't1', expense_type: 'toll', amount: 50 });

    const rows = await DB.getOfflineExpenses();
    await DB.clearOfflineExpense(rows[0].id);

    const remaining = await DB.getOfflineExpenses();
    expect(remaining).toHaveLength(1);
    expect(remaining[0].expense_type).toBe('toll');
  });

  test('clearOfflineExpenses handles batch removal and empty-list no-op', async () => {
    await DB.saveOfflineExpense({ trip_id: 't1', expense_type: 'fuel', amount: 100 });
    await DB.saveOfflineExpense({ trip_id: 't2', expense_type: 'food', amount: 70 });

    await expect(DB.clearOfflineExpenses([])).resolves.not.toThrow();
    let rows = await DB.getOfflineExpenses();
    expect(rows).toHaveLength(2);

    await DB.clearOfflineExpenses(rows.map((r) => r.id));
    rows = await DB.getOfflineExpenses();
    expect(rows).toHaveLength(0);
  });
});
