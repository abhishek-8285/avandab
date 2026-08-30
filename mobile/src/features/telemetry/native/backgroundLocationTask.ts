import * as TaskManager from 'expo-task-manager';
import * as Location from 'expo-location';
import { filterLocationFrame } from '../location/locationFilter';
import { telemetrySQLiteBuffer, BufferedTelemetryFrame } from '../buffering/sqliteBuffer';
import { telemetrySyncer } from '../sync/telemetrySyncer';
import { getAdaptiveSamplingConfig, DutyState } from '../battery/batteryPolicy';

export const BACKGROUND_LOCATION_TASK_NAME = 'AVANDAB_BACKGROUND_LOCATION_TASK';

interface LocationTaskData {
  locations?: Location.LocationObject[];
}

let activeToken: string | null = null;
let activeSessionId: string | null = null;
let activeInstallationId: string = 'installation-default';
let activeTripId: string | null = null;

export function configureTelemetryContext(
  token: string | null,
  sessionId: string | null,
  installationId: string,
  tripId: string | null
) {
  activeToken = token;
  activeSessionId = sessionId;
  activeInstallationId = installationId;
  activeTripId = tripId;
}

TaskManager.defineTask(BACKGROUND_LOCATION_TASK_NAME, async ({ data, error }: any) => {
  if (error || !data) {
    return;
  }

  const { locations } = data as LocationTaskData;
  if (!locations || locations.length === 0) {
    return;
  }

  for (const loc of locations) {
    const filter = filterLocationFrame({
      latitude: loc.coords.latitude,
      longitude: loc.coords.longitude,
      accuracy: loc.coords.accuracy,
      speed: loc.coords.speed,
      heading: loc.coords.heading,
      timestamp: loc.timestamp,
    });

    if (!filter.accepted) {
      continue;
    }

    const frame: BufferedTelemetryFrame = {
      client_event_id: `evt_${Date.now()}_${Math.random().toString(36).substr(2, 6)}`,
      session_id: activeSessionId || 'default-session',
      installation_id: activeInstallationId,
      occurred_at: new Date(loc.timestamp).toISOString(),
      latitude: loc.coords.latitude,
      longitude: loc.coords.longitude,
      accuracy_meters: loc.coords.accuracy || 10,
      speed_kmph: (loc.coords.speed || 0) * 3.6,
      heading_degrees: loc.coords.heading || 0,
      battery_level_pct: 100,
      battery_state: 'unplugged',
      trip_id: activeTripId,
      synced: 0,
      created_at: Date.now(),
    };

    await telemetrySQLiteBuffer.insertFrame(frame);
  }

  if (activeToken) {
    telemetrySyncer.flush(activeToken).catch(() => {});
  }
});

export async function startBackgroundLocationUpdates(
  duty: DutyState,
  batteryPct = 100,
  isStationary = false
): Promise<void> {
  const config = getAdaptiveSamplingConfig(duty, batteryPct, isStationary);
  if (config.timeIntervalMs === 0) {
    await stopBackgroundLocationUpdates();
    return;
  }

  const isRunning = await Location.hasStartedLocationUpdatesAsync(BACKGROUND_LOCATION_TASK_NAME).catch(() => false);
  if (isRunning) {
    await Location.stopLocationUpdatesAsync(BACKGROUND_LOCATION_TASK_NAME).catch(() => {});
  }

  await Location.startLocationUpdatesAsync(BACKGROUND_LOCATION_TASK_NAME, {
    accuracy: Location.Accuracy.Balanced,
    timeInterval: config.timeIntervalMs,
    distanceInterval: config.distanceIntervalMeters,
    deferredUpdatesInterval: config.timeIntervalMs,
    deferredUpdatesDistance: config.distanceIntervalMeters,
    foregroundService: config.enableForegroundNotification
      ? {
          notificationTitle: 'Avandab Fleet Active',
          notificationBody: 'Streaming live GPS telemetry for commercial dispatch.',
          notificationColor: '#0d9488',
        }
      : undefined,
    pausesUpdatesAutomatically: false,
    showsBackgroundLocationIndicator: true,
  });
}

export async function stopBackgroundLocationUpdates(): Promise<void> {
  const isRunning = await Location.hasStartedLocationUpdatesAsync(BACKGROUND_LOCATION_TASK_NAME).catch(() => false);
  if (isRunning) {
    await Location.stopLocationUpdatesAsync(BACKGROUND_LOCATION_TASK_NAME).catch(() => {});
  }
}
