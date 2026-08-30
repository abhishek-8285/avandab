export type DutyState =
  | 'OFF_DUTY'
  | 'AVAILABLE'
  | 'ON_DUTY'
  | 'TRIP_ACTIVE'
  | 'STATIONARY'
  | 'LOW_BATTERY';

export interface SamplingConfig {
  timeIntervalMs: number;
  distanceIntervalMeters: number;
  accuracyLevel: number; // 1-6 (Balanced to Highest)
  enableForegroundNotification: boolean;
}

export function getAdaptiveSamplingConfig(
  duty: DutyState,
  batteryPct: number,
  isStationary: boolean
): SamplingConfig {
  if (duty === 'OFF_DUTY') {
    return {
      timeIntervalMs: 0, // Stopped
      distanceIntervalMeters: 0,
      accuracyLevel: 1,
      enableForegroundNotification: false,
    };
  }

  // Battery Conservation Mode (< 20% battery)
  if (batteryPct > 0 && batteryPct < 20) {
    return {
      timeIntervalMs: 60000, // 60s
      distanceIntervalMeters: 50,
      accuracyLevel: 3, // Balanced
      enableForegroundNotification: true,
    };
  }

  if (isStationary || duty === 'STATIONARY') {
    return {
      timeIntervalMs: 60000, // 60s
      distanceIntervalMeters: 25,
      accuracyLevel: 3,
      enableForegroundNotification: true,
    };
  }

  if (duty === 'TRIP_ACTIVE') {
    return {
      timeIntervalMs: 5000, // 5s for active freight navigation
      distanceIntervalMeters: 5,
      accuracyLevel: 5, // BestForNavigation / High
      enableForegroundNotification: true,
    };
  }

  if (duty === 'ON_DUTY') {
    return {
      timeIntervalMs: 15000, // 15s
      distanceIntervalMeters: 10,
      accuracyLevel: 4,
      enableForegroundNotification: true,
    };
  }

  // AVAILABLE (Standby / Idle)
  return {
    timeIntervalMs: 30000, // 30s
    distanceIntervalMeters: 20,
    accuracyLevel: 3,
    enableForegroundNotification: true,
  };
}
