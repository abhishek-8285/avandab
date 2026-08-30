import * as Location from 'expo-location';
import { Camera } from 'expo-camera';
import { DB } from './storage';

export interface LocationState {
  granted: boolean;
  latitude: number | null;
  longitude: number | null;
  error: string | null;
}

export interface CameraState {
  granted: boolean;
  error: string | null;
}

class TelemetryService {
  private locationSubscription: Location.LocationSubscription | null = null;

  // Request Location Permissions & Start Instrumentation Tracking
  async requestLocationPermission(): Promise<LocationState> {
    try {
      const response = await Location.requestForegroundPermissionsAsync();
      const isEnabled = await Location.hasServicesEnabledAsync();
      
      if (!response.granted && response.status !== 'granted') {
        return { granted: false, latitude: null, longitude: null, error: `Permission status: ${response.status}` };
      }

      // Check if location services (GPS toggle) are enabled on device
      if (!isEnabled) {
        return { granted: false, latitude: null, longitude: null, error: 'Device GPS is OFF in Android Quick Settings' };
      }

      // Fast location retrieval with timeout fallback
      let coords: { latitude: number; longitude: number; accuracy: number | null } | null = null;

      try {
        const locationPromise = Location.getLastKnownPositionAsync();
        const timeoutPromise = new Promise<null>((resolve) => setTimeout(() => resolve(null), 1000));
        const lastKnown = await Promise.race([locationPromise, timeoutPromise]);

        if (lastKnown && lastKnown.coords) {
          coords = {
            latitude: lastKnown.coords.latitude,
            longitude: lastKnown.coords.longitude,
            accuracy: lastKnown.coords.accuracy ?? null,
          };
        }
      } catch {}

      if (!coords) {
        // Fast attempt with timeout so indoors/airplane mode never hangs
        try {
          const currentPromise = Location.getCurrentPositionAsync({ accuracy: Location.Accuracy.Lowest });
          const timeout = new Promise<null>((resolve) => setTimeout(() => resolve(null), 1500));
          const current = await Promise.race([currentPromise, timeout]);
          if (current && current.coords) {
            coords = {
              latitude: current.coords.latitude,
              longitude: current.coords.longitude,
              accuracy: current.coords.accuracy ?? null,
            };
          }
        } catch {}
      }

      const defaultLat = coords ? coords.latitude : 19.0760;
      const defaultLng = coords ? coords.longitude : 72.8777;

      try {
        // Log telemetry event to offline SQLite database
        await DB.logGPSLocation(defaultLat, defaultLng, coords?.accuracy ?? null);
      } catch {}

      return {
        granted: true,
        latitude: defaultLat,
        longitude: defaultLng,
        error: null,
      };
    } catch (err: any) {
      return { granted: false, latitude: null, longitude: null, error: err.message || 'Location error' };
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
        // Instrument location telemetry: log to SQLite DB
        await DB.logGPSLocation(latitude, longitude, loc.coords.accuracy ?? null);
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
