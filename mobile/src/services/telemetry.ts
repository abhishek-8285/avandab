import * as Location from 'expo-location';
import { Camera } from 'expo-camera';
import { DB } from './storage';

// readBatteryPct returns the phone battery as 0-100, or null when the
// platform refuses (expo-battery absence/emulator quirk must never break a
// GPS fix — every caller treats null as "unknown").
export async function readBatteryPct(): Promise<number | null> {
  try {
    const Battery = require('expo-battery');
    if (Battery && typeof Battery.getBatteryLevelAsync === 'function') {
      const lvl = await Battery.getBatteryLevelAsync();
      if (typeof lvl === 'number' && lvl >= 0 && lvl <= 1) {
        return Math.round(lvl * 100);
      }
    }
    return null;
  } catch {
    return null;
  }
}

export interface LocationState {
  /** OS foreground permission ONLY — never conflated with fix availability. */
  granted: boolean;
  latitude: number | null;
  longitude: number | null;
  error: string | null;
  /** True: aged last-known/persisted fix. Viewport-usable, publishable ONLY flagged stale. */
  isStale?: boolean;
  /** True: coarse viewport-only default. Never persisted, never published. */
  isFallback?: boolean;
  /** ISO timestamp of the underlying fix; null for coarse/denied (no fix). */
  lastFixAt?: string | null;
  /** Consumer-facing accuracy (degraded for stale); null when unknown. */
  accuracy?: number | null;
  /** Reported platform speed in m/s; null when unknown — never fabricated. */
  speed?: number | null;
  /** Age of a stale fix in ms; null for fresh/coarse. */
  staleAgeMs?: number | null;
}

/** Coarse viewport-only default: Zero Mile, Nagpur (geographic centre of India). */
export const COARSE_LATITUDE = 21.1458;
export const COARSE_LONGITUDE = 79.0882;
/** Staleness penalty applied to consumer-facing accuracy of aged fixes. */
export const STALE_ACCURACY_PENALTY_M = 500;

/**
 * Publish gate: coarse fallbacks are viewport-only and must never leave the
 * device as telemetry. Stale fixes pass — callers must flag them stale.
 */
export function canPublishFix(s: Pick<LocationState, 'latitude' | 'longitude' | 'isFallback'>): boolean {
  return s.latitude != null && s.longitude != null && !s.isFallback;
}

export interface CameraState {
  granted: boolean;
  error: string | null;
}

class TelemetryService {
  private locationSubscription: Location.LocationSubscription | null = null;

