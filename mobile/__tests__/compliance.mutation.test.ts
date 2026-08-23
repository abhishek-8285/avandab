// Mutation-killing assertions for src/services/compliance.ts survivors from
// Stryker run #1 (dayNumber regex anchoring, same-day expiry boundary).
import { evaluateCompliance, type VehicleDocument } from '../src/services/compliance';

const FUTURE = '2030-06-01';

function fullSet(overrides: Record<string, string> = {}): VehicleDocument[] {
  const base: Record<string, string> = {
    rc: FUTURE,
    fitness: FUTURE,
    insurance: FUTURE,
    puc: FUTURE,
    permit: FUTURE,
    road_tax: FUTURE,
  };
  for (const [k, v] of Object.entries(overrides)) base[k] = v;
  return Object.entries(base).map(([docType, expiryDate]) => ({ docType, expiryDate }) as VehicleDocument);
}

describe('Compliance mutation killers', () => {
  test('non-ISO garbage in expiryDate is treated as undated — never as expired', () => {
    // Regex must be anchored: "X2026-07-01" contains a date-like suffix but is
    // NOT a valid ISO prefix. now=2026-08-01 would make it expired if parsed.
    const result = evaluateCompliance(
      [{ docType: 'rc', expiryDate: 'X2026-07-01' }],
      new Date('2026-08-01T10:00:00Z')
    );
    expect(result.expired).toEqual([]);
    expect(result.score).not.toBe('red');
    // rc itself counts as present, so only the other five docs are missing.
    expect(result.missing).toEqual(['fitness', 'insurance', 'puc', 'permit', 'road_tax']);
  });

  test('document expiring exactly TODAY is expiringSoon, not expired', () => {
    const today = '2026-08-01';
    const result = evaluateCompliance(fullSet({ insurance: today }), new Date('2026-08-01T00:30:00Z'));

    expect(result.score).toBe('amber'); // amber, NOT red
    expect(result.canStartTrip).toBe(false);
    expect(result.expired).toEqual([]);
    expect(result.expiringSoon.map((d) => d.docType)).toEqual(['insurance']);
  });

  test('document that expired YESTERDAY is red and blocks the trip', () => {
    const yesterday = '2026-07-31';
    const result = evaluateCompliance(fullSet({ insurance: yesterday }), new Date('2026-08-01T00:30:00Z'));

    expect(result.score).toBe('red');
    expect(result.canStartTrip).toBe(false);
    expect(result.expired.map((d) => d.docType)).toEqual(['insurance']);
  });

  test('day arithmetic uses UTC day numbers — timezone-shifted now cannot misclassify', () => {
    // Late-UTC instant still lands on the same calendar day comparison.
    const result = evaluateCompliance(
      fullSet({ road_tax: '2026-08-08' }), // exactly 7 days ahead
      new Date('2026-08-01T23:59:59Z')
    );
    expect(result.expiringSoon.map((d) => d.docType)).toEqual(['road_tax']);
    expect(result.expired).toEqual([]);
    expect(result.score).toBe('amber');

    // One day past the 7-day window → clean green.
    const outside = evaluateCompliance(
      fullSet({ road_tax: '2026-08-09' }),
      new Date('2026-08-01T12:00:00Z')
    );
    expect(outside.score).toBe('green');
    expect(outside.canStartTrip).toBe(true);
  });
});
