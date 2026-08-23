import { RetryableOperation, retry } from '../src/utils/retry';

describe('RetryableOperation', () => {
  beforeEach(() => {
    jest.useFakeTimers();
  });

  afterEach(() => {
    jest.useRealTimers();
  });

  test('success on first try does not schedule any delay', async () => {
    let calls = 0;
    const result = await new RetryableOperation({ baseDelayMs: 1000 }).execute(async () => {
      calls += 1;
      return 'ok';
    });
    expect(result).toBe('ok');
    expect(calls).toBe(1);
    expect(jest.getTimerCount()).toBe(0);
  });

  test('retries then succeeds', async () => {
    let calls = 0;
    const op = new RetryableOperation({ baseDelayMs: 100, maxRetries: 5 });
    const p = op.execute(async () => {
      calls += 1;
      if (calls < 3) throw new Error(`flaky ${calls}`);
      return 'done';
    });
    await jest.advanceTimersByTimeAsync(60_000);
    await expect(p).resolves.toBe('done');
    expect(calls).toBe(3);
  });

  test('exhausts retries and throws the last error', async () => {
    let calls = 0;
    const op = new RetryableOperation({ baseDelayMs: 10, maxRetries: 2 });
    const p = op.execute(async () => {
      calls += 1;
      throw new Error(`boom ${calls}`);
    });
    const captured = p.then(
      () => 'resolved',
      (e: Error) => e.message,
    );
    await jest.runAllTimersAsync();
    expect(await captured).toBe('boom 3'); // initial + 2 retries, last error wins
    expect(calls).toBe(3);
  });

  test('delays grow exponentially and cap at maxDelayMs', async () => {
    const setTimeoutSpy = jest.spyOn(global, 'setTimeout');
    const op = new RetryableOperation({
      baseDelayMs: 1000,
      maxDelayMs: 5000,
      multiplier: 2,
      maxRetries: 5,
      jitterFactor: 0, // deterministic
    });
    const p = op.execute(async () => {
      throw new Error('always fails');
    });
    p.catch(() => {});
    await jest.runAllTimersAsync();
    await expect(p).rejects.toThrow('always fails');

    const delays = setTimeoutSpy.mock.calls.map((c) => c[1]);
    expect(delays).toEqual([1000, 2000, 4000, 5000, 5000]); // capped at maxDelayMs
    setTimeoutSpy.mockRestore();
  });

  test('jitter stays within ±20% bounds', () => {
    const op = new RetryableOperation({ baseDelayMs: 1000, maxDelayMs: 10_000, multiplier: 2 });
    for (let i = 0; i < 200; i++) {
      const d0 = op.computeDelayMs(0);
      expect(d0).toBeGreaterThanOrEqual(800 - Number.EPSILON);
      expect(d0).toBeLessThanOrEqual(1200 + Number.EPSILON);
    }
    // Capped attempt still jitters around the cap
    for (let i = 0; i < 200; i++) {
      const dCap = op.computeDelayMs(20); // raw would be huge → capped at 10000
      expect(dCap).toBeGreaterThanOrEqual(8000 - Number.EPSILON);
      expect(dCap).toBeLessThanOrEqual(12000 + Number.EPSILON);
    }
  });

  test('never runs op when signal is already aborted', async () => {
    const controller = new AbortController();
    controller.abort();
    let calls = 0;
    await expect(
      new RetryableOperation().execute(async () => {
        calls += 1;
        return 'nope';
      }, controller.signal),
    ).rejects.toThrow(/abort/i);
    expect(calls).toBe(0);
    expect(jest.getTimerCount()).toBe(0);
  });

  test('stops retrying once aborted mid-flight', async () => {
    const controller = new AbortController();
    let calls = 0;
    const op = new RetryableOperation({ baseDelayMs: 5000, maxRetries: 5 });
    const p = op.execute(async () => {
      calls += 1;
      throw new Error(`err ${calls}`);
    }, controller.signal);
    const captured = p.then(
      () => 'resolved',
      (e: Error) => e.message,
    );
    await jest.advanceTimersByTimeAsync(0); // first attempt fails, sleep scheduled
    controller.abort();
    await jest.runAllTimersAsync();
    expect(await captured).toBe('err 1'); // original error re-thrown, no retry
    expect(calls).toBe(1);
  });
});

describe('retry()', () => {
  beforeEach(() => {
    jest.useFakeTimers();
  });

  afterEach(() => {
    jest.useRealTimers();
  });

  test('resolves value and honors options', async () => {
    let calls = 0;
    const p = retry(
      async () => {
        calls += 1;
        if (calls < 2) throw new Error('once');
        return 42;
      },
      { baseDelayMs: 50 },
    );
    await jest.advanceTimersByTimeAsync(5000);
    await expect(p).resolves.toBe(42);
    expect(calls).toBe(2);
  });

  test('rejects after exhausting maxRetries', async () => {
    const p = retry(
      async () => {
        throw new Error('dead');
      },
      { maxRetries: 1, baseDelayMs: 20 },
    );
    const captured = p.then(
      () => 'resolved',
      (e: Error) => e.message,
    );
    await jest.runAllTimersAsync();
    expect(await captured).toBe('dead');
  });
});
