// Integration layer: autonomous poller loop — poll → auto-assign → retry → auth.
// Complements unit tests by driving the full cycle over fake timers with a
// scripted fetch transport, verifying Idempotency-Key stability across polls
// and that an empty pending list stops assignment traffic.
import { TripPoller } from '../../src/services/tripPoller';
import { getApiBaseURL } from '../../src/constants/network';
import { useAuthStore } from '../../src/stores/authStore';

const globalFetch = global.fetch;

interface CapturedCall {
  url: string;
  init?: RequestInit;
}

describe('trip loop integration (poller + retry + auth)', () => {
  beforeEach(async () => {
    jest.useFakeTimers();
    TripPoller.reset();
    await useAuthStore.getState().setAuth('mock_token_123', {
      id: 'u_1',
      name: 'Rajesh Kumar',
      role: 'driver',
      email: 'driver@avandab.com',
      driverId: 'drv_1',
    });
  });

  afterEach(() => {
    TripPoller.stop();
    jest.useRealTimers();
    global.fetch = globalFetch;
  });

  test('poll → assign fires listener, reuses Idempotency-Key across polls, stops on empty list', async () => {
    const listener = jest.fn();
    const unsubscribe = TripPoller.onTripAccepted(listener);

    const calls: CapturedCall[] = [];
    let listCalls = 0;
    global.fetch = jest.fn(async (input: any, init?: RequestInit) => {
      const url = String(input);
      calls.push({ url, init });
      if (url.includes('/assign-driver')) {
        return { ok: true, status: 200 };
      }
      listCalls += 1;
      if (listCalls <= 2) {
        return { ok: true, status: 200, json: async () => ({ trips: [{ id: 't1' }] }) };
      }
      return { ok: true, status: 200, json: async () => ({ trips: [] }) };
    }) as any;

    TripPoller.start(undefined, 100, { assignMaxRetries: 2, assignBaseDelayMs: 20 });

    // Tick 1: GET pending returns t1 → assign POST 200 → listener fires.
    await jest.advanceTimersByTimeAsync(100);
    expect(listener).toHaveBeenCalledTimes(1);
    expect(listener).toHaveBeenCalledWith({ tripId: 't1' });
    expect(TripPoller.getStatus().acceptedCount).toBe(1);

    // Tick 2: same trip still listed → re-POST with SAME Idempotency-Key,
    // but no duplicate accept event.
    await jest.advanceTimersByTimeAsync(100);
    expect(listener).toHaveBeenCalledTimes(1);

    let assigns = calls.filter((c) => c.url.includes('/assign-driver'));
    expect(assigns).toHaveLength(2);
    expect(assigns[0].url).toBe(
      `${getApiBaseURL()}/api/v1/trips/${encodeURIComponent('t1')}/assign-driver`
    );

    const keys = assigns.map((a) => (a.init!.headers as Record<string, string>)['Idempotency-Key']);
    expect(keys[0]).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i);
    expect(keys[0]).toBe(keys[1]);

    for (const a of assigns) {
      const headers = a.init!.headers as Record<string, string>;
      expect(a.init!.method).toBe('POST');
      expect(headers.Authorization).toBe('Bearer mock_token_123');
      expect(headers['Content-Type']).toBe('application/json');
    }

    // Tick 3: pending list is empty → NO further assign POSTs.
    await jest.advanceTimersByTimeAsync(100);
    assigns = calls.filter((c) => c.url.includes('/assign-driver'));
    expect(assigns).toHaveLength(2);

    const polls = calls.filter((c) => !c.url.includes('/assign-driver'));
    expect(polls).toHaveLength(3);
    expect(polls[0].url).toContain('/api/v1/trips?driver_id=me&status=pending');
    const pollHeaders = polls[0].init!.headers as Record<string, string>;
    expect(pollHeaders.Accept).toBe('application/json');
    expect(pollHeaders.Authorization).toBe('Bearer mock_token_123');

    unsubscribe();
    TripPoller.stop();
    expect(TripPoller.getStatus().running).toBe(false);

    // Post-stop silence: nothing at all fires.
    const callsAfterStop = calls.length;
    await jest.advanceTimersByTimeAsync(500);
    expect(calls.length).toBe(callsAfterStop);
  });

  test('transient assign failure retries inside the loop and still accepts on success', async () => {
    const listener = jest.fn();
    TripPoller.onTripAccepted(listener);

    let assignAttempts = 0;
    global.fetch = jest.fn(async (input: any) => {
      const url = String(input);
      if (url.includes('/assign-driver')) {
        assignAttempts += 1;
        if (assignAttempts === 1) throw new Error('flaky socket');
        return { ok: true, status: 200 };
      }
      return { ok: true, status: 200, json: async () => ({ trips: [{ id: 't1' }] }) };
    }) as any;

    const warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => {});
    TripPoller.start(undefined, 100, { assignMaxRetries: 2, assignBaseDelayMs: 20 });

    await jest.advanceTimersByTimeAsync(100); // attempt 1 fails
    expect(assignAttempts).toBe(1);
    expect(listener).not.toHaveBeenCalled();

    await jest.advanceTimersByTimeAsync(50); // backoff (~20ms ± jitter) → attempt 2 succeeds
    expect(assignAttempts).toBe(2);
    expect(listener).toHaveBeenCalledWith({ tripId: 't1' });

    warnSpy.mockRestore();
    TripPoller.stop();
  });
});
