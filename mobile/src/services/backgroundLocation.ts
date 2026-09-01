import * as TaskManager from 'expo-task-manager';
import * as Location from 'expo-location';
import { DB } from './storage';
import { MQTT } from './mqtt';
import { useAuthStore } from '../stores/authStore';
import { readBatteryPct } from './telemetry';

export const BACKGROUND_LOCATION_TASK = 'AVANDAB_BACKGROUND_GPS';

type LocationCallback = (lat: number, lng: number, speedKmh?: number | null) => void;

let foregroundEcho: LocationCallback | null = null;

/**
 * The actual GPS batch processor. Exported so tests can drive it without
 * depending on TaskManager's module instance (jest-expo hands the service a
 * different sandbox than the test file sees).
 *
 * Each deferred fix is persisted to SQLite (flushed by SyncEngine) and echoed
 * to MQTT best-effort. No fabricated coordinates — fixes without lat/lng are
 * skipped.
 */
export async function backgroundGPSTask({ data, error }: any): Promise<void> {
  if (error) {
    console.log('[BG-GPS WARNING]', error.message);
    return;
  }
  const locations: any[] = data?.locations ?? [];
  const driverId =
    useAuthStore.getState().user?.driverId || useAuthStore.getState().user?.id || '';
  // Battery read once per batch — a native call per fix is wasted work when
  // the level moves a fraction of a percent between fixes.
  const batteryPct = await readBatteryPct();
  for (const loc of locations) {
    const { latitude, longitude } = loc.coords ?? {};
    if (latitude == null || longitude == null) continue;
    try {
      await DB.logGPSLocation(latitude, longitude, loc.coords.accuracy ?? null, {
        speed: typeof loc.coords.speed === 'number' ? loc.coords.speed : null,
        heading: typeof loc.coords.heading === 'number' ? loc.coords.heading : null,
        motion: typeof loc.coords.speed === 'number' ? loc.coords.speed > 0.5 : null,
        battery_level: batteryPct,
      });
    } catch (e: any) {
      console.log('[BG-GPS] SQLite log failed:', e?.message);
    }
    if (driverId) {
      MQTT.publishLocation(driverId, latitude, longitude);
    }
    foregroundEcho?.(latitude, longitude, typeof loc.coords.speed === 'number' ? Math.round(loc.coords.speed * 3.6) : null);
  }
}

// Registered at module scope so it is armed before the JS bundle finishes
// loading (TaskManager requirement).
TaskManager.defineTask(BACKGROUND_LOCATION_TASK, backgroundGPSTask);

export const BackgroundGPS = {
  /** The exact handler registered with TaskManager (test seam). */
  taskHandler: backgroundGPSTask,

  /** True when the OS-level background updates task is running. */
  async isRunning(): Promise<boolean> {
    try {
      return await Location.hasStartedLocationUpdatesAsync(BACKGROUND_LOCATION_TASK);
    } catch {
      return false;
    }
  },

  /**
   * Starts OS-level location updates that survive backgrounding. Requires
   * background permission (Android ACCESS_BACKGROUND_LOCATION is declared in
   * app.json; iOS needs Always authorization).
   */
  async start(): Promise<{ started: boolean; error: string | null }> {
    try {
      const fg = await Location.requestForegroundPermissionsAsync();
      if (!fg.granted) {
        return { started: false, error: 'Foreground location permission denied' };
      }
      const bg = await Location.requestBackgroundPermissionsAsync();
      if (!bg.granted) {
        return { started: false, error: 'Background location permission denied — GPS runs only while the app is open' };
      }

      await Location.startLocationUpdatesAsync(BACKGROUND_LOCATION_TASK, {
        accuracy: Location.Accuracy.Balanced,
        timeInterval: 15000,
        distanceInterval: 25,
        showsBackgroundLocationIndicator: true,
        pausesUpdatesAutomatically: false,
        foregroundService: {
          notificationTitle: 'Avandab trip tracking active',
          notificationBody: 'Sharing live position for your assigned trip.',
          notificationColor: '#00685f',
          killServiceOnDestroy: false,
        },
      });
      return { started: true, error: null };
    } catch (e: any) {
      return { started: false, error: e?.message || 'Failed to start background GPS' };
    }
  },

  async stop(): Promise<void> {
    try {
      await Location.stopLocationUpdatesAsync(BACKGROUND_LOCATION_TASK);
    } catch {
      // not started — nothing to stop
    }
  },

  /**
   * Optional UI echo: while a screen wants live coords, background fixes are
   * forwarded to it too (single subscription slot; screens re-register on
   * focus).
   */
  setForegroundEcho(cb: LocationCallback | null): void {
    foregroundEcho = cb;
  },
};
