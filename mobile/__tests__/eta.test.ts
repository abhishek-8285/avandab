import { predictEta, trafficMultiplier } from '../src/services/eta';

// Local-time constructors so month/hour boundaries hold regardless of TZ
const at = (month: number, hour: number) => new Date(2026, month, 15, hour, 0, 0);

describe('trafficMultiplier', () => {
  test('monsoon alone (June–September)', () => {
    expect(trafficMultiplier(at(5, 13))).toBeCloseTo(1.3); // June noon
    expect(trafficMultiplier(at(8, 13))).toBeCloseTo(1.3); // September
  });

  test('rush hour alone (08:00–10:59, 17:00–19:59)', () => {
    expect(trafficMultiplier(at(2, 9))).toBeCloseTo(1.25);
    expect(trafficMultiplier(at(2, 18))).toBeCloseTo(1.25);
  });

  test('off-peak night alone (22:00–04:59)', () => {
    expect(trafficMultiplier(at(2, 23))).toBeCloseTo(0.9);
    expect(trafficMultiplier(at(2, 2))).toBeCloseTo(0.9);
  });

  test('otherwise ×1.0', () => {
    expect(trafficMultiplier(at(2, 13))).toBeCloseTo(1.0);
  });

  test('compound monsoon + rush = 1.3 * 1.25', () => {
    expect(trafficMultiplier(at(5, 8))).toBeCloseTo(1.3 * 1.25);
  });

  test('month boundary: May (4) no monsoon, June (5) monsoon', () => {
    expect(trafficMultiplier(new Date(2026, 4, 31, 12))).toBeCloseTo(1.0);
    expect(trafficMultiplier(new Date(2026, 5, 1, 12))).toBeCloseTo(1.3);
  });

  test('hour boundaries: 7 vs 8, 10 vs 11, 16 vs 17, 19 vs 20, 21 vs 22, 4 vs 5', () => {
    expect(trafficMultiplier(at(2, 7))).toBeCloseTo(1.0);
    expect(trafficMultiplier(at(2, 8))).toBeCloseTo(1.25);
    expect(trafficMultiplier(at(2, 10))).toBeCloseTo(1.25);
    expect(trafficMultiplier(at(2, 11))).toBeCloseTo(1.0);
    expect(trafficMultiplier(at(2, 16))).toBeCloseTo(1.0);
    expect(trafficMultiplier(at(2, 17))).toBeCloseTo(1.25);
    expect(trafficMultiplier(at(2, 19))).toBeCloseTo(1.25);
    expect(trafficMultiplier(at(2, 20))).toBeCloseTo(1.0);
    expect(trafficMultiplier(at(2, 21))).toBeCloseTo(1.0);
    expect(trafficMultiplier(at(2, 22))).toBeCloseTo(0.9);
    expect(trafficMultiplier(at(2, 4))).toBeCloseTo(0.9);
    expect(trafficMultiplier(at(2, 5))).toBeCloseTo(1.0);
  });
});

describe('predictEta', () => {
  test('applies monsoon multiplier and rounds up (ceil)', () => {
    // June noon ×1.3 → 33 * 1.3 = 42.900000000000006 → 43
    expect(predictEta(33, at(5, 13))).toBe(43);
  });

  test('applies rush multiplier with ceil on exact fractional product', () => {
    // March morning ×1.25 → 41 * 1.25 = 51.25 → 52
    expect(predictEta(41, at(2, 9))).toBe(52);
  });

  test('off-peak discount rounds up too', () => {
    // March night ×0.9 → 51 * 0.9 = 45.9 → 46
    expect(predictEta(51, at(2, 23))).toBe(46);
  });

  test('whole-minute result stays exact', () => {
    // March noon ×1.0 → 42 → 42
    expect(predictEta(42, at(2, 13))).toBe(42);
  });

  test('compound monsoon + rush: 100 * 1.625 = 162.5 → 163', () => {
    expect(predictEta(100, at(6, 18))).toBe(163); // July evening rush
  });

  test('arrivalHintMinutes acts as conservative floor', () => {
    // Prediction 130 but hint says 200 — never undercut the known estimate
    expect(predictEta(100, at(5, 13), 200)).toBe(200);
    // Hint below prediction is ignored
    expect(predictEta(100, at(5, 13), 50)).toBe(130);
  });

  test('negative baseMinutes throws', () => {
    expect(() => predictEta(-1, at(2, 13))).toThrow();
  });
});
