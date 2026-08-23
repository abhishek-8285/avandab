// Mutation-killing assertions for src/services/eta.ts survivors from Stryker
// run #1:
//   L9  ConditionalExpression — monsoon month-window condition
//   L32 EqualityOperator     — negative-base guard comparison
//   L33 StringLiteral        — thrown sentinel message
import { predictEta, trafficMultiplier } from '../src/services/eta';

describe('ETA mutation killers', () => {
  test('monsoon window is exactly June–September inclusive', () => {
    // Noon picks no rush/off-peak multipliers so the monsoon factor stands alone.
    expect(trafficMultiplier(new Date(2026, 4, 15, 12, 0, 0))).toBeCloseTo(1.0, 10); // May — outside
    expect(trafficMultiplier(new Date(2026, 5, 15, 12, 0, 0))).toBeCloseTo(1.3, 10); // June — first month
    expect(trafficMultiplier(new Date(2026, 8, 30, 12, 0, 0))).toBeCloseTo(1.3, 10); // Sept 30 — last day
    expect(trafficMultiplier(new Date(2026, 9, 1, 12, 0, 0))).toBeCloseTo(1.0, 10); // Oct 1 — outside
  });

  test('zero base minutes is valid input, not an error', () => {
    // Off-peak night departure: 0 × 0.9 → ceil → 0.
    expect(predictEta(0, new Date(2026, 0, 10, 23, 0, 0))).toBe(0);
  });

  test('negative or non-finite base throws the exact NEGATIVE_BASE_MINUTES sentinel', () => {
    expect(() => predictEta(-1, new Date(2026, 0, 10, 12, 0, 0))).toThrow(
      'NEGATIVE_BASE_MINUTES'
    );
    expect(() => predictEta(Number.NaN, new Date(2026, 0, 10, 12, 0, 0))).toThrow(
      'NEGATIVE_BASE_MINUTES'
    );
    expect(() => predictEta(Number.POSITIVE_INFINITY, new Date(2026, 0, 10, 12, 0, 0))).toThrow(
      'NEGATIVE_BASE_MINUTES'
    );
  });

  test('omitted arrival hint never raises the prediction', () => {
    // Rush-hour departure: 60 × 1.25 = 75.
    expect(predictEta(60, new Date(2026, 0, 12, 9, 0, 0))).toBe(75);
    // Hint floors the prediction upward.
    expect(predictEta(60, new Date(2026, 0, 12, 9, 0, 0), 90)).toBe(90);
    // Hint below prediction is ignored.
    expect(predictEta(60, new Date(2026, 0, 12, 9, 0, 0), 10)).toBe(75);
  });
});
