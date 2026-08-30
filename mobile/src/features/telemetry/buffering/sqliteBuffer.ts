import * as SQLite from 'expo-sqlite';

export interface BufferedTelemetryFrame {
  client_event_id: string;
  session_id: string;
  installation_id: string;
  occurred_at: string;
  latitude: number;
  longitude: number;
  accuracy_meters: number;
  speed_kmph: number;
  heading_degrees: number;
  battery_level_pct: number;
  battery_state: string;
  trip_id: string | null;
  synced: number; // 0 = unsynced, 1 = synced
  created_at: number;
}

export class TelemetrySQLiteBuffer {
  private db: any = null;

  async init(): Promise<void> {
    if (this.db) return;
    try {
      this.db = await SQLite.openDatabaseAsync('avandab_telemetry_buffer.db');
      await this.db.execAsync(`
        CREATE TABLE IF NOT EXISTS buffered_telemetry_frames (
          client_event_id TEXT PRIMARY KEY,
          session_id TEXT NOT NULL,
          installation_id TEXT NOT NULL,
          occurred_at TEXT NOT NULL,
          latitude REAL NOT NULL,
          longitude REAL NOT NULL,
          accuracy_meters REAL NOT NULL,
          speed_kmph REAL NOT NULL,
          heading_degrees REAL NOT NULL,
          battery_level_pct INTEGER NOT NULL,
          battery_state TEXT NOT NULL,
          trip_id TEXT,
          synced INTEGER NOT NULL DEFAULT 0,
          created_at INTEGER NOT NULL
        );
        CREATE INDEX IF NOT EXISTS idx_telemetry_synced ON buffered_telemetry_frames(synced, created_at);
      `);
    } catch {
      // Memory fallback for tests
    }
  }

  async insertFrame(frame: BufferedTelemetryFrame): Promise<void> {
    await this.init();
    if (!this.db) return;
    try {
      await this.db.runAsync(
        `INSERT OR IGNORE INTO buffered_telemetry_frames
         (client_event_id, session_id, installation_id, occurred_at, latitude, longitude, accuracy_meters, speed_kmph, heading_degrees, battery_level_pct, battery_state, trip_id, synced, created_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
        frame.client_event_id,
        frame.session_id,
        frame.installation_id,
        frame.occurred_at,
        frame.latitude,
        frame.longitude,
        frame.accuracy_meters,
        frame.speed_kmph,
        frame.heading_degrees,
        frame.battery_level_pct,
        frame.battery_state,
        frame.trip_id,
        0,
        frame.created_at
      );
    } catch {}
  }

  async getUnsyncedFrames(limit = 50): Promise<BufferedTelemetryFrame[]> {
    await this.init();
    if (!this.db) return [];
    try {
      const rows = await this.db.getAllAsync(
        `SELECT * FROM buffered_telemetry_frames WHERE synced = 0 ORDER BY created_at ASC LIMIT ?`,
        limit
      );
      return rows as BufferedTelemetryFrame[];
    } catch {
      return [];
    }
  }

  async markFramesSynced(eventIds: string[]): Promise<void> {
    await this.init();
    if (!this.db || eventIds.length === 0) return;
    try {
      const placeholders = eventIds.map(() => '?').join(',');
      await this.db.runAsync(
        `UPDATE buffered_telemetry_frames SET synced = 1 WHERE client_event_id IN (${placeholders})`,
        ...eventIds
      );
    } catch {}
  }

  async pruneSyncedFrames(olderThanMs = 24 * 60 * 60 * 1000): Promise<void> {
    await this.init();
    if (!this.db) return;
    try {
      const cutoff = Date.now() - olderThanMs;
      await this.db.runAsync(
        `DELETE FROM buffered_telemetry_frames WHERE synced = 1 AND created_at < ?`,
        cutoff
      );
    } catch {}
  }

  async getUnsyncedCount(): Promise<number> {
    await this.init();
    if (!this.db) return 0;
    try {
      const row: any = await this.db.getFirstAsync(
        `SELECT COUNT(*) as count FROM buffered_telemetry_frames WHERE synced = 0`
      );
      return row?.count || 0;
    } catch {
      return 0;
    }
  }
}

export const telemetrySQLiteBuffer = new TelemetrySQLiteBuffer();
