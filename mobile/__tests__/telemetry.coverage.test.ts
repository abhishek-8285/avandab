// Coverage strengthening for TelemetryService (was 0%).
// Drives permission/GPS acquisition matrix, live tracking subscription
// lifecycle, and camera permission paths against the global native mocks.
import * as Location from 'expo-location';
import { Camera } from 'expo-camera';
import { Telemetry } from '../src/services/telemetry';
import { getSQLiteMockState, resetSQLiteMockState } from '../jest/setup';

const Loc = Location as jest.Mocked<typeof Location>;
const Cam = Camera as unknown as {
  requestCameraPermissionsAsync: jest.Mock;
};

describe('Telemetry.requestLocationPermission', () => {
  beforeEach(() => {
    resetSQLiteMockState();
  });

  test('granted + stale last-known fix → coordinates returned flagged stale and persisted marked stale', async () => {
    const fixTime = Date.now() - 5 * 60 * 1000;
    Loc.getCurrentPositionAsync.mockResolvedValueOnce(null as any);
    Loc.getLastKnownPositionAsync.mockResolvedValueOnce({
      coords: { latitude: 18.5204, longitude: 73.8567, accuracy: 9 },
      timestamp: fixTime,
    } as any);

    const res = await Telemetry.requestLocationPermission();

    expect(res).toMatchObject({ granted: true, latitude: 18.5204, longitude: 73.8567 });
    expect(res.error).toMatch(/stale/i);
    expect(res.isStale).toBe(true);
    expect(res.isFallback).toBe(false);
    expect(res.lastFixAt).toBe(new Date(fixTime).toISOString());
    const logs = getSQLiteMockState().offline_gps_logs;
    expect(logs).toHaveLength(1);
    expect(logs[0]).toMatchObject({ latitude: 18.5204, longitude: 73.8567, accuracy: 9, is_stale: 1 });
  });

  test('live current position is preferred fresh when no last-known fix exists', async () => {
    const res = await Telemetry.requestLocationPermission();

    expect(res.granted).toBe(true);
    expect(res.latitude).toBe(19.076);
    expect(res.longitude).toBe(72.8777);
    expect(res.isStale).toBe(false);
    expect(res.isFallback).toBe(false);
    expect(getSQLiteMockState().offline_gps_logs).toHaveLength(1);
  });

  test('lastKnown throwing is swallowed and live position is used', async () => {
    Loc.getLastKnownPositionAsync.mockRejectedValueOnce(new Error('gps hw fault'));

    const res = await Telemetry.requestLocationPermission();

    expect(res.granted).toBe(true);
    expect(res.latitude).toBe(19.076);
    expect(res.isStale).toBe(false);
  });

  test('both fix sources unavailable → coarse viewport-only default, never persisted as a measurement', async () => {
    Loc.getLastKnownPositionAsync.mockResolvedValueOnce(null);
    Loc.getCurrentPositionAsync.mockResolvedValueOnce(null as any);

    const res = await Telemetry.requestLocationPermission();

    expect(res).toMatchObject({
      granted: true,
      latitude: 21.1458,
      longitude: 79.0882,
      isFallback: true,
    });
    expect(res.isStale).toBe(false);
    expect(res.lastFixAt).toBeNull();
    // Coarse default is viewport-only: zero SQLite rows (no fake Mumbai fix).
    expect(getSQLiteMockState().offline_gps_logs).toHaveLength(0);
  });

  test('current-position failure after missing lastKnown uses coarse viewport default without persisting', async () => {
    Loc.getCurrentPositionAsync.mockRejectedValueOnce(new Error('timeout'));

    const res = await Telemetry.requestLocationPermission();

    expect(res.granted).toBe(true);
    expect(res.latitude).toBe(21.1458);
    expect(res.isFallback).toBe(true);
    expect(getSQLiteMockState().offline_gps_logs).toHaveLength(0);
  });

  test('permission denied short-circuits with the OS status', async () => {
    Loc.requestForegroundPermissionsAsync.mockResolvedValueOnce({
      status: 'denied',
      granted: false,
    } as any);

    const res = await Telemetry.requestLocationPermission();

    expect(res.granted).toBe(false);
    expect(res.error).toContain('Permission status: denied');
  });

  test('device GPS toggle off keeps permission granted and serves honest fallback', async () => {
    Loc.hasServicesEnabledAsync.mockResolvedValueOnce(false);
    Loc.getLastKnownPositionAsync.mockResolvedValueOnce(null);
    Loc.getCurrentPositionAsync.mockResolvedValueOnce(null as any);

    const res = await Telemetry.requestLocationPermission();

    expect(res.granted).toBe(true); // permission held — GPS-off is fix state, not denial
    expect(res.isFallback).toBe(true);
    expect(res.error).toContain('Device GPS is OFF');
    expect(getSQLiteMockState().offline_gps_logs).toHaveLength(0);
  });

  test('unexpected permission-layer errors surface their message', async () => {
    Loc.requestForegroundPermissionsAsync.mockRejectedValueOnce(new Error('play services dead'));

    const res = await Telemetry.requestLocationPermission();

    expect(res).toMatchObject({
      granted: false,
      latitude: null,
      longitude: null,
      error: 'play services dead',
    });
  });
});

