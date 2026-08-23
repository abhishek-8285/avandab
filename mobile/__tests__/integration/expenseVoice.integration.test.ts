// Integration layer: voice expense chain — speech parsing → queue persistence →
// multipart flush with idempotency contract. Verifies the client half of the
// server-side ON CONFLICT(idempotency_key) dedupe: every new utterance gets a
// fresh unique key, and retries of the SAME queued row reuse its key.
import { buildExpenseDraft, parseExpenseUtterance } from '../../src/services/speech';
import { OfflineQueue } from '../../src/services/offlineQueue';
import { getApiBaseURL } from '../../src/constants/network';
import { resetSQLiteMockState } from '../../jest/setup';
import { useAuthStore } from '../../src/stores/authStore';

const globalFetch = global.fetch;
const RealFormData = global.FormData;

const UTTERANCE = 'Diesel ₹2500 at HPCL pump near Pune station';

class RecordingFormData extends RealFormData {
  static records: { name: string; value: unknown }[] = [];

  append(name: string, value: any): void {
    RecordingFormData.records.push({ name, value });
    super.append(name, value);
  }

  static last(): Record<string, unknown> {
    const out: Record<string, unknown> = {};
    for (const { name, value } of RecordingFormData.records) out[name] = value;
    return out;
  }
}

describe('voice expense integration (speech → queue → kharcha flush)', () => {
  beforeEach(async () => {
    resetSQLiteMockState();
    RecordingFormData.records = [];
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
    global.FormData = RealFormData;
  });

  test('utterance parses to fuel/2500/HPCL and drafts carry a UUID v4 idempotency key', () => {
    const parsed = parseExpenseUtterance(UTTERANCE);
    expect(parsed).toEqual({ amount: 2500, vendor: 'HPCL', category: 'fuel', dateHint: null });

    const draft = buildExpenseDraft(UTTERANCE, 'trip-888');
    expect(draft.trip_id).toBe('trip-888');
    expect(draft.expense_type).toBe('fuel');
    expect(draft.amount).toBe(2500);
    expect(draft.notes).toBe(UTTERANCE);
    expect(draft.idempotency_key).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i
    );
  });

  test('queued voice expense flushes to /kharcha/expense with trip_id, type, stringified amount and its key', async () => {
    const draft = buildExpenseDraft(UTTERANCE, 'trip-888');
    await OfflineQueue.enqueueExpense(draft);
    expect(await OfflineQueue.pendingExpenses()).toHaveLength(1);

    const fetchMock = jest.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ success: true }),
    });
    global.fetch = fetchMock as any;
    global.FormData = RecordingFormData as any;

    const result = await OfflineQueue.flush();
    expect(result.expensesFlushed).toBe(1);

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe(`${getApiBaseURL()}/api/v1/kharcha/expense`);
    expect(init.method).toBe('POST');
    expect(init.headers.Authorization).toBe('Bearer mock_token_123');

    const form = RecordingFormData.last();
    expect(form.trip_id).toBe('trip-888');
    expect(form.expense_type).toBe('fuel');
    expect(form.type).toBe('fuel');
    expect(form.amount).toBe('2500'); // stringified on the wire
    expect(form.notes).toBe(UTTERANCE);
    expect(form.idempotency_key).toBe(draft.idempotency_key);

    // Queue drained after success.
    expect(await OfflineQueue.pendingExpenses()).toEqual([]);
  });

  test('second identical utterance produces a DISTINCT idempotency key (client-side dedupe contract)', async () => {
    const fetchMock = jest.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ success: true }),
    });
    global.fetch = fetchMock as any;
    global.FormData = RecordingFormData as any;

    // First utterance → enqueue → flush.
    const draft1 = buildExpenseDraft(UTTERANCE, 'trip-888');
    await OfflineQueue.enqueueExpense(draft1);
    await OfflineQueue.flush();
    expect(
      RecordingFormData.records.filter((r) => r.name === 'idempotency_key').map((r) => r.value)
    ).toEqual([draft1.idempotency_key]);

    // Second identical utterance later (queue already empty) → new key.
    const draft2 = buildExpenseDraft(UTTERANCE, 'trip-888');
    expect(draft2.idempotency_key).not.toBe(draft1.idempotency_key);
    await OfflineQueue.enqueueExpense(draft2);
    await OfflineQueue.flush();

    expect(fetchMock).toHaveBeenCalledTimes(2);
    const keysOnWire = RecordingFormData.records
      .filter((r) => r.name === 'idempotency_key')
      .map((r) => r.value);
    expect(keysOnWire).toEqual([draft1.idempotency_key, draft2.idempotency_key]);
    expect(await OfflineQueue.pendingExpenses()).toEqual([]);
  });

  test('network failure keeps the expense queued — no data loss', async () => {
    const draft = buildExpenseDraft(UTTERANCE, 'trip-888');
    await OfflineQueue.enqueueExpense(draft);

    global.fetch = jest.fn().mockRejectedValue(new Error('Network unreachable')) as any;

    const result = await OfflineQueue.flush();
    expect(result.expensesFlushed).toBe(0);

    const remaining = await OfflineQueue.pendingExpenses();
    expect(remaining).toHaveLength(1);
    expect(remaining[0].idempotency_key).toBe(draft.idempotency_key);
    expect(remaining[0].amount).toBe(2500);
  });
});
