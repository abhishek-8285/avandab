import AsyncStorage from '@react-native-async-storage/async-storage';
import * as SQLite from 'expo-sqlite';
import { Trip } from '../types/api';
import { currentAccountId } from '../stores/authStore';

const KEYS = {
  OFFLINE_TRIPS: '@avandab_offline_trips',
  OFFLINE_EXPENSES: '@avandab_offline_expenses',
};

// ==========================================
// 1. Key-Value Storage (User Prefs)
// ==========================================
export const Storage = {
  async saveOfflineTrips(trips: Trip[]): Promise<void> {
    await AsyncStorage.setItem(KEYS.OFFLINE_TRIPS, JSON.stringify(trips));
  },

  async getOfflineTrips(): Promise<Trip[]> {
    const json = await AsyncStorage.getItem(KEYS.OFFLINE_TRIPS);
    return json ? JSON.parse(json) : [];
  },
};

// ==========================================
// 2. High-Performance SQLite (Structured Offline Data)
// ==========================================
let db: SQLite.SQLiteDatabase | null = null;

export interface OfflineExpense {
  id: number;
  trip_id: string;
  expense_type: string;
  amount: number;
  receipt_uri: string | null;
  notes: string;
  latitude: number | null;
  longitude: number | null;
  created_at: string;
}

export const initDatabase = async (): Promise<void> => {
  if (db) return;
  db = await SQLite.openDatabaseAsync('avandab_offline.db');

  await db.execAsync(`
    PRAGMA journal_mode = WAL;
    CREATE TABLE IF NOT EXISTS trips (
      id TEXT PRIMARY KEY NOT NULL,
      tripNumber TEXT NOT NULL,
      driverName TEXT NOT NULL,
      vehiclePlate TEXT NOT NULL,
      origin TEXT NOT NULL,
      destination TEXT NOT NULL,
      status TEXT NOT NULL,
      startTime TEXT NOT NULL,
      owner_id TEXT
    );

    CREATE TABLE IF NOT EXISTS offline_gps_logs (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      latitude REAL NOT NULL,
      longitude REAL NOT NULL,
      timestamp TEXT NOT NULL,
      accuracy REAL,
      speed REAL,
      heading REAL,
      motion INTEGER,
      battery_level REAL,
      synced INTEGER DEFAULT 0,
      owner_id TEXT
    );

    CREATE TABLE IF NOT EXISTS offline_expenses (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      trip_id TEXT NOT NULL,
      expense_type TEXT NOT NULL,
      amount REAL NOT NULL,
      receipt_uri TEXT,
      notes TEXT NOT NULL DEFAULT '',
      latitude REAL,
      longitude REAL,
      created_at TEXT NOT NULL DEFAULT (datetime('now')),
      owner_id TEXT
    );
  `);

  // Upgrade path for installs created before the accuracy column existed.
  try {
    await db.execAsync(`ALTER TABLE offline_gps_logs ADD COLUMN accuracy REAL;`);
  } catch {
    // Column already present — expected on every run after first upgrade.
  }
  // Upgrade path for installs predating the parity columns written by
  // logGPSLocation (speed/heading/motion/battery_level). Without these,
  // every GPS log throws "no such column" at runtime.
  for (const ddl of [
    `ALTER TABLE offline_gps_logs ADD COLUMN speed REAL;`,
    `ALTER TABLE offline_gps_logs ADD COLUMN heading REAL;`,
    `ALTER TABLE offline_gps_logs ADD COLUMN motion INTEGER;`,
    `ALTER TABLE offline_gps_logs ADD COLUMN battery_level REAL;`,
    `ALTER TABLE offline_gps_logs ADD COLUMN owner_id TEXT;`,
    // Stale flag: re-observed last-known fixes persist raw but flagged, so the
    // sync engine can publish them ONLY flagged stale (never as fresh).
    `ALTER TABLE offline_gps_logs ADD COLUMN is_stale INTEGER;`,
    `ALTER TABLE trips ADD COLUMN owner_id TEXT;`,
    `ALTER TABLE offline_expenses ADD COLUMN owner_id TEXT;`,
  ]) {
    try {
      await db.execAsync(ddl);
    } catch {
      // Column already present — expected on every run after first upgrade.
    }
  }
};

