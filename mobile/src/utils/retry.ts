// Exponential-backoff retry helper for flaky mobile networks.
// Pure logic — no RN/native deps, fully unit-testable with jest fake timers.

export interface RetryOptions {
  maxRetries?: number; // retry attempts AFTER the first failure (default 5)
  baseDelayMs?: number; // delay before the first retry (default 5000)
  maxDelayMs?: number; // cap for any single delay (default 60000)
  multiplier?: number; // exponential growth factor (default 2)
  jitterFactor?: number; // ± fraction of randomization on each delay (default 0.2)
}

const DEFAULT_MAX_RETRIES = 5;
const DEFAULT_BASE_DELAY_MS = 5000;
const DEFAULT_MAX_DELAY_MS = 60000;
const DEFAULT_MULTIPLIER = 2;
const DEFAULT_JITTER_FACTOR = 0.2;

export class RetryableOperation {
  private readonly maxRetries: number;
  private readonly baseDelayMs: number;
  private readonly maxDelayMs: number;
  private readonly multiplier: number;
  private readonly jitterFactor: number;

  constructor(options: RetryOptions = {}) {
    this.maxRetries = options.maxRetries ?? DEFAULT_MAX_RETRIES;
    this.baseDelayMs = options.baseDelayMs ?? DEFAULT_BASE_DELAY_MS;
    this.maxDelayMs = options.maxDelayMs ?? DEFAULT_MAX_DELAY_MS;
    this.multiplier = options.multiplier ?? DEFAULT_MULTIPLIER;
    this.jitterFactor = options.jitterFactor ?? DEFAULT_JITTER_FACTOR;
  }

  /**
   * Delay before the given 0-indexed retry attempt:
   * baseDelayMs * multiplier^attempt capped at maxDelayMs, then jittered ±jitterFactor.
   */
  computeDelayMs(attempt: number): number {
    const raw = Math.min(
      this.baseDelayMs * Math.pow(this.multiplier, Math.max(0, attempt)),
      this.maxDelayMs,
    );
    if (this.jitterFactor <= 0) return raw;
    const spread = raw * this.jitterFactor;
    return raw - spread + Math.random() * spread * 2;
  }

  /**
   * Runs op until it resolves, retries are exhausted (throws the last error),
   * or the abort signal fires (no further attempts, no further delays).
   */
  async execute<T>(op: () => Promise<T>, signal?: AbortSignal): Promise<T> {
    let attempt = 0;
    let lastError: unknown;
    for (;;) {
      if (signal?.aborted) {
        throw lastError instanceof Error ? lastError : new Error('RetryableOperation aborted');
      }
      try {
        return await op();
      } catch (err) {
        lastError = err;
        if (signal?.aborted || attempt >= this.maxRetries) throw err;
        await this.sleep(this.computeDelayMs(attempt));
        attempt += 1;
      }
    }
  }

  private sleep(ms: number): Promise<void> {
    return new Promise((resolve) => setTimeout(resolve, ms));
  }
}

/** Convenience one-shot retry with the same backoff rules. */
export async function retry<T>(op: () => Promise<T>, options: RetryOptions = {}): Promise<T> {
  return new RetryableOperation(options).execute(op);
}
