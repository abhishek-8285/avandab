// Real coverage for the broker-credential service (the other MQTT tests mock
// this module, so nothing exercised it). The hardened broker refuses anonymous
// clients (docs/13 §5.2), so this is the path that decides whether a driver can
// publish at all — and the plaintext it returns is one-shot by design.
import * as SecureStore from 'expo-secure-store';
import {
  getBrokerCredential,
  storedBrokerCredential,
  clearCachedBrokerCredential,
} from '../src/services/mqttCredentials';
import { useAuthStore } from '../src/stores/authStore';

const Store = SecureStore as jest.Mocked<typeof SecureStore>;
const globalFetch = global.fetch;

const session = () => ({
  id: 'u_1',
  name: 'Raj',
  role: 'driver',
  email: 'r@x.com',
  driverId: 'drv_1',
});

function jsonResponse(body: unknown, ok = true, status = 200) {
  return { ok, status, json: async () => body } as unknown as Response;
}

describe('broker credential service', () => {
  beforeEach(async () => {
    clearCachedBrokerCredential();
    jest.clearAllMocks();
    Store.getItemAsync.mockResolvedValue(null);
    Store.setItemAsync.mockResolvedValue(undefined);
    await useAuthStore.getState().setAuth('tok', session() as any);
  });

  afterEach(() => {
    global.fetch = globalFetch;
    clearCachedBrokerCredential();
  });

  test('no session means no credential — never an unauthenticated request', async () => {
    useAuthStore.setState({ token: null });
    const fetchMock = jest.fn();
    global.fetch = fetchMock as any;

    await expect(getBrokerCredential()).resolves.toBeNull();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  test('a secret already in secure storage is used without touching the API', async () => {
    Store.getItemAsync.mockResolvedValue(JSON.stringify({ username: 'DRV-1', password: 'kept' }));
    const fetchMock = jest.fn();
    global.fetch = fetchMock as any;

    await expect(getBrokerCredential()).resolves.toEqual({ username: 'DRV-1', password: 'kept' });
    expect(fetchMock).not.toHaveBeenCalled();
  });

  test('first call provisions, persists the secret, and returns it once', async () => {
    const fetchMock = jest
      .fn()
      .mockResolvedValue(jsonResponse({ username: 'DRV-1', password: 'fresh', provisioned: true }));
    global.fetch = fetchMock as any;

    await expect(getBrokerCredential()).resolves.toEqual({ username: 'DRV-1', password: 'fresh' });
    expect(Store.setItemAsync).toHaveBeenCalledWith(
      'avandab_mqtt_credential',
      JSON.stringify({ username: 'DRV-1', password: 'fresh' }),
    );
    expect(fetchMock.mock.calls[0][0]).toContain('/api/v1/telemetry/mqtt-credentials');
    expect((fetchMock.mock.calls[0][1] as any).headers.Authorization).toBe('Bearer tok');
  });

  // The server keeps only the hash, so a device that lost its secret gets
  // `provisioned: false` — that is the signal to rotate, not an error.
  test('a device with no stored secret rotates instead of failing', async () => {
    const fetchMock = jest
      .fn()
      .mockResolvedValueOnce(jsonResponse({ username: 'DRV-1', provisioned: false }))
      .mockResolvedValueOnce(jsonResponse({ username: 'DRV-1', password: 'rotated', provisioned: true }));
    global.fetch = fetchMock as any;

    await expect(getBrokerCredential()).resolves.toEqual({ username: 'DRV-1', password: 'rotated' });
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(fetchMock.mock.calls[1][0]).toContain('/api/v1/telemetry/mqtt-credentials/rotate');
    expect((fetchMock.mock.calls[1][1] as any).method).toBe('POST');
  });

  test('rotation that yields nothing leaves the device without a credential', async () => {
    const fetchMock = jest
      .fn()
      .mockResolvedValueOnce(jsonResponse({ username: 'DRV-1', provisioned: false }))
      .mockResolvedValueOnce(jsonResponse({}, false, 500));
    global.fetch = fetchMock as any;

    await expect(getBrokerCredential()).resolves.toBeNull();
  });

  test('a refused request is not fatal — the app falls back to anonymous', async () => {
    global.fetch = jest.fn().mockResolvedValue(jsonResponse({}, false, 403)) as any;

    await expect(getBrokerCredential()).resolves.toBeNull();
  });

  test('a network failure is not fatal', async () => {
    global.fetch = jest.fn().mockRejectedValue(new Error('offline')) as any;

    await expect(getBrokerCredential()).resolves.toBeNull();
  });

  // A keystore that refuses must not crash tracking; the caller decides.
  test('secure storage that refuses degrades to a session-only credential', async () => {
    Store.getItemAsync.mockRejectedValue(new Error('keystore locked'));
    Store.setItemAsync.mockRejectedValue(new Error('keystore locked'));
    global.fetch = jest
      .fn()
      .mockResolvedValue(jsonResponse({ username: 'DRV-1', password: 'fresh', provisioned: true })) as any;

    await expect(getBrokerCredential()).resolves.toEqual({ username: 'DRV-1', password: 'fresh' });
  });

  test('a corrupt stored value is treated as no credential', async () => {
    Store.getItemAsync.mockResolvedValue('not json');
    global.fetch = jest
      .fn()
      .mockResolvedValue(jsonResponse({ username: 'DRV-1', password: 'fresh', provisioned: true })) as any;

    await expect(getBrokerCredential()).resolves.toEqual({ username: 'DRV-1', password: 'fresh' });
  });

  test('storedBrokerCredential returns null when the keystore is empty', async () => {
    await expect(storedBrokerCredential()).resolves.toBeNull();
  });

  test('clearing drops the session cache so the next call re-reads the store', async () => {
    Store.getItemAsync.mockResolvedValue(JSON.stringify({ username: 'DRV-1', password: 'kept' }));
    await expect(storedBrokerCredential()).resolves.toEqual({ username: 'DRV-1', password: 'kept' });

    clearCachedBrokerCredential();
    await expect(storedBrokerCredential()).resolves.toEqual({ username: 'DRV-1', password: 'kept' });
  });
});