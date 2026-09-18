// Red-green ownership: missing GPS must never fabricate a fresh measurement.
// Fallback chain under test:
//   (1) live fix → fresh, persisted + publishable unflagged
//   (2) last-known (Expo, then our own persisted fix) → stale-flagged with age
//       + degraded accuracy; viewport-usable, publishable ONLY flagged stale
//   (3) coarse Zero-Mile Nagpur default → viewport-ONLY, never persisted nor
//       published.
// Permission (granted) is OS state only — never conflated with fix availability.
import * as Location from 'expo-location';
import { Telemetry, canPublishFix, COARSE_LATITUDE, COARSE_LONGITUDE } from '../src/services/telemetry';
import { DB } from '../src/services/storage';
import { MQTT } from '../src/services/mqtt';
import { getSQLiteMockState, resetSQLiteMockState } from '../jest/setup';

const Loc = Location as jest.Mocked<typeof Location>;

jest.mock('../src/services/mqtt', () => ({ MQTT: { publishLocation: jest.fn() } }));
const publishMock = MQTT.publishLocation as jest.Mock;

describe('honest no-fix location behavior', () => {
  beforeEach(() => {
    resetSQLiteMockState();
    Telemetry.stopLiveLocationTracking();
    publishMock.mockClear();
  });

  test('no fix anywhere → coarse Nagpur viewport default, granted stays true, nothing persisted', async () => {
    Loc.getLastKnownPositionAsync.mockResolvedValueOnce(null);
    Loc.getCurrentPositionAsync.mockResolvedValueOnce(null as any);

    const res = await Telemetry.requestLocationPermission();

    expect(res.granted).toBe(true); // OS permission held — fix absence is not denial
    expect(res.latitude).toBe(COARSE_LATITUDE);
    expect(res.longitude).toBe(COARSE_LONGITUDE);
    expect(COARSE_LATITUDE).toBe(21.1458);
    expect(COARSE_LONGITUDE).toBe(79.0882);
    expect(res.isFallback).toBe(true);
    expect(res.isStale).toBe(false);
    expect(res.lastFixAt).toBeNull();
    expect(res.error).toMatch(/no gps fix/i);
    // Viewport-only: must never become a fake SQLite measurement…
    expect(getSQLiteMockState().offline_gps_logs).toHaveLength(0);
    // …and must never be published as telemetry.
    expect(canPublishFix(res)).toBe(false);
  });

  test('stale Expo last-known fix → flagged stale with age, persisted marked stale, publishable only flagged', async () => {
    const fixTime = Date.now() - 10 * 60 * 1000; // 10 min old
    Loc.getCurrentPositionAsync.mockResolvedValueOnce(null as any);
    Loc.getLastKnownPositionAsync.mockResolvedValueOnce({
      coords: { latitude: 18.5204, longitude: 73.8567, accuracy: 9, speed: null, heading: null },
      timestamp: fixTime,
    } as any);

    const res = await Telemetry.requestLocationPermission();

    expect(res.granted).toBe(true);
    expect(res.latitude).toBe(18.5204);
    expect(res.longitude).toBe(73.8567);
    expect(res.isStale).toBe(true);
    expect(res.isFallback).toBe(false);
    expect(res.lastFixAt).toBe(new Date(fixTime).toISOString());
    expect(res.staleAgeMs).toBeGreaterThan(0);
    // Reduced accuracy: consumer-facing confidence is worse than reported 9 m.
    expect(res.accuracy).not.toBe(9);
    expect(res.accuracy as number).toBeGreaterThan(9);
    // Real past measurement → persisted, but marked stale so sync flags it…
    const logs = getSQLiteMockState().offline_gps_logs;
    expect(logs).toHaveLength(1);
    expect(logs[0].is_stale).toBe(1);
    // …and publishable only flagged stale.
    expect(canPublishFix(res)).toBe(true);
    MQTT.publishLocation('drv_1', res.latitude as number, res.longitude as number, { isStale: res.isStale });
    expect(publishMock).toHaveBeenCalledWith('drv_1', 18.5204, 73.8567, { isStale: true });
  });

  test('own last persisted fix backs viewport when Expo has nothing → stale, never duplicated', async () => {
    await DB.logGPSLocation(19.11, 72.91, 12);
    expect(getSQLiteMockState().offline_gps_logs).toHaveLength(1);
    Loc.getCurrentPositionAsync.mockResolvedValueOnce(null as any);
    Loc.getLastKnownPositionAsync.mockResolvedValueOnce(null);

    const res = await Telemetry.requestLocationPermission();

    expect(res.latitude).toBe(19.11);
    expect(res.longitude).toBe(72.91);
    expect(res.isStale).toBe(true);
    expect(res.isFallback).toBe(false);
    expect(res.lastFixAt).toEqual(expect.any(String));
    expect(canPublishFix(res)).toBe(true); // publishable — only flagged stale
    // Already persisted: serving it must not insert a duplicate fake-fresh row.
    expect(getSQLiteMockState().offline_gps_logs).toHaveLength(1);
  });

  test('live fix → fresh, unflagged, persisted as a real measurement', async () => {
    Loc.getCurrentPositionAsync.mockResolvedValueOnce({
      coords: { latitude: 19.07, longitude: 72.88, accuracy: 8, speed: 5, heading: 90 },
      timestamp: Date.now(),
    } as any);

    const res = await Telemetry.requestLocationPermission();

    expect(res.isStale).toBe(false);
    expect(res.isFallback).toBe(false);
    expect(res.accuracy).toBe(8);
    const logs = getSQLiteMockState().offline_gps_logs;
    expect(logs).toHaveLength(1);
    expect(logs[0].is_stale).not.toBe(1);
    expect(canPublishFix(res)).toBe(true);
  });

  test('GPS toggle off is not permission denial: granted true + honest fallback + error', async () => {
    Loc.hasServicesEnabledAsync.mockResolvedValueOnce(false);
    Loc.getLastKnownPositionAsync.mockResolvedValueOnce(null);
    Loc.getCurrentPositionAsync.mockResolvedValueOnce(null as any);

    const res = await Telemetry.requestLocationPermission();

    expect(res.granted).toBe(true);
    expect(res.isFallback).toBe(true);
    expect(res.error).toMatch(/gps is off/i);
    expect(getSQLiteMockState().offline_gps_logs).toHaveLength(0);
  });

  test('permission denied → granted false with null coords (never a fallback presented as fix)', async () => {
    Loc.requestForegroundPermissionsAsync.mockResolvedValueOnce({ status: 'denied', granted: false } as any);

    const res = await Telemetry.requestLocationPermission();

    expect(res.granted).toBe(false);
    expect(res.latitude).toBeNull();
    expect(res.longitude).toBeNull();
    expect(canPublishFix(res)).toBe(false);
  });

  test('unknown speed stays null end-to-end (no fabricated 48 km/h)', async () => {
    const onUpdate = jest.fn();
    const remove = jest.fn();
    Loc.watchPositionAsync.mockResolvedValueOnce({ remove } as any);
    await Telemetry.startLiveLocationTracking(onUpdate);
    const onFix = Loc.watchPositionAsync.mock.calls[0][1] as (loc: any) => Promise<void>;

    await onFix({ coords: { latitude: 19.1, longitude: 72.9, accuracy: 5 } }); // speed absent

    expect(onUpdate).toHaveBeenCalledWith(19.1, 72.9, null);
    Telemetry.stopLiveLocationTracking();
  });
});
