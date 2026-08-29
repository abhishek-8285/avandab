import * as Location from 'expo-location';
import { BackgroundGPS, backgroundGPSTask } from '../services/backgroundLocation';

export type LocationPolicy = {
  accuracy: Location.Accuracy;
  timeInterval: number;
  distanceInterval: number;
};

const POLICIES: Record<string, LocationPolicy> = {
  inactive: { accuracy: Location.Accuracy.Low, timeInterval: 60000, distanceInterval: 100 },
  active_moving: { accuracy: Location.Accuracy.Balanced, timeInterval: 15000, distanceInterval: 25 },
  active_stationary: { accuracy: Location.Accuracy.Balanced, timeInterval: 30000, distanceInterval: 25 },
  poor_accuracy: { accuracy: Location.Accuracy.Low, timeInterval: 30000, distanceInterval: 50 },
};

function pickPolicy(tripActive: boolean, moving: boolean, accuracy: number | null): LocationPolicy {
  if (!tripActive) return POLICIES.inactive;
  if (accuracy != null && accuracy > 50) return POLICIES.poor_accuracy;
  return moving ? POLICIES.active_moving : POLICIES.active_stationary;
}

export const LocationService = {
  policies: POLICIES,
  pickPolicy,

  async requestPermissions(): Promise<{ granted: boolean; error?: string }> {
    const fg = await Location.requestForegroundPermissionsAsync();
    if (!fg.granted) return { granted: false, error: 'Foreground denied' };
    const bg = await Location.requestBackgroundPermissionsAsync();
    if (!bg.granted) return { granted: false, error: 'Background denied — tracking while open only' };
    return { granted: true };
  },

  async start(tripActive: boolean) {
    const perms = await this.requestPermissions();
    if (!perms.granted) return perms;
    const policy = pickPolicy(tripActive, true, null);
    return BackgroundGPS.start(); // delegates to native foregroundService Avandab trip tracking active
  },

  async stop() {
    await BackgroundGPS.stop();
  },

  // Pure filter: drop poor accuracy fixes that would overwrite good position
  shouldAcceptFix(accuracy: number | null, speedKmh: number | null): boolean {
    if (accuracy != null && accuracy > 100) return false;
    return true;
  },
};
