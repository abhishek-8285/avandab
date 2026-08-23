import { exportMyData, eraseMyData, wipeLocalPii } from '../src/services/dataRights';
import * as SecureStore from 'expo-secure-store';
import { useAuthStore } from '../src/stores/authStore';

const globalFetch = global.fetch;

describe('data rights — exportMyData', () => {
  afterEach(() => {
    global.fetch = globalFetch;
  });

  test('GETs user data endpoint with bearer and returns JSON as-is', async () => {
    await useAuthStore.getState().setAuth('tok', {
      id: 'u_1',
      name: 'Raj',
      role: 'driver',
      email: 'r@x.com',
    });
    const body = {
      profile: { name: 'Raj' },
      trips: [{ id: 't1' }],
      nested: { snake_case_key: true },
    };
    const fetchMock = jest.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => body,
    });
    global.fetch = fetchMock as any;

    const out = await exportMyData();

    expect(fetchMock.mock.calls[0][0]).toContain('/api/v1/users/me/data');
    expect(fetchMock.mock.calls[0][1].method).toBe('GET');
    expect(fetchMock.mock.calls[0][1].headers.Authorization).toBe('Bearer tok');
    expect(out).toEqual(body);
    expect((out as any).nested.snake_case_key).toBe(true);
  });

  test('non-ok throws DATA_EXPORT_FAILED', async () => {
    const fetchMock = jest.fn().mockResolvedValue({ ok: false, status: 500 });
    global.fetch = fetchMock as any;

    await expect(exportMyData()).rejects.toThrow('DATA_EXPORT_FAILED');
  });
});

describe('data rights — eraseMyData', () => {
  afterEach(() => {
    global.fetch = globalFetch;
  });

  test('DELETEs user data endpoint with bearer', async () => {
    const fetchMock = jest.fn().mockResolvedValue({ ok: true, status: 204 });
    global.fetch = fetchMock as any;

    await eraseMyData();

    expect(fetchMock.mock.calls[0][0]).toContain('/api/v1/users/me/data');
    expect(fetchMock.mock.calls[0][1].method).toBe('DELETE');
    expect(fetchMock.mock.calls[0][1].headers.Authorization).toBe('Bearer tok');
  });

  test('204 no body resolves true without calling json()', async () => {
    const jsonFn = jest.fn();
    const fetchMock = jest.fn().mockResolvedValue({ ok: true, status: 204, json: jsonFn });
    global.fetch = fetchMock as any;

    await expect(eraseMyData()).resolves.toBe(true);
    expect(jsonFn).not.toHaveBeenCalled();
  });

  test('{success:true} resolves true', async () => {
    const fetchMock = jest.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ success: true }),
    });
    global.fetch = fetchMock as any;

    await expect(eraseMyData()).resolves.toBe(true);
  });

  test('non-ok throws DATA_ERASE_FAILED', async () => {
    const fetchMock = jest.fn().mockResolvedValue({ ok: false, status: 403 });
    global.fetch = fetchMock as any;

    await expect(eraseMyData()).rejects.toThrow('DATA_ERASE_FAILED');
  });

  test('ok but success falsy throws DATA_ERASE_FAILED', async () => {
    const fetchMock = jest.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ success: false }),
    });
    global.fetch = fetchMock as any;

    await expect(eraseMyData()).rejects.toThrow('DATA_ERASE_FAILED');
  });
});

describe('wipeLocalPii', () => {
  test('deletes both keys even when first delete rejects', async () => {
    (SecureStore.deleteItemAsync as jest.Mock)
      .mockRejectedValueOnce(new Error('secure store blew up'))
      .mockResolvedValueOnce(undefined);

    await expect(wipeLocalPii()).resolves.toBeUndefined();

    const keys = (SecureStore.deleteItemAsync as jest.Mock).mock.calls.map((c) => c[0]);
    expect(keys.sort()).toEqual(['auth_token', 'auth_user']);
  });

  test('happy path deletes both keys', async () => {
    (SecureStore.deleteItemAsync as jest.Mock).mockResolvedValue(undefined);

    await wipeLocalPii();

    expect(SecureStore.deleteItemAsync).toHaveBeenCalledWith('auth_token');
    expect(SecureStore.deleteItemAsync).toHaveBeenCalledWith('auth_user');
  });
});
