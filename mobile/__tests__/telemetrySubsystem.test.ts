import { getAdaptiveSamplingConfig } from '../src/features/telemetry/battery/batteryPolicy';
import { filterLocationFrame } from '../src/features/telemetry/location/locationFilter';

describe('Telemetry Subsystem', () => {
  describe('Battery & Duty Adaptive Sampling', () => {
    it('stops sampling when OFF_DUTY', () => {
      const config = getAdaptiveSamplingConfig('OFF_DUTY', 100, false);
      expect(config.timeIntervalMs).toBe(0);
      expect(config.enableForegroundNotification).toBe(false);
    });

    it('enables high-frequency sampling (5s) for TRIP_ACTIVE', () => {
      const config = getAdaptiveSamplingConfig('TRIP_ACTIVE', 80, false);
      expect(config.timeIntervalMs).toBe(5000);
      expect(config.distanceIntervalMeters).toBe(5);
      expect(config.accuracyLevel).toBe(5);
    });

    it('throttles to 60s when battery is low (< 20%)', () => {
      const config = getAdaptiveSamplingConfig('TRIP_ACTIVE', 15, false);
      expect(config.timeIntervalMs).toBe(60000);
    });

    it('samples at 30s when AVAILABLE/standby', () => {
      const config = getAdaptiveSamplingConfig('AVAILABLE', 90, false);
      expect(config.timeIntervalMs).toBe(30000);
    });
  });

  describe('Location Filtering', () => {
    it('accepts valid Mumbai coordinate frame', () => {
      const res = filterLocationFrame({
        latitude: 19.076,
        longitude: 72.8777,
        accuracy: 8,
        speed: 12, // ~43 km/h
        heading: 180,
        timestamp: Date.now(),
      });
      expect(res.accepted).toBe(true);
    });

    it('rejects 0,0 uncalibrated coordinate frame', () => {
      const res = filterLocationFrame({
        latitude: 0,
        longitude: 0,
        accuracy: 10,
        speed: 0,
        heading: 0,
        timestamp: Date.now(),
      });
      expect(res.accepted).toBe(false);
      expect(res.reason).toContain('Zero-island');
    });

    it('rejects poor accuracy (> 100m error)', () => {
      const res = filterLocationFrame({
        latitude: 19.076,
        longitude: 72.8777,
        accuracy: 150,
        speed: 5,
        heading: 90,
        timestamp: Date.now(),
      });
      expect(res.accepted).toBe(false);
      expect(res.reason).toContain('Poor GPS accuracy');
    });

    it('rejects impossible speed spike (> 160 km/h for truck)', () => {
      const res = filterLocationFrame({
        latitude: 19.076,
        longitude: 72.8777,
        accuracy: 10,
        speed: 60, // 216 km/h
        heading: 90,
        timestamp: Date.now(),
      });
      expect(res.accepted).toBe(false);
      expect(res.reason).toContain('speed spike');
    });
  });
});
