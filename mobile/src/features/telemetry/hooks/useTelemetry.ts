import { useState, useEffect, useCallback } from 'react';
import { telemetryApi } from '../api/telemetryApi';
import { telemetrySQLiteBuffer } from '../buffering/sqliteBuffer';
import {
  configureTelemetryContext,
  startBackgroundLocationUpdates,
  stopBackgroundLocationUpdates,
} from '../native/backgroundLocationTask';
import { requestLocationPermissions } from '../permissions/locationPermissions';
import { DutyState } from '../battery/batteryPolicy';

export function useTelemetry(token?: string, initialDuty: DutyState = 'AVAILABLE') {
  const [sessionId, setSessionId] = useState<string | null>(null);
  const [vehicleId, setVehicleId] = useState<string | null>(null);
  const [isTracking, setIsTracking] = useState<boolean>(false);
  const [unsyncedCount, setUnsyncedCount] = useState<number>(0);
  const [dutyState, setDutyState] = useState<DutyState>(initialDuty);

  const refreshBacklog = useCallback(async () => {
    const count = await telemetrySQLiteBuffer.getUnsyncedCount();
    setUnsyncedCount(count);
  }, []);

  useEffect(() => {
    refreshBacklog();
    const interval = setInterval(refreshBacklog, 5000);
    return () => clearInterval(interval);
  }, [refreshBacklog]);

  const startTracking = async (duty: DutyState = 'AVAILABLE') => {
    if (!token) return;

    const perm = await requestLocationPermissions();
    if (!perm.foregroundGranted) {
      throw new Error('Location permission is required for fleet telemetry.');
    }

    const session = await telemetryApi.startSession(
      token,
      'device_install_main',
      '2.4.1',
      'Android'
    );

    setSessionId(session.id);
    if (session.vehicle_id) {
      setVehicleId(session.vehicle_id);
    }

    configureTelemetryContext(token, session.id, 'device_install_main', null);
    await startBackgroundLocationUpdates(duty);
    setIsTracking(true);
    setDutyState(duty);
  };

  const stopTracking = async () => {
    if (token && sessionId) {
      await telemetryApi.endSession(token, sessionId).catch(() => {});
    }
    await stopBackgroundLocationUpdates();
    setIsTracking(false);
    setSessionId(null);
  };

  return {
    sessionId,
    vehicleId,
    isTracking,
    dutyState,
    unsyncedCount,
    startTracking,
    stopTracking,
    refreshBacklog,
  };
}
