import { OfflineQueue } from '../src/services/offlineQueue';
import { resetSQLiteMockState } from '../jest/setup';
import { useAuthStore } from '../src/stores/authStore';

// Mock fetch globally
const globalFetch = global.fetch;

describe('OfflineQueue', () => {
  beforeEach(async () => {
    resetSQLiteMockState();
    await OfflineQueue.init();
    await useAuthStore.getState().setAuth('mock_token_123', {
      id: 'u_1',
      name: 'Rajesh Kumar',
      role: 'driver',
      email: 'driver@avandab.com',
      driverId: 'drv_1',
    });
  });

  afterEach(() => {
    global.fetch = globalFetch;
  });

  test('enqueuePOD stores in SQLite and pendingPODs returns it', async () => {
    await OfflineQueue.enqueuePOD('trip_101', {
      consignee_name: 'Acme Logistics',
      notes: 'Received at Dock 4',
      photo_uri: 'file:///photo.jpg',
      latitude: 18.5204,
      longitude: 73.8567,
    });

    const pods = await OfflineQueue.pendingPODs();
    expect(pods).toHaveLength(1);
    expect(pods[0].trip_id).toBe('trip_101');
    expect(pods[0].consignee_name).toBe('Acme Logistics');
    expect(pods[0].notes).toBe('Received at Dock 4');
  });

  test('enqueuePOD dedupes by tripId (no duplicate rows)', async () => {
    await OfflineQueue.enqueuePOD('trip_102', { consignee_name: 'Buyer A' });
    await OfflineQueue.enqueuePOD('trip_102', { consignee_name: 'Buyer A duplicate' });

    const pods = await OfflineQueue.pendingPODs();
    expect(pods.filter((p) => p.trip_id === 'trip_102')).toHaveLength(1);
  });

  test('clearPOD removes the row', async () => {
    await OfflineQueue.enqueuePOD('trip_103', { consignee_name: 'Buyer B' });
    await OfflineQueue.clearPOD('trip_103');

    const pods = await OfflineQueue.pendingPODs();
    expect(pods.find((p) => p.trip_id === 'trip_103')).toBeUndefined();
  });

  test('clearGPS with empty array does nothing', async () => {
    await expect(OfflineQueue.clearGPS([])).resolves.not.toThrow();
  });

  test('enqueueGPS stores, pendingGPS returns, clearGPS removes', async () => {
    await OfflineQueue.enqueueGPS({
      driver_id: 'drv_1',
      latitude: 18.5204,
      longitude: 73.8567,
      timestamp: new Date().toISOString(),
      accuracy_m: 5.0,
    });

    const gpsLogs = await OfflineQueue.pendingGPS();
    expect(gpsLogs).toHaveLength(1);
    expect(gpsLogs[0].driver_id).toBe('drv_1');

    await OfflineQueue.clearGPS([gpsLogs[0].id]);
    const afterClear = await OfflineQueue.pendingGPS();
    expect(afterClear).toHaveLength(0);
  });

  test('flush with network succeeds and clears flushed items with photos and coordinates', async () => {
    await OfflineQueue.enqueuePOD('trip_flush_1', {
      consignee_name: 'Receiver 1',
      notes: 'Dock notes',
      photo_uri: 'file:///photo.jpg',
      latitude: 18.5204,
      longitude: 73.8567,
    });
    await OfflineQueue.enqueueGPS({
      driver_id: 'drv_1',
      latitude: 18.5204,
      longitude: 73.8567,
      timestamp: new Date().toISOString(),
    });

    global.fetch = jest.fn().mockImplementation(async (url: string) => {
      if (url.includes('/deliver-pod')) {
        return { ok: true, json: async () => ({ status: 'delivered', trip_number: 'TRP-1' }) };
      }
      if (url.includes('/telemetry/sync')) {
        return { ok: true, json: async () => ({ success: true, synced_ids: [1] }) };
      }
      return { ok: true, json: async () => ({}) };
    }) as any;

    const result = await OfflineQueue.flush();
    expect(result.podsFlushed).toBe(1);
    expect(result.gpsFlushed).toBe(1);

    const remainingPods = await OfflineQueue.pendingPODs();
    expect(remainingPods).toHaveLength(0);
  });

  test('flush without token stops early', async () => {
    await useAuthStore.getState().logout();
    await OfflineQueue.enqueuePOD('trip_unauth', { consignee_name: 'Unauth' });
    await OfflineQueue.enqueueGPS({ driver_id: 'd1', latitude: 1, longitude: 2, timestamp: '' });

    const result = await OfflineQueue.flush();
    expect(result.podsFlushed).toBe(0);
    expect(result.gpsFlushed).toBe(0);
  });

  test('flush without network leaves items in queue', async () => {
    await OfflineQueue.enqueuePOD('trip_flush_offline', { consignee_name: 'Receiver Offline' });
    await OfflineQueue.enqueueGPS({ driver_id: 'd1', latitude: 1, longitude: 2, timestamp: '' });

    global.fetch = jest.fn().mockRejectedValue(new Error('Network error')) as any;

    const result = await OfflineQueue.flush();
    expect(result.podsFlushed).toBe(0);
    expect(result.gpsFlushed).toBe(0);

    const remaining = await OfflineQueue.pendingPODs();
    expect(remaining).toHaveLength(1);
  });

  test('enqueueExpense stores, pendingExpenses returns, clearExpense removes', async () => {
    await OfflineQueue.enqueueExpense({
      trip_id: 'trip_exp_1',
      expense_type: 'fuel',
      amount: 1200,
      receipt_uri: 'file:///receipt.jpg',
      notes: 'Diesel refill',
      latitude: 18.5204,
      longitude: 73.8567,
    });

    const expenses = await OfflineQueue.pendingExpenses();
    expect(expenses).toHaveLength(1);
    expect(expenses[0].trip_id).toBe('trip_exp_1');
    expect(expenses[0].amount).toBe(1200);

    await OfflineQueue.clearExpense(expenses[0].id);
    expect(await OfflineQueue.pendingExpenses()).toHaveLength(0);
  });

  test('idempotency_key persists through queue and flush sends it', async () => {
    const key = `exp-${Date.now()}-testkey`;
    await OfflineQueue.enqueueExpense({
      trip_id: 'trip_exp_idem',
      expense_type: 'toll',
      amount: 300,
      idempotency_key: key,
    });

    const queued = await OfflineQueue.pendingExpenses();
    const match = queued.find((e) => e.trip_id === 'trip_exp_idem');
    expect(match?.idempotency_key).toBe(key);
    await OfflineQueue.clearExpense(match!.id);

    // Flush path: the queued key must reach the backend form data
    await OfflineQueue.enqueueExpense({
      trip_id: 'trip_exp_flush2',
      expense_type: 'food',
      amount: 90,
      idempotency_key: key,
    });

    global.fetch = jest.fn().mockImplementation(async (url: string) => {
      if (url.includes('/kharcha/expense')) {
        return { ok: true, json: async () => ({}) };
      }
      return { ok: true, json: async () => ({}) };
    }) as any;

    // Spy on FormData appends — works across RN FormData implementations.
    const appended: Record<string, unknown> = {};
    const RealFD = global.FormData;
    class SpyFormData extends RealFD {
      append(name: string, value: any) {
        appended[name] = value;
        super.append(name, value);
      }
    }
    global.FormData = SpyFormData as any;

    await OfflineQueue.flush();
    global.FormData = RealFD;

    expect(appended.trip_id).toBe('trip_exp_flush2');
    expect(appended.idempotency_key).toBe(key);
  });

  test('clearExpenses removes many; empty array is a no-op', async () => {
    await OfflineQueue.enqueueExpense({ trip_id: 't1', expense_type: 'toll', amount: 100 });
    await OfflineQueue.enqueueExpense({ trip_id: 't2', expense_type: 'toll', amount: 200 });
    await expect(OfflineQueue.clearExpenses([])).resolves.not.toThrow();

    const expenses = await OfflineQueue.pendingExpenses();
    expect(expenses).toHaveLength(2);
    await OfflineQueue.clearExpenses(expenses.map((e) => e.id));
    expect(await OfflineQueue.pendingExpenses()).toHaveLength(0);
  });

  test('flush uploads expenses with receipt and coords then clears', async () => {
    await OfflineQueue.enqueueExpense({
      trip_id: 'trip_exp_flush',
      expense_type: 'fuel',
      amount: 1500,
      receipt_uri: 'file:///receipt.jpg',
      notes: 'HP pump',
      latitude: 18.5204,
      longitude: 73.8567,
    });

    global.fetch = jest.fn().mockImplementation(async (url: string) => {
      if (url.includes('/kharcha/expense')) {
        return { ok: true, json: async () => ({}) };
      }
      return { ok: true, json: async () => ({}) };
    }) as any;

    const result = await OfflineQueue.flush();
    expect(result.expensesFlushed).toBe(1);
    expect(await OfflineQueue.pendingExpenses()).toHaveLength(0);
  });

  test('flush keeps expenses when server rejects or network fails', async () => {
    await OfflineQueue.enqueueExpense({ trip_id: 't_reject', expense_type: 'fuel', amount: 500 });
    await OfflineQueue.enqueueExpense({ trip_id: 't_offline', expense_type: 'toll', amount: 60 });

    global.fetch = jest.fn().mockImplementation(async (url: string) => {
      if (url.includes('/kharcha/expense')) {
        return { ok: false, status: 400, json: async () => ({}) };
      }
      throw new Error('Network down');
    }) as any;

    const result = await OfflineQueue.flush();
    expect(result.expensesFlushed).toBe(0);
    expect(await OfflineQueue.pendingExpenses()).toHaveLength(2);
  });

  test('flush sends every optional POD field when present', async () => {
    await OfflineQueue.enqueuePOD('trip_full_pod', {
      consignee_name: 'Full Fields',
      consignee_phone: '+919876543210',
      notes: 'Gate pass required',
      pod_signature_data: 'data:image/png;base64,AAAA',
      quantity_short: 2,
      damage_qty: 1,
      refusal_reason: 'Damaged carton refused',
    });

    const sentForms: any[] = [];
    global.fetch = jest.fn().mockImplementation(async (_url: string, opts: any) => {
      sentForms.push(opts.body);
      return { ok: true, json: async () => ({}) };
    }) as any;

    const result = await OfflineQueue.flush();
    expect(result.podsFlushed).toBe(1);

    const form = sentForms[0];
    expect(form.get('consignee_phone')).toBe('+919876543210');
    expect(form.get('pod_signature_data')).toBe('data:image/png;base64,AAAA');
    expect(form.get('signature_dataurl')).toBe('data:image/png;base64,AAAA');
    expect(form.get('quantity_short')).toBe('2');
    expect(form.get('damage_qty')).toBe('1');
    expect(form.get('refusal_reason')).toBe('Damaged carton refused');
  });

  test('flush keeps POD queued when server rejects it', async () => {
    await OfflineQueue.enqueuePOD('trip_rejected', { consignee_name: 'Reject Me' });

    global.fetch = jest.fn().mockResolvedValue({ ok: false, status: 422, json: async () => ({}) }) as any;

    const result = await OfflineQueue.flush();
    expect(result.podsFlushed).toBe(0);
    expect(await OfflineQueue.pendingPODs()).toHaveLength(1);
  });

  test('flush clears only synced_ids when GPS sync reports partial success', async () => {
    await OfflineQueue.enqueueGPS({ driver_id: 'drv_1', latitude: 1, longitude: 2, timestamp: '2026-08-20T10:00:00Z' });
    await OfflineQueue.enqueueGPS({ driver_id: 'drv_1', latitude: 3, longitude: 4, timestamp: '2026-08-20T10:01:00Z' });

    global.fetch = jest.fn().mockImplementation(async (url: string) => {
      if (url.includes('/telemetry/sync')) {
        return { ok: true, json: async () => ({ success: false, synced_ids: [1] }) };
      }
      return { ok: true, json: async () => ({}) };
    }) as any;

    const result = await OfflineQueue.flush();
    expect(result.gpsFlushed).toBe(1);

    const remaining = await OfflineQueue.pendingGPS();
    expect(remaining).toHaveLength(1);
    expect(remaining[0].latitude).toBe(3);
  });

  test('GPS flush sends per-log ids and falls back to success-clear on legacy ack', async () => {
    await OfflineQueue.enqueueGPS({
      driver_id: 'drv_1',
      latitude: 18.5204,
      longitude: 73.8567,
      timestamp: new Date().toISOString(),
    });

    let sentBody: any = null;
    global.fetch = jest.fn().mockImplementation(async (url: string, opts: any) => {
      if (url.includes('/telemetry/sync')) {
        sentBody = JSON.parse(opts.body);
        // Legacy server: success without synced_ids.
        return { ok: true, json: async () => ({ success: true }) };
      }
      return { ok: true, json: async () => ({}) };
    }) as any;

    const result = await OfflineQueue.flush();
    expect(result.gpsFlushed).toBe(1);
    expect(sentBody.logs).toHaveLength(1);
    expect(sentBody.logs[0].id).toBeDefined();
    expect(await OfflineQueue.pendingGPS()).toHaveLength(0);
  });
});