export const DB = {
  async saveTrips(trips: Trip[], ownerId?: string | null): Promise<void> {
    await initDatabase();
    if (!db) return;
    const owner = ownerId ?? currentAccountId();

    for (const trip of trips) {
      await db.runAsync(
        `INSERT OR REPLACE INTO trips (id, tripNumber, driverName, vehiclePlate, origin, destination, status, startTime, owner_id)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);`,
        [trip.id, trip.tripNumber, trip.driverName, trip.vehiclePlate, trip.origin, trip.destination, trip.status, trip.startTime, owner]
      );
    }
  },

  async getTrips(ownerId?: string | null): Promise<Trip[]> {
    await initDatabase();
    if (!db) return [];
    const owner = ownerId ?? currentAccountId();
    // No signed-in account (logged out / legacy callers): return everything so
    // offline fallback still works. When signed in: strict owner partition —
    // legacy NULL-owner rows stay retained but invisible until explicitly read.
    if (owner == null) {
      const rows = await db.getAllAsync<Trip>('SELECT * FROM trips ORDER BY startTime DESC;');
      return rows;
    }
    const rows = await db.getAllAsync<Trip>('SELECT * FROM trips WHERE owner_id = ? ORDER BY startTime DESC;', [owner]);
    return rows;
  },

  async logGPSLocation(
    lat: number,
    lng: number,
    accuracy?: number | null,
    extra?: { speed?: number | null; heading?: number | null; motion?: boolean | null; battery_level?: number | null; isStale?: boolean | null },
    ownerId?: string | null
  ): Promise<void> {
    await initDatabase();
    if (!db) return;
    const owner = ownerId ?? currentAccountId();

    await db.runAsync(
      'INSERT INTO offline_gps_logs (latitude, longitude, timestamp, accuracy, speed, heading, motion, battery_level, owner_id, is_stale) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);',
      [lat, lng, new Date().toISOString(), accuracy ?? null, extra?.speed ?? null, extra?.heading ?? null, extra?.motion == null ? null : (extra.motion ? 1 : 0), extra?.battery_level ?? null, owner, extra?.isStale == null ? null : (extra.isStale ? 1 : 0)]
    );
  },

  async getUnsyncedGPSLogs(ownerId?: string | null): Promise<{ id: number; latitude: number; longitude: number; timestamp: string; accuracy_m: number | null; speed: number | null; heading: number | null; motion: number | null; battery_level: number | null; is_stale: number | null }[]> {
    await initDatabase();
    if (!db) return [];
    const owner = ownerId ?? currentAccountId();
    if (owner == null) {
      const rows = await db.getAllAsync<{ id: number; latitude: number; longitude: number; timestamp: string; accuracy_m: number | null; speed: number | null; heading: number | null; motion: number | null; battery_level: number | null; is_stale: number | null }>(
        `SELECT id, latitude, longitude, timestamp, accuracy AS accuracy_m, speed, heading, motion, battery_level, is_stale FROM offline_gps_logs WHERE synced = 0 ORDER BY id ASC LIMIT 50;`
      );
      return rows;
    }
    const rows = await db.getAllAsync<{ id: number; latitude: number; longitude: number; timestamp: string; accuracy_m: number | null; speed: number | null; heading: number | null; motion: number | null; battery_level: number | null; is_stale: number | null }>(
      `SELECT id, latitude, longitude, timestamp, accuracy AS accuracy_m, speed, heading, motion, battery_level, is_stale FROM offline_gps_logs WHERE synced = 0 AND owner_id = ? ORDER BY id ASC LIMIT 50;`,
      [owner]
    );
    return rows;
  },

  // Owner-aware last-persisted-fix lookup for the stale tier of the no-fix
  // chain (telemetry serves it flagged stale; never re-inserted).
  async getLastGPSLog(ownerId?: string | null): Promise<{ id: number; latitude: number; longitude: number; timestamp: string; accuracy: number | null; speed: number | null; heading: number | null; motion: number | null; battery_level: number | null; is_stale: number | null } | null> {
    await initDatabase();
    if (!db) return null;
    const owner = ownerId ?? currentAccountId();
    if (owner == null) {
      return await db.getFirstAsync<{ id: number; latitude: number; longitude: number; timestamp: string; accuracy: number | null; speed: number | null; heading: number | null; motion: number | null; battery_level: number | null; is_stale: number | null }>(
        `SELECT id, latitude, longitude, timestamp, accuracy, speed, heading, motion, battery_level, is_stale FROM offline_gps_logs ORDER BY id DESC LIMIT 1;`
      );
    }
    return await db.getFirstAsync<{ id: number; latitude: number; longitude: number; timestamp: string; accuracy: number | null; speed: number | null; heading: number | null; motion: number | null; battery_level: number | null; is_stale: number | null }>(
      `SELECT id, latitude, longitude, timestamp, accuracy, speed, heading, motion, battery_level, is_stale FROM offline_gps_logs WHERE owner_id = ? ORDER BY id DESC LIMIT 1;`,
      [owner]
    );
  },

  async markLogsAsSynced(ids: number[]): Promise<void> {
    if (!ids || ids.length === 0) return;
    await initDatabase();
    if (!db) return;

    const placeholders = ids.map(() => '?').join(',');
    await db.runAsync(
      `UPDATE offline_gps_logs SET synced = 1 WHERE id IN (${placeholders});`,
      ids
    );
  },

  // ── Offline Expenses cache (mirrors offlineQueue.offline_expenses) ──
  async saveOfflineExpense(expense: {
    trip_id: string;
    expense_type: string;
    amount: number;
    receipt_uri?: string | null;
    notes?: string;
    latitude?: number | null;
    longitude?: number | null;
  }, ownerId?: string | null): Promise<void> {
    await initDatabase();
    if (!db) return;
    const owner = ownerId ?? currentAccountId();
    await db.runAsync(
      `INSERT INTO offline_expenses (trip_id, expense_type, amount, receipt_uri, notes, latitude, longitude, owner_id)
       VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
      [
        expense.trip_id,
        expense.expense_type,
        expense.amount,
        expense.receipt_uri || null,
        expense.notes || '',
        expense.latitude ?? null,
        expense.longitude ?? null,
        owner,
      ]
    );
  },

  async getOfflineExpenses(ownerId?: string | null): Promise<OfflineExpense[]> {
    await initDatabase();
    if (!db) return [];
    const owner = ownerId ?? currentAccountId();
    if (owner == null) return await db.getAllAsync<OfflineExpense>('SELECT * FROM offline_expenses ORDER BY created_at ASC');
    return await db.getAllAsync<OfflineExpense>('SELECT * FROM offline_expenses WHERE owner_id = ? ORDER BY created_at ASC', [owner]);
  },

  async getPendingOfflineExpenses(ownerId?: string | null): Promise<OfflineExpense[]> {
    await initDatabase();
    if (!db) return [];
    const owner = ownerId ?? currentAccountId();
    if (owner == null) return await db.getAllAsync<OfflineExpense>('SELECT * FROM offline_expenses ORDER BY created_at ASC');
    return await db.getAllAsync<OfflineExpense>('SELECT * FROM offline_expenses WHERE owner_id = ? ORDER BY created_at ASC', [owner]);
  },

  async clearOfflineExpense(id: number): Promise<void> {
    await initDatabase();
    if (!db) return;
    await db.runAsync('DELETE FROM offline_expenses WHERE id = ?', [id]);
  },

  async clearOfflineExpenses(ids: number[]): Promise<void> {
    if (!ids || ids.length === 0) return;
    await initDatabase();
    if (!db) return;
    const placeholders = ids.map(() => '?').join(',');
    await db.runAsync(`DELETE FROM offline_expenses WHERE id IN (${placeholders})`, ids);
  },
};