describe('Telemetry.startLiveLocationTracking', () => {
  beforeEach(() => {
    resetSQLiteMockState();
    Telemetry.stopLiveLocationTracking();
  });

  test('does nothing without foreground permission', async () => {
    Loc.getForegroundPermissionsAsync.mockResolvedValueOnce({ status: 'denied' } as any);

    await Telemetry.startLiveLocationTracking(jest.fn());

    expect(Loc.watchPositionAsync).not.toHaveBeenCalled();
  });

  test('streams fixes: persists them, converts m/s → km/h, passes null for bad speed', async () => {
    const onUpdate = jest.fn();
    const remove = jest.fn();
    Loc.watchPositionAsync.mockResolvedValueOnce({ remove } as any);

    await Telemetry.startLiveLocationTracking(onUpdate);

    expect(Loc.watchPositionAsync).toHaveBeenCalledWith(
      expect.objectContaining({ timeInterval: 10000, distanceInterval: 20 }),
      expect.any(Function)
    );
    const onFix = Loc.watchPositionAsync.mock.calls[0][1] as (loc: any) => Promise<void>;

    await onFix({ coords: { latitude: 19.1, longitude: 72.9, accuracy: 5, speed: 2 } });
    await onFix({ coords: { latitude: 19.2, longitude: 73.0, speed: -1 } }); // invalid speed
    await onFix({ coords: { latitude: 19.3, longitude: 73.1 } }); // speed absent

    const logs = getSQLiteMockState().offline_gps_logs;
    expect(logs.map((l) => l.latitude)).toEqual([19.1, 19.2, 19.3]);
    expect(logs[0].accuracy).toBe(5);
    expect(onUpdate).toHaveBeenNthCalledWith(1, 19.1, 72.9, 7); // 2 m/s ≈ 7 km/h
    expect(onUpdate).toHaveBeenNthCalledWith(2, 19.2, 73.0, null);
    expect(onUpdate).toHaveBeenNthCalledWith(3, 19.3, 73.1, null);

    Telemetry.stopLiveLocationTracking();
    expect(remove).toHaveBeenCalledTimes(1);
  });

  test('stop without an active subscription is a safe no-op', () => {
    expect(() => Telemetry.stopLiveLocationTracking()).not.toThrow();
  });
});

describe('Telemetry.requestCameraPermission', () => {
  test('granted camera permission succeeds cleanly', async () => {
    Cam.requestCameraPermissionsAsync.mockResolvedValueOnce({ status: 'granted' });

    await expect(Telemetry.requestCameraPermission()).resolves.toEqual({
      granted: true,
      error: null,
    });
  });

  test('denied camera permission reports honestly', async () => {
    Cam.requestCameraPermissionsAsync.mockResolvedValueOnce({ status: 'denied' });

    await expect(Telemetry.requestCameraPermission()).resolves.toEqual({
      granted: false,
      error: 'Camera permission denied',
    });
  });

  test('native camera errors surface their message instead of crashing', async () => {
    Cam.requestCameraPermissionsAsync.mockRejectedValueOnce(new Error('camera busy'));

    await expect(Telemetry.requestCameraPermission()).resolves.toEqual({
      granted: false,
      error: 'camera busy',
    });
  });
});
