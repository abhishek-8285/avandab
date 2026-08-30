export interface RawLocationFrame {
  latitude: number;
  longitude: number;
  accuracy: number | null;
  speed: number | null;
  heading: number | null;
  timestamp: number;
}

export interface FilterResult {
  accepted: boolean;
  reason?: string;
}

const MAX_ACCEPTABLE_ACCURACY_METERS = 100;
const MAX_SPEED_KMPH = 160; // Commercial truck speed limit threshold

export function filterLocationFrame(frame: RawLocationFrame): FilterResult {
  if (isNaN(frame.latitude) || isNaN(frame.longitude)) {
    return { accepted: false, reason: 'NaN coordinates' };
  }

  if (frame.latitude === 0 && frame.longitude === 0) {
    return { accepted: false, reason: 'Zero-island coordinate (uncalibrated GPS)' };
  }

  if (frame.latitude < -90 || frame.latitude > 90 || frame.longitude < -180 || frame.longitude > 180) {
    return { accepted: false, reason: 'Coordinates out of physical range' };
  }

  if (frame.accuracy !== null && frame.accuracy > MAX_ACCEPTABLE_ACCURACY_METERS) {
    return { accepted: false, reason: `Poor GPS accuracy (${frame.accuracy.toFixed(1)}m > 100m)` };
  }

  if (frame.speed !== null) {
    const speedKmph = frame.speed * 3.6; // m/s to km/h
    if (speedKmph > MAX_SPEED_KMPH) {
      return { accepted: false, reason: `Unrealistic speed spike (${speedKmph.toFixed(0)} km/h)` };
    }
  }

  return { accepted: true };
}
