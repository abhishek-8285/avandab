// Coverage strengthening for TripPoller edge branches: UUID fallback without
// crypto.randomUUID, double-start guard, HTTP poll failures, envelope-shape
// tolerance, exhausted assign retries, and throwing listeners.
import { TripPoller } from '../src/services/tripPoller';
import { useAuthStore } from '../src/stores/authStore';

const globalFetch = global.fetch;

function okResponse(body: unknown) {
  return { ok: true, status: 200, json: async () => body };
}

const assignCalls = (): any[][] =>
  (global.fetch as jest.Mock).mock.calls.filter(([u]) => String(u).includes('assign-driver'));

describe('TripPoller edge branches', () => {
  let logSpy: jest.SpyInstance;
  let warnSpy: jest.SpyInstance;

  beforeEach(async () => {
    jest.useFakeTimers();
    TripPoller.reset();
    global.fetch = jest.fn();
    logSpy = jest.spyOn(console, 'log').mockImplementation(() => {});
    warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => {});
    await useAuthStore.getState().setAuth('tok', {
      id: 'u_1',
      name: 'Raj',
      role: 'driver',
      email: 'r@x.com',
      driverId: 'drv_1',
    });
  });

  afterEach(() => {
    TripPoller.stop();
    jest.useRealTimers();
    jest.restoreAllMocks();
    global.fetch = globalFetch;
  });

  test('start while already running does not spawn a second interval', async () => {
    (global.fetch as jest.Mock).mockResolvedValue(okResponse({ trips: [] }));

    TripPoller.start(undefined, 30000);
    TripPoller.start(undefined, 30000); // guarded

    await jest.advanceTimersByTimeAsync(30000);
    expect(global.fetch).toHaveBeenCalledTimes(1);

    const startedLogs = logSpy.mock.calls.filter(([m]) =>
      String(m).includes('Auto-accept polling started')
    );
    expect(startedLogs).toHaveLength(1);
  });

  test('HTTP failure on the pending-trips GET warns and keeps the loop alive', async () => {
    (global.fetch as jest.Mock).mockResolvedValue({ ok: false, status: 503 });

    TripPoller.start(undefined, 1000);
    await jest.advanceTimersByTimeAsync(1000);

    expect(warnSpy).toHaveBeenCalledWith('[TRIP POLLER] Poll failed: HTTP 503');
    expect(assignCalls()).toHaveLength(0);
    expect(TripPoller.getStatus().lastPollAt).not.toBeNull();

    // Loop survives into the next tick.
    (global.fetch as jest.Mock).mockResolvedValue(okResponse({ trips: [] }));
    await jest.advanceTimersByTimeAsync(1000);
    expect(TripPoller.getStatus().running).toBe(true);
  });

  test('extracts trips from {data: []} envelope shape', async () => {
    (global.fetch as jest.Mock).mockImplementation(async (url: string | URL) => {
      if (String(url).includes('assign-driver')) return { ok: true, status: 200 };
      return okResponse({ data: [{ id: 'env_trip' }] });
    });

    TripPoller.start(undefined, 30000);
    await jest.advanceTimersByTimeAsync(30000);

    expect(assignCalls()[0][0]).toContain('/api/v1/trips/env_trip/assign-driver');
  });

  test('garbage list entries are filtered out — no assigns fire', async () => {
    (global.fetch as jest.Mock).mockResolvedValue(
      okResponse({ trips: [null, {}, { id: 7 }, 'junk', { id: 'ok' }] })
    );

    TripPoller.start(undefined, 30000);
    await jest.advanceTimersByTimeAsync(30000);

    // Only the single well-formed entry survives the filter.
    expect(assignCalls()).toHaveLength(1);
    expect(String(assignCalls()[0][0])).toContain('/api/v1/trips/ok/assign-driver');
  });

  test('non-object non-array payload yields zero assigns without crashing', async () => {
    (global.fetch as jest.Mock).mockResolvedValue(okResponse('not-a-list'));

    TripPoller.start(undefined, 30000);
    await jest.advanceTimersByTimeAsync(30000);

    expect(assignCalls()).toHaveLength(0);
    expect(TripPoller.getStatus().acceptedCount).toBe(0);
  });

  test('envelope with neither trips nor data arrays assigns nothing', async () => {
    (global.fetch as jest.Mock).mockResolvedValue(okResponse({ trips: 'nope', data: 42 }));

    TripPoller.start(undefined, 30000);
    await jest.advanceTimersByTimeAsync(30000);

    expect(assignCalls()).toHaveLength(0);
  });

  test('assign exhaustion after maxRetries warns and never emits accept', async () => {
    (global.fetch as jest.Mock).mockImplementation(async (url: string | URL) => {
      if (String(url).includes('assign-driver')) throw new Error('gateway gone');
      return okResponse({ trips: [{ id: 'doomed' }] });
    });
    const listener = jest.fn();

    TripPoller.onTripAccepted(listener);
    TripPoller.start(undefined, 30000, { assignMaxRetries: 0, assignBaseDelayMs: 10 });
    await jest.advanceTimersByTimeAsync(30000);

    // maxRetries 0 → exactly one attempt, immediate throw.
    expect(assignCalls()).toHaveLength(1);
    expect(listener).not.toHaveBeenCalled();
    expect(TripPoller.getStatus().acceptedCount).toBe(0);
    expect(warnSpy).toHaveBeenCalledWith(
      '[TRIP POLLER] Auto-accept failed for doomed:',
      'gateway gone'
    );
  });

  test('a throwing listener never breaks the loop or other listeners', async () => {
    (global.fetch as jest.Mock).mockImplementation(async (url: string | URL) => {
      if (String(url).includes('assign-driver')) return { ok: true, status: 200 };
      return okResponse({ trips: [{ id: 'trip_x' }] });
    });

    const bad = jest.fn(() => {
      throw new Error('listener exploded');
    });
    const good = jest.fn();
    TripPoller.onTripAccepted(bad);
    TripPoller.onTripAccepted(good);

    TripPoller.start(undefined, 30000);
    await jest.advanceTimersByTimeAsync(30000);

    expect(bad).toHaveBeenCalledTimes(1);
    expect(good).toHaveBeenCalledWith({ tripId: 'trip_x' });
    expect(TripPoller.getStatus().running).toBe(true);
  });

  describe('UUID v4 fallback (no crypto.randomUUID)', () => {
    let savedCrypto: PropertyDescriptor | undefined;

    beforeEach(() => {
      savedCrypto = Object.getOwnPropertyDescriptor(globalThis, 'crypto');
      Object.defineProperty(globalThis, 'crypto', {
        configurable: true,
        value: { ...(globalThis.crypto as unknown as object), randomUUID: undefined },
      });
    });

    afterEach(() => {
      if (savedCrypto) Object.defineProperty(globalThis, 'crypto', savedCrypto);
    });

    test('manual RFC 4122 impl produces a valid v4 key when randomUUID is absent', async () => {
      (global.fetch as jest.Mock).mockImplementation(async (url: string | URL) => {
        if (String(url).includes('assign-driver')) return { ok: true, status: 200 };
        return okResponse({ trips: [{ id: 'fallback_trip' }] });
      });

      TripPoller.start(undefined, 30000);
      await jest.advanceTimersByTimeAsync(60000); // two polls → two POSTs

      const calls = assignCalls();
      expect(calls.length).toBeGreaterThanOrEqual(2);
      const key1 = calls[0][1].headers['Idempotency-Key'];
      const key2 = calls[1][1].headers['Idempotency-Key'];
      for (const key of [key1, key2]) {
        expect(key).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
      }
      expect(key1).toBe(key2); // still stable per trip
    });
  });
});
