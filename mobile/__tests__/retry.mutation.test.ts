// Mutation-killing assertions for src/utils/retry.ts survivors from Stryker
// run #1:
//   L29 LogicalOperator    — `??` default for maxDelayMs (0 must stay 0)
//   L42 ConditionalExpr ×2 — jitter bypass must not touch Math.random
//   L44 ArithmeticOperator ×2 — jitter spread arithmetic
import { RetryableOperation } from '../src/utils/retry';

describe('RetryableOperation mutation killers', () => {
  afterEach(() => {
    jest.restoreAllMocks();
  });

  test('maxDelayMs of 0 is honored literally — ?? must not become ||', () => {
    const op = new RetryableOperation({
      baseDelayMs: 100,
      maxDelayMs: 0,
      multiplier: 1,
      jitterFactor: 0,
    });
    // raw = min(100 × 1^5, 0) = 0 — a falsy cap is still a cap.
    expect(op.computeDelayMs(5)).toBe(0);
  });

  test('jitterFactor <= 0 returns the exact raw delay without consulting Math.random', () => {
    const randomSpy = jest.spyOn(Math, 'random');
    const zero = new RetryableOperation({
      baseDelayMs: 1000,
      multiplier: 2,
      jitterFactor: 0,
      maxDelayMs: 100000,
    });
    expect(zero.computeDelayMs(3)).toBe(8000);
    expect(randomSpy).not.toHaveBeenCalled();

    const negative = new RetryableOperation({
      baseDelayMs: 500,
      multiplier: 1,
      jitterFactor: -1,
      maxDelayMs: 100000,
    });
    expect(negative.computeDelayMs(2)).toBe(500);
    expect(randomSpy).not.toHaveBeenCalled();
  });

  test('jitter arithmetic at the extremes: random=1 lands exactly on +spread', () => {
    jest.spyOn(Math, 'random').mockReturnValue(1);
    const op = new RetryableOperation({
      baseDelayMs: 1000,
      maxDelayMs: 60000,
      multiplier: 1,
      jitterFactor: 0.2, // spread = 200 → delay = 1000 − 200 + (1 × 200 × 2) = 1200
    });
    expect(op.computeDelayMs(0)).toBe(1200);
  });

  test('jitter arithmetic at the floor: random=0 lands exactly on −spread', () => {
    jest.spyOn(Math, 'random').mockReturnValue(0);
    const op = new RetryableOperation({
      baseDelayMs: 1000,
      maxDelayMs: 60000,
      multiplier: 1,
      jitterFactor: 0.2,
    });
    expect(op.computeDelayMs(0)).toBe(800);
  });
});
