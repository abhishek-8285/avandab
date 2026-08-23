import { canGenerate, generateEwayBill, getEwayBill } from '../src/services/ewaybill';
import { useAuthStore } from '../src/stores/authStore';

const globalFetch = global.fetch;

describe('canGenerate (GST ₹50,000 threshold)', () => {
  test('49999 is below threshold', () => {
    expect(canGenerate(49999)).toBe(false);
  });

  test('50000 meets threshold', () => {
    expect(canGenerate(50000)).toBe(true);
  });
});

describe('getEwayBill', () => {
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

  test('returns null on 404 (no EWB yet)', async () => {
    const fetchMock = jest.fn().mockResolvedValue({
      ok: false,
      status: 404,
      json: async () => ({}),
    });
    global.fetch = fetchMock as any;

    const res = await getEwayBill('trip_1');
    expect(res).toBeNull();
    expect(fetchMock.mock.calls[0][0]).toContain('/api/v1/trips/trip_1/ewaybill');
    expect(fetchMock.mock.calls[0][1].headers.Authorization).toBe('Bearer tok');
  });

  test('throws on other non-ok statuses', async () => {
    global.fetch = jest.fn().mockResolvedValue({
      ok: false,
      status: 503,
      json: async () => ({}),
    }) as any;

    await expect(getEwayBill('trip_1')).rejects.toThrow('503');
  });
});

describe('generateEwayBill', () => {
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

  test('posts to correct URL with empty JSON body and auth header', async () => {
    const fetchMock = jest.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        ewayBillNumber: 'EB123',
        validUntil: '2026-08-25',
        qrData: 'qr-payload',
        totalValue: 75000,
      }),
    });
    global.fetch = fetchMock as any;

    const res = await generateEwayBill('trip_2', { totalValue: 75000, force: true });

    expect(fetchMock.mock.calls[0][0]).toContain('/api/v1/trips/trip_2/ewaybill/generate');
    expect(fetchMock.mock.calls[0][1].method).toBe('POST');
    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({});
    expect(fetchMock.mock.calls[0][1].headers.Authorization).toBe('Bearer tok');
    expect(res.ewayBillNumber).toBe('EB123');
  });

  test('below threshold throws EWB_NOT_REQUIRED before hitting network', async () => {
    const fetchMock = jest.fn();
    global.fetch = fetchMock as any;

    await expect(generateEwayBill('trip_2', { totalValue: 49999 })).rejects.toThrow('EWB_NOT_REQUIRED');
    expect(fetchMock).not.toHaveBeenCalled();
  });

  test('includes ship_to_gstin passthrough when provided (June 15 2026 rule)', async () => {
    const fetchMock = jest.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        ewayBillNumber: 'EB9',
        validUntil: '2026-08-25',
        qrData: 'qr',
        totalValue: 60000,
        shipToGstin: '27ABCDE1234F1Z5',
      }),
    });
    global.fetch = fetchMock as any;

    const res = await generateEwayBill('trip_3', { totalValue: 60000, shipToGstin: '27ABCDE1234F1Z5' });

    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({ ship_to_gstin: '27ABCDE1234F1Z5' });
    expect(res.shipToGstin).toBe('27ABCDE1234F1Z5');
  });
});
