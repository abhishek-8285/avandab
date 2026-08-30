import { telemetryApi } from '../api/telemetryApi';
import { telemetrySQLiteBuffer, BufferedTelemetryFrame } from '../buffering/sqliteBuffer';

export class TelemetrySyncer {
  private isSyncing = false;

  async flush(token: string): Promise<{ syncedCount: number; errors: number }> {
    if (this.isSyncing || !token) {
      return { syncedCount: 0, errors: 0 };
    }

    this.isSyncing = true;
    let syncedCount = 0;
    let errors = 0;

    try {
      const pending = await telemetrySQLiteBuffer.getUnsyncedFrames(30);
      if (pending.length === 0) {
        return { syncedCount: 0, errors: 0 };
      }

      const syncedIds: string[] = [];

      for (const frame of pending) {
        try {
          const resp = await telemetryApi.sendTelemetryEvent(token, frame);
          if (resp.status === 'ACCEPTED' || resp.status === 'DUPLICATE') {
            syncedIds.push(frame.client_event_id);
            syncedCount++;
          } else if (resp.status === 'STALE' || resp.status === 'INVALID_COORDINATES') {
            // Terminal frame error: mark synced so it doesn't block queue
            syncedIds.push(frame.client_event_id);
          } else if (resp.status === 'UNAUTHORIZED') {
            errors++;
            break; // Stop flushing until re-authenticated
          }
        } catch (err) {
          errors++;
          break; // Network interrupted
        }
      }

      if (syncedIds.length > 0) {
        await telemetrySQLiteBuffer.markFramesSynced(syncedIds);
      }
    } finally {
      this.isSyncing = false;
    }

    return { syncedCount, errors };
  }
}

export const telemetrySyncer = new TelemetrySyncer();
