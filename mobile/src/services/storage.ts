import AsyncStorage from '@react-native-async-storage/async-storage';
import * as SQLite from 'expo-sqlite';
import { Trip } from '../types/api';

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
      startTime TEXT NOT NULL
    );

    CREATE TABLE IF NOT EXISTS offline_gps_logs (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      latitude REAL NOT NULL,
      longitude REAL NOT NULL,
      timestamp TEXT NOT NULL,
      accuracy REAL,
      synced INTEGER DEFAULT 0
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
      created_at TEXT NOT NULL DEFAULT (datetime('now'))
    );
  `);

  // Upgrade path for installs created before the accuracy column existed.
  try {
    await db.execAsync(`ALTER TABLE offline_gps_logs ADD COLUMN accuracy REAL;`);
  } catch {
    // Column already present — expected on every run after first upgrade.
  }
};

export const DB = {
  async saveTrips(trips: Trip[]): Promise<void> {
    await initDatabase();
    if (!db) return;

    for (const trip of trips) {
      await db.runAsync(
        `INSERT OR REPLACE INTO trips (id, tripNumber, driverName, vehiclePlate, origin, destination, status, startTime)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?);`,
        [trip.id, trip.tripNumber, trip.driverName, trip.vehiclePlate, trip.origin, trip.destination, trip.status, trip.startTime]
      );
    }
  },

  async getTrips(): Promise<Trip[]> {
    await initDatabase();
    if (!db) return [];

    const rows = await db.getAllAsync<Trip>('SELECT * FROM trips ORDER BY startTime DESC;');
    return rows;
  },

  async logGPSLocation(
    lat: number,
    lng: number,
    accuracy?: number | null,
    extra?: { speed?: number | null; heading?: number | null; motion?: boolean | null; battery_level?: number | null }
  ): Promise<void> {
    await initDatabase();
    if (!db) return;

    await db.runAsync(
      'INSERT INTO offline_gps_logs (latitude, longitude, timestamp, accuracy, speed, heading, motion, battery_level) VALUES (?, ?, ?, ?, ?, ?, ?, ?);',
      [lat, lng, new Date().toISOString(), accuracy ?? null, extra?.speed ?? null, extra?.heading ?? null, extra?.motion == null ? null : (extra.motion ? 1 : 0), extra?.battery_level ?? null]
    );
  },

  async getUnsyncedGPSLogs(): Promise<{ id: number; latitude: number; longitude: number; timestamp: string; accuracy_m: number | null; speed: number | null; heading: number | null; motion: number | null; battery_level: number | null }[]> {
    await initDatabase();
    if (!db) return [];

    const rows = await db.getAllAsync<{ id: number; latitude: number; longitude: number; timestamp: string; accuracy_m: number | null; speed: number | null; heading: number | null; motion: number | null; battery_level: number | null }>(
      `SELECT id, latitude, longitude, timestamp, accuracy AS accuracy_m, speed, heading, motion, battery_level FROM offline_gps_logs WHERE synced = 0 ORDER BY id ASC LIMIT 50;`
    );
    return rows;
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
  }): Promise<void> {
    await initDatabase();
    if (!db) return;
    await db.runAsync(
      `INSERT INTO offline_expenses (trip_id, expense_type, amount, receipt_uri, notes, latitude, longitude)
       VALUES (?, ?, ?, ?, ?, ?, ?)`,
      [
        expense.trip_id,
        expense.expense_type,
        expense.amount,
        expense.receipt_uri || null,
        expense.notes || '',
        expense.latitude ?? null,
        expense.longitude ?? null,
      ]
    );
  },

  async getOfflineExpenses(): Promise<OfflineExpense[]> {
    await initDatabase();
    if (!db) return [];
    return await db.getAllAsync<OfflineExpense>('SELECT * FROM offline_expenses ORDER BY created_at ASC');
  },

  async getPendingOfflineExpenses(): Promise<OfflineExpense[]> {
    await initDatabase();
    if (!db) return [];
    return await db.getAllAsync<OfflineExpense>('SELECT * FROM offline_expenses ORDER BY created_at ASC');
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