  // Honest no-fix chain — a missing GPS must never fabricate a measurement:
  // (1) live fix → fresh, persisted + publishable unflagged;
  // (2) last-known (Expo, then our own persisted fix) → stale-flagged with age
  //     + degraded accuracy; viewport-usable, publishable ONLY flagged stale;
  // (3) coarse Zero-Mile default → viewport-only, never persisted/published.
  // `granted` reflects OS permission only; fix absence is reported via
  // isStale/isFallback + error, never by flipping granted.
  async requestLocationPermission(): Promise<LocationState> {
    const fresh = (): LocationState => ({
      granted: true, latitude: null, longitude: null, error: null,
      isStale: false, isFallback: false, lastFixAt: null,
      accuracy: null, speed: null, staleAgeMs: null,
    });
    try {
      const response = await Location.requestForegroundPermissionsAsync();
      const permissionGranted = response.granted || response.status === 'granted';
      if (!permissionGranted) {
        return { ...fresh(), granted: false, error: `Permission status: ${response.status}` };
      }

      // GPS toggle is fix state, not permission state — it shapes the error
      // and skips the live attempt, never the granted flag.
      let gpsOff = false;
      try {
        gpsOff = !(await Location.hasServicesEnabledAsync());
      } catch {}
      const gpsOffError = 'Device GPS is OFF in Android Quick Settings';

      const nowIso = () => new Date().toISOString();
      const toIso = (ts: unknown): string | null => {
        try {
          if (typeof ts === 'number' && Number.isFinite(ts)) return new Date(ts).toISOString();
          if (typeof ts === 'string' && ts) {
            const d = new Date(ts);
            if (!Number.isNaN(d.getTime())) return d.toISOString();
          }
        } catch {}
        return null;
      };
      const degrade = (accuracy: number | null): number | null =>
        accuracy == null ? null : accuracy + STALE_ACCURACY_PENALTY_M;

      // (1) Live fix first — the only source of a fresh measurement.
      if (!gpsOff) {
        try {
          const currentPromise = Location.getCurrentPositionAsync({ accuracy: Location.Accuracy.Lowest });
          const timeout = new Promise<null>((resolve) => setTimeout(() => resolve(null), 1500));
          const current = await Promise.race([currentPromise, timeout]);
          if (current && current.coords) {
            const lat = current.coords.latitude;
            const lng = current.coords.longitude;
            if (lat != null && lng != null) {
              const accuracy = current.coords.accuracy ?? null;
              const speed = typeof current.coords.speed === 'number' ? current.coords.speed : null;
              const heading = typeof current.coords.heading === 'number' ? current.coords.heading : null;
              try {
                const batteryPct = await readBatteryPct();
                await DB.logGPSLocation(lat, lng, accuracy, {
                  speed, heading,
                  motion: speed != null ? speed > 0.5 : null,
                  battery_level: batteryPct,
                });
              } catch {}
              return {
                ...fresh(), latitude: lat, longitude: lng,
                lastFixAt: toIso((current as { timestamp?: unknown }).timestamp) ?? nowIso(),
                accuracy, speed,
              };
            }
          }
        } catch {}
      }

      // (2a) Stale: Expo's last-known fix — a real past measurement, flagged.
      try {
        const locationPromise = Location.getLastKnownPositionAsync();
        const timeoutPromise = new Promise<null>((resolve) => setTimeout(() => resolve(null), 1000));
        const lastKnown = await Promise.race([locationPromise, timeoutPromise]);
        if (lastKnown && lastKnown.coords?.latitude != null && lastKnown.coords?.longitude != null) {
          const lat = lastKnown.coords.latitude;
          const lng = lastKnown.coords.longitude;
          const reportedAccuracy = lastKnown.coords.accuracy ?? null;
          const speed = typeof lastKnown.coords.speed === 'number' ? lastKnown.coords.speed : null;
          const lastFixAt = toIso((lastKnown as { timestamp?: unknown }).timestamp) ?? nowIso();
          const staleAgeMs = Math.max(0, Date.parse(nowIso()) - Date.parse(lastFixAt));
          try {
            // Persisted raw (as reported) but marked stale so sync flags it —
            // the degraded accuracy is consumer-facing only.
            const batteryPct = await readBatteryPct();
            await DB.logGPSLocation(lat, lng, reportedAccuracy, {
              speed,
              heading: typeof lastKnown.coords.heading === 'number' ? lastKnown.coords.heading : null,
              motion: speed != null ? speed > 0.5 : null,
              battery_level: batteryPct,
              isStale: true,
            });
          } catch {}
          return {
            ...fresh(), latitude: lat, longitude: lng,
            error: gpsOff ? gpsOffError : 'No live GPS fix — showing last-known position (stale)',
            isStale: true, lastFixAt, accuracy: degrade(reportedAccuracy), speed, staleAgeMs,
          };
        }
      } catch {}

      // (2b) Stale: our own last persisted fix — already stored, never re-inserted.
      try {
        const last = await DB.getLastGPSLog();
        if (last && last.latitude != null && last.longitude != null) {
          const lastFixAt = last.timestamp ?? nowIso();
          let staleAgeMs: number | null = null;
          try {
            staleAgeMs = Math.max(0, Date.now() - Date.parse(lastFixAt));
          } catch {}
          return {
            ...fresh(), latitude: last.latitude, longitude: last.longitude,
            error: gpsOff ? gpsOffError : 'No live GPS fix — showing last saved position (stale)',
            isStale: true, lastFixAt,
            accuracy: degrade(last.accuracy),
            speed: last.speed, staleAgeMs,
          };
        }
      } catch {}

      // (3) Coarse viewport-only default — never persisted, never published
      // (callers gate via canPublishFix).
      return {
        ...fresh(), latitude: COARSE_LATITUDE, longitude: COARSE_LONGITUDE,
        error: gpsOff ? gpsOffError : 'No GPS fix available — showing coarse map default (not a measurement)',
        isFallback: true,
      };
    } catch (err: any) {
      return { ...fresh(), granted: false, error: err.message || 'Location error' };
    }
  }

  // Subscribe to live continuous GPS updates for trip route tracking.
  // onLocationUpdate receives (lat, lng, speedKmh) — speed is null when the
  // platform does not report it; callers must not fabricate a value.
  async startLiveLocationTracking(
    onLocationUpdate: (lat: number, lng: number, speedKmh?: number | null) => void
  ): Promise<void> {
    const { status } = await Location.getForegroundPermissionsAsync();
    if (status !== 'granted') return;

    // Single-subscription guard: re-entry (tab switch / refocus) must not
    // accumulate watchers — stop the previous one before starting a new one.
    if (this.locationSubscription) {
      try {
        this.locationSubscription.remove();
      } catch {}
      this.locationSubscription = null;
    }
    this.locationSubscription = await Location.watchPositionAsync(
      {
        accuracy: Location.Accuracy.Balanced,
        timeInterval: 10000, // Every 10 seconds
        distanceInterval: 20, // Or every 20 meters
      },
      async (loc) => {
        const { latitude, longitude } = loc.coords;
        const speedKmh =
          typeof loc.coords.speed === 'number' && loc.coords.speed >= 0
            ? Math.round(loc.coords.speed * 3.6)
            : null;
        // Instrument location telemetry: log to SQLite DB (speed in m/s from
        // the platform; motion derived — anything under 0.5 m/s is parked).
        const batteryPct = await readBatteryPct();
        await DB.logGPSLocation(latitude, longitude, loc.coords.accuracy ?? null, {
          speed: typeof loc.coords.speed === 'number' ? loc.coords.speed : null,
          heading: typeof loc.coords.heading === 'number' ? loc.coords.heading : null,
          motion: typeof loc.coords.speed === 'number' ? loc.coords.speed > 0.5 : null,
          battery_level: batteryPct,
        });
        onLocationUpdate(latitude, longitude, speedKmh);
      }
    );
  }

  stopLiveLocationTracking(): void {
    if (this.locationSubscription) {
      this.locationSubscription.remove();
      this.locationSubscription = null;
    }
  }

  // Request Camera Permission
  async requestCameraPermission(): Promise<CameraState> {
    try {
      const { status } = await Camera.requestCameraPermissionsAsync();
      if (status !== 'granted') {
        return { granted: false, error: 'Camera permission denied' };
      }
      return { granted: true, error: null };
    } catch (err: any) {
      return { granted: false, error: err.message || 'Camera error' };
    }
  }
}

export const Telemetry = new TelemetryService();
