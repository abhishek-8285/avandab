// Coverage strengthening for BackgroundGPS edge branches: malformed task
// payloads, driver-id fallback chain, SQLite write failures, and start/stop
// error paths.
import * as SQLite from 'expo-sqlite';
import * as Location from 'expo-location';
import { backgroundGPSTask, BackgroundGPS } from '../src/services/backgroundLocation';
import { MQTT } from '../src/services/mqtt';
import { useAuthStore } from '../src/stores/authStore';
import { getSQLiteMockState, resetSQLiteMockState } from '../jest/setup';

jest.mock('../src/services/mqtt', () => ({
  MQTT: { publishLocation: jest.fn() },
}));

const Loc = Location as jest.Mocked<typeof Location>;

const handler = backgroundGPSTask as (evt: any) => Promise<void>;

async function authAs(user: Record<string, unknown>): Promise<void> {
  await useAuthStore.getState().setAuth('tok', {
    id: 'u_1',
    name: 'Raj',
    role: 'driver',
    email: 'r@x.com',
    ...user,
  });
}

describe('BackgroundGPS task handler edges', () => {
  let logSpy: jest.SpyInstance;

  beforeEach(async () => {
    resetSQLiteMockState();
    await authAs({ driverId: 'drv_9' });
    logSpy = jest.spyOn(console, 'log').mockImplementation(() => {});
  });

  afterEach(() => {
    BackgroundGPS.setForegroundEcho(null);
    logSpy.mockRestore();
  });

  test('missing data payload resolves without touching storage or MQTT', async () => {
    await expect(handler({ data: null })).resolves.toBeUndefined();
    expect(getSQLiteMockState().offline_gps_logs).toHaveLength(0);
    expect(MQTT.publishLocation).not.toHaveBeenCalled();
  });

  test('fixes without usable coordinates are skipped silently', async () => {
    const echo = jest.fn();
    BackgroundGPS.setForegroundEcho(echo);

    await handler({
      data: {
        locations: [{}, { coords: { latitude: null, longitude: 73.8 } }],
      },
    });

    expect(getSQLiteMockState().offline_gps_logs).toHaveLength(0);
    expect(echo).not.toHaveBeenCalled();
    expect(MQTT.publishLocation).not.toHaveBeenCalled();
  });

  test('non-numeric speed echoes as null instead of a fabricated value', async () => {
    const echo = jest.fn();
    BackgroundGPS.setForegroundEcho(echo);

    await handler({
      data: { locations: [{ coords: { latitude: 28.71, longitude: 77.1 } }] },
    });

    expect(echo).toHaveBeenCalledWith(28.71, 77.1, null);
  });

  test('driver id falls back to user.id when driverId is absent', async () => {
    await useAuthStore.getState().logout();
    await useAuthStore.getState().setAuth('tok', {
      id: 'u_77',
      name: 'Fallback',
      role: 'driver',
      email: 'f@x.com',
    });

    await handler({
      data: { locations: [{ coords: { latitude: 19.0, longitude: 72.8 } }] },
    });

    expect(MQTT.publishLocation).toHaveBeenCalledWith('u_77', 19.0, 72.8);
  });

  test('SQLite write failure is logged; MQTT publish and UI echo still proceed', async () => {
    const db = await SQLite.openDatabaseAsync('probe');
    (db.runAsync as jest.Mock).mockRejectedValueOnce(new Error('disk full'));

    const echo = jest.fn();
    BackgroundGPS.setForegroundEcho(echo);

    await handler({
      data: { locations: [{ coords: { latitude: 18.5, longitude: 73.8, accuracy: 6 } }] },
    });

    expect(logSpy).toHaveBeenCalledWith('[BG-GPS] SQLite log failed:', 'disk full');
    expect(MQTT.publishLocation).toHaveBeenCalledWith('drv_9', 18.5, 73.8);
    expect(echo).toHaveBeenCalledWith(18.5, 73.8, null);
  });
});

describe('BackgroundGPS lifecycle edges', () => {
  beforeEach(async () => {
    resetSQLiteMockState();
    await authAs({ driverId: 'drv_9' });
  });

  afterEach(() => {
    jest.restoreAllMocks();
  });

  test('start refuses to arm OS updates without foreground permission', async () => {
    Loc.requestForegroundPermissionsAsync.mockResolvedValueOnce({
      status: 'denied',
      granted: false,
    } as any);

    const res = await BackgroundGPS.start();

    expect(res.started).toBe(false);
    expect(res.error).toBe('Foreground location permission denied');
    expect(Loc.startLocationUpdatesAsync).not.toHaveBeenCalled();
  });

  test('OS-level start failure is reported with the native message', async () => {
    Loc.startLocationUpdatesAsync.mockRejectedValueOnce(new Error('foreground service crash'));

    const res = await BackgroundGPS.start();

    expect(res.started).toBe(false);
    expect(res.error).toBe('foreground service crash');
  });

  test('isRunning maps OS query failures to false', async () => {
    Loc.hasStartedLocationUpdatesAsync.mockRejectedValueOnce(new Error('os grumpy'));

    await expect(BackgroundGPS.isRunning()).resolves.toBe(false);
  });
});
