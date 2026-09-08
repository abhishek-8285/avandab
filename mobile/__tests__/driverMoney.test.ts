import { getDriverBalance, getDriverSettlements, requestAdvance } from '../src/services/driverMoney';
import { useAuthStore } from '../src/stores/authStore';

const globalFetch = global.fetch;

describe('driverMoney', () => {
  beforeEach(() => {
    useAuthStore.setState({ token: 'tok', user: { id: 'u1' } as any });
  });

  afterEach(() => {
    global.fetch = globalFetch;
  });

  test('403/404 balance returns null (flag off or unlinked)', async () => {
    global.fetch = jest.fn().mockResolvedValue({ status: 403, ok: false }) as any;
    await expect(getDriverBalance()).resolves.toBeNull();
    global.fetch = jest.fn().mockResolvedValue({ status: 404, ok: false }) as any;
    await expect(getDriverBalance()).resolves.toBeNull();
  });

  test('server error throws; ok returns json', async () => {
    global.fetch = jest.fn().mockResolvedValue({ status: 500, ok: false }) as any;
    await expect(getDriverBalance()).rejects.toThrow('balance fetch failed (500)');
    global.fetch = jest.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({ balance: 100 }) }) as any;
    await expect(getDriverBalance()).resolves.toEqual({ balance: 100 });
  });

  test('settlements returns list; advance posts', async () => {
    global.fetch = jest.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({ settlements: [{ id: 1 }] }) }) as any;
    await expect(getDriverSettlements()).resolves.toEqual([{ id: 1 }]);
    (global.fetch as jest.Mock).mockResolvedValue({ ok: true, status: 201, json: async () => ({ id: 'a9', status: 'pending' }) });
    await expect(requestAdvance({ trip_id: 't1', amount: 500, reason: 'fuel' })).resolves.toEqual({ id: 'a9', status: 'pending' });
  });
});
