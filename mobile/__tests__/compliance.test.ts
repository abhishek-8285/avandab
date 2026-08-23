import { evaluateCompliance, fetchCompliance, REQUIRED_DOCS, VehicleDocument } from '../src/services/compliance';
import { useAuthStore } from '../src/stores/authStore';

const globalFetch = global.fetch;

const NOW = new Date('2026-08-23T10:00:00Z'); // fixed "today" for deterministic tests

// Full valid set — every doc expires far in the future
function allValid(): VehicleDocument[] {
  return REQUIRED_DOCS.map((docType) => ({ docType, expiryDate: '2030-01-01' }));
}

describe('evaluateCompliance (pure)', () => {
  test('expired doc → red and trip blocked', () => {
    const docs = allValid();
    docs[docs.findIndex((d) => d.docType === 'insurance')].expiryDate = '2026-08-22'; // yesterday
    const res = evaluateCompliance(docs, NOW);
    expect(res.score).toBe('red');
    expect(res.canStartTrip).toBe(false);
    expect(res.expired).toHaveLength(1);
    expect(res.expired[0].docType).toBe('insurance');
  });

  test('doc expiring exactly in 3 days → amber and blocked', () => {
    const docs = allValid();
    docs[docs.findIndex((d) => d.docType === 'puc')].expiryDate = '2026-08-26';
    const res = evaluateCompliance(docs, NOW);
    expect(res.score).toBe('amber');
    expect(res.canStartTrip).toBe(false);
    expect(res.expiringSoon.map((d) => d.docType)).toEqual(['puc']);
  });

  test('expiry in 7 days boundary → amber and blocked', () => {
    const docs = allValid();
    docs[docs.findIndex((d) => d.docType === 'rc')].expiryDate = '2026-08-30';
    const res = evaluateCompliance(docs, NOW);
    expect(res.score).toBe('amber');
    expect(res.canStartTrip).toBe(false);
  });

  test('expiry in 8 days → green', () => {
    const docs = allValid();
    docs[docs.findIndex((d) => d.docType === 'rc')].expiryDate = '2026-08-31';
    const res = evaluateCompliance(docs, NOW);
    expect(res.score).toBe('green');
    expect(res.canStartTrip).toBe(true);
    expect(res.missing).toEqual([]);
  });

  test('missing docs → amber but trip allowed', () => {
    const docs = allValid().filter((d) => d.docType !== 'permit' && d.docType !== 'road_tax');
    const res = evaluateCompliance(docs, NOW);
    expect(res.score).toBe('amber');
    expect(res.canStartTrip).toBe(true); // soft warning
    expect(res.missing).toEqual(['permit', 'road_tax']);
    expect(res.expired).toEqual([]);
  });

  test('empty list → all missing → amber, allowed', () => {
    const res = evaluateCompliance([], NOW);
    expect(res.missing).toEqual([...REQUIRED_DOCS]);
    expect(res.score).toBe('amber');
    expect(res.canStartTrip).toBe(true);
  });
});

describe('fetchCompliance', () => {
  beforeEach(async () => {
    await useAuthStore.getState().setAuth('tok', {
      id: 'u_1',
      name: 'Raj',
      role: 'driver',
      email: 'r@x.com',
    });
  });

  afterEach(() => {
    global.fetch = globalFetch;
  });

  test('maps snake_case response and passes auth header', async () => {
    const fetchMock = jest.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        documents: [
          { doc_type: 'rc', expiry_date: '2030-01-01' },
          { doc_type: 'insurance', expiry_date: null },
          { doc_type: 'unknown_kind', expiry_date: '2099-01-01' }, // ignored defensively
        ],
      }),
    });
    global.fetch = fetchMock as any;

    const res = await fetchCompliance('veh_9');

    expect(fetchMock.mock.calls[0][0]).toContain('/api/v1/documents/vehicle/veh_9');
    expect(fetchMock.mock.calls[0][1].headers.Authorization).toBe('Bearer tok');

    expect(res.missing).toEqual(
      REQUIRED_DOCS.filter((t) => t !== 'rc' && t !== 'insurance')
    );
    expect(res.score).toBe('amber');
    expect(res.canStartTrip).toBe(true); // only missing, nothing expired/expiring
  });

  test('non-ok response throws', async () => {
    global.fetch = jest.fn().mockResolvedValue({
      ok: false,
      status: 500,
      json: async () => ({}),
    }) as any;

    await expect(fetchCompliance('veh_9')).rejects.toThrow('500');
  });

  test('fetch errors propagate', async () => {
    global.fetch = jest.fn().mockRejectedValue(new Error('network down')) as any;
    await expect(fetchCompliance('veh_9')).rejects.toThrow('network down');
  });
});
