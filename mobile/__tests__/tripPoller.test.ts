import { TripPoller } from '../src/services/tripPoller';
import { useAuthStore } from '../src/stores/authStore';

const globalFetch = global.fetch;

function okResponse(body: unknown) {
  return { ok: true, status: 200, json: async () => body };
}

const assignCalls = (): any[][] =>
  (global.fetch as jest.Mock).mock.calls.filter(([u]) => String(u).includes('assign-driver'));

describe('TripPoller', () => {
  beforeEach(async () => {
    jest.useFakeTimers();
    TripPoller.reset();
    global.fetch = jest.fn();
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
    global.fetch = globalFetch;
  });

  test('polls pending trips endpoint with bearer token', async () => {
    (global.fetch as jest.Mock).mockResolvedValue(okResponse({ trips: [] }));

    TripPoller.start(undefined, 30000);
    expect(TripPoller.getStatus().running).toBe(true);

    await jest.advanceTimersByTimeAsync(30000);

    expect(global.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/trips?driver_id=me&status=pending'),
      expect.objectContaining({
        headers: expect.objectContaining({ Authorization: 'Bearer tok' }),
      }),
    );
    expect(TripPoller.getStatus().lastPollAt).not.toBeNull();
  });

  test('auto-assign POSTs right endpoint and reuses Idempotency-Key across polls', async () => {
    (global.fetch as jest.Mock).mockImplementation(async (url: string | URL | Request) => {
      const u = String(url);
      if (u.includes('assign-driver')) return { ok: true, status: 200 };
      return okResponse({ trips: [{ id: 'trip_1' }] });
    });

    TripPoller.start(undefined, 30000);
    await jest.advanceTimersByTimeAsync(30000);
    await jest.advanceTimersByTimeAsync(30000); // second poll, same trip still pending

    const calls = assignCalls();
    expect(calls).toHaveLength(2);
    expect(String(calls[0][0])).toContain('/api/v1/trips/trip_1/assign-driver');
    expect(calls[0][1].method).toBe('POST');
    expect(calls[0][1].headers.Authorization).toBe('Bearer tok');

    const key1: string = calls[0][1].headers['Idempotency-Key'];
    const key2: string = calls[1][1].headers['Idempotency-Key'];
    // Stable per trip + valid UUID v4 shape (works with or without crypto.randomUUID)
    expect(key1).toBe(key2);
    expect(key1).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i);
  });

  test('409 is treated as accepted without retrying', async () => {
    (global.fetch as jest.Mock).mockImplementation(async (url: string | URL | Request) => {
      const u = String(url);
      if (u.includes('assign-driver')) return { ok: false, status: 409 };
      return okResponse({ trips: [{ id: 'trip_409' }] });
    });
    const listener = jest.fn();
    const unsub = TripPoller.onTripAccepted(listener);

    TripPoller.start(undefined, 30000);
    await jest.advanceTimersByTimeAsync(30000); // tick 1

    // 409 handled inline — no retry backoff within the cycle
    expect(assignCalls()).toHaveLength(1);
    expect(listener).toHaveBeenCalledTimes(1);
    expect(listener).toHaveBeenCalledWith({ tripId: 'trip_409' });
    expect(TripPoller.getStatus().acceptedCount).toBe(1);

    await jest.advanceTimersByTimeAsync(30000); // tick 2: server still lists it pending
    // Re-POSTed idempotently (same key), but NOT counted/emitted again
    const calls = assignCalls();
    expect(calls).toHaveLength(2);
    expect(calls[0][1].headers['Idempotency-Key']).toBe(calls[1][1].headers['Idempotency-Key']);
    expect(listener).toHaveBeenCalledTimes(1);
    expect(TripPoller.getStatus().acceptedCount).toBe(1);
    unsub();
  });

  test('network error on assign retries via backoff using injected options', async () => {
    jest.spyOn(Math, 'random').mockReturnValue(0.5); // neutralize jitter → exact delays
    let attempts = 0;
    (global.fetch as jest.Mock).mockImplementation(async (url: string | URL | Request) => {
      const u = String(url);
      if (u.includes('assign-driver')) {
        attempts += 1;
        throw new Error('network down');
      }
      return okResponse({ trips: [{ id: 'trip_err' }] });
    });

    TripPoller.start(undefined, 30000, { assignMaxRetries: 2, assignBaseDelayMs: 1000 });
    await jest.advanceTimersByTimeAsync(30000); // tick 1: list + assign attempt 1
    expect(attempts).toBe(1);

    await jest.advanceTimersByTimeAsync(1000); // delay 1: 1000ms
    expect(attempts).toBe(2);

    await jest.advanceTimersByTimeAsync(2000); // delay 2: 2000ms — exhausted (2 retries)
    expect(attempts).toBe(3);

    await jest.advanceTimersByTimeAsync(26500); // total elapsed < next tick — no new cycle
    expect(attempts).toBe(3);
    expect(TripPoller.getStatus().acceptedCount).toBe(0);
  });

  test('listener notified once per accepted trip; unsubscribe works', async () => {
    let listCalls = 0;
    (global.fetch as jest.Mock).mockImplementation(async (url: string | URL | Request) => {
      const u = String(url);
      if (u.includes('assign-driver')) return { ok: true, status: 200 };
      listCalls += 1;
      return okResponse({
        trips: listCalls === 1 ? [{ id: 'trip_a' }, { id: 'trip_b' }] : [{ id: 'trip_a' }, { id: 'trip_b' }, { id: 'trip_c' }],
      });
    });
    const l1 = jest.fn();
    const l2 = jest.fn();
    const unsub1 = TripPoller.onTripAccepted(l1);
    TripPoller.onTripAccepted(l2);

    TripPoller.start(undefined, 30000);
    await jest.advanceTimersByTimeAsync(30000); // poll 1: trips a + b accepted

    expect(l1).toHaveBeenCalledTimes(2);
    expect(l1).toHaveBeenNthCalledWith(1, { tripId: 'trip_a' });
    expect(l1).toHaveBeenNthCalledWith(2, { tripId: 'trip_b' });
    expect(l2).toHaveBeenCalledTimes(2);

    unsub1();
    await jest.advanceTimersByTimeAsync(30000); // poll 2: only NEW trip_c may emit

    expect(l1).toHaveBeenCalledTimes(2); // unsubscribed
    expect(l2).toHaveBeenCalledTimes(3);
    expect(l2).toHaveBeenLastCalledWith({ tripId: 'trip_c' });
    expect(TripPoller.getStatus().acceptedCount).toBe(3);
  });

  test('guards against concurrent poll cycles', async () => {
    let resolveList!: (v: unknown) => void;
    (global.fetch as jest.Mock).mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveList = resolve;
        }),
    );

    TripPoller.start(undefined, 30000);
    await jest.advanceTimersByTimeAsync(30000); // tick 1 starts a hanging poll
    expect(global.fetch).toHaveBeenCalledTimes(1);

    await jest.advanceTimersByTimeAsync(30000); // tick 2 must bail out (isPolling)
    expect(global.fetch).toHaveBeenCalledTimes(1);

    resolveList(okResponse({ trips: [] }));
    await jest.advanceTimersByTimeAsync(30000); // tick 3 runs normally again
    expect(global.fetch).toHaveBeenCalledTimes(2);
  });

  test('stop() halts polling', async () => {
    (global.fetch as jest.Mock).mockResolvedValue(okResponse({ trips: [] }));

    TripPoller.start(undefined, 30000);
    await jest.advanceTimersByTimeAsync(30000);
    expect(TripPoller.getStatus().running).toBe(true);

    TripPoller.stop();
    expect(TripPoller.getStatus().running).toBe(false);

    const callsBefore = (global.fetch as jest.Mock).mock.calls.length;
    await jest.advanceTimersByTimeAsync(90000);
    expect((global.fetch as jest.Mock).mock.calls.length).toBe(callsBefore);
  });

  test('fetch failures are swallowed and warned, loop keeps running', async () => {
    const warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => {});
    (global.fetch as jest.Mock).mockRejectedValue(new Error('offline'));

    TripPoller.start(undefined, 30000);
    await jest.advanceTimersByTimeAsync(30000);
    await jest.advanceTimersByTimeAsync(30000);

    expect(warnSpy).toHaveBeenCalled();
    expect(TripPoller.getStatus().running).toBe(true);
    expect(TripPoller.getStatus().lastPollAt).not.toBeNull();
  });
});
