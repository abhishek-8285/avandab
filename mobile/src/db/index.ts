import * as SQLite from 'expo-sqlite';

const DB_FILE = 'avandab_offline.db';
let db: SQLite.SQLiteDatabase | null = null;
let initPromise: Promise<SQLite.SQLiteDatabase> | null = null;

async function init(): Promise<SQLite.SQLiteDatabase> {
  const d = await SQLite.openDatabaseAsync(DB_FILE);
  await d.execAsync(`PRAGMA journal_mode = WAL;`);
  // Single source of truth — all domain tables in one file, one Tx
  await d.execAsync(`
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
      idempotency_key TEXT,
      created_at TEXT NOT NULL DEFAULT (datetime('now'))
    );
    CREATE TABLE IF NOT EXISTS queued_pods (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      trip_id TEXT NOT NULL,
      consignee_name TEXT NOT NULL DEFAULT '',
      consignee_phone TEXT,
      notes TEXT NOT NULL DEFAULT '',
      photo_uri TEXT,
      latitude REAL,
      longitude REAL,
      pod_signature_data TEXT,
      quantity_short REAL DEFAULT 0,
      damage_qty REAL DEFAULT 0,
      refusal_reason TEXT,
      created_at TEXT NOT NULL DEFAULT (datetime('now'))
    );
    CREATE TABLE IF NOT EXISTS queued_gps (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      driver_id TEXT NOT NULL,
      latitude REAL NOT NULL,
      longitude REAL NOT NULL,
      timestamp TEXT NOT NULL,
      accuracy_m REAL,
      created_at TEXT NOT NULL DEFAULT (datetime('now'))
    );
    CREATE TABLE IF NOT EXISTS outbox (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      command TEXT NOT NULL,
      payload TEXT NOT NULL,
      idempotency_key TEXT NOT NULL UNIQUE,
      created_at TEXT NOT NULL DEFAULT (datetime('now')),
      attempts INTEGER NOT NULL DEFAULT 0
    );
  `);
  try {
    await d.execAsync(`ALTER TABLE offline_gps_logs ADD COLUMN accuracy REAL;`);
  } catch {}
  try {
    await d.execAsync(`ALTER TABLE queued_pods ADD COLUMN consignee_phone TEXT`);
  } catch {}
  try {
    await d.execAsync(`ALTER TABLE queued_pods ADD COLUMN pod_signature_data TEXT`);
  } catch {}
  try {
    await d.execAsync(`ALTER TABLE queued_pods ADD COLUMN quantity_short REAL DEFAULT 0`);
  } catch {}
  try {
    await d.execAsync(`ALTER TABLE queued_pods ADD COLUMN damage_qty REAL DEFAULT 0`);
  } catch {}
  try {
    await d.execAsync(`ALTER TABLE queued_pods ADD COLUMN refusal_reason TEXT`);
  } catch {}
  try {
    await d.execAsync(`ALTER TABLE offline_expenses ADD COLUMN idempotency_key TEXT`);
  } catch {}
  return d;
}

export async function getDB(): Promise<SQLite.SQLiteDatabase> {
  if (db) return db;
  if (!initPromise) {
    initPromise = init().then((d) => {
      db = d;
      return d;
    });
  }
  return initPromise;
}

export async function withTransaction<T>(fn: (tx: SQLite.SQLiteDatabase) => Promise<T>): Promise<T> {
  const d = await getDB();
  await d.execAsync('BEGIN IMMEDIATE;');
  try {
    const res = await fn(d);
    await d.execAsync('COMMIT;');
    return res;
  } catch (e) {
    try {
      await d.execAsync('ROLLBACK;');
    } catch {}
    throw e;
  }
}

// For tests: reset singleton
export function __resetDB() {
  db = null;
  initPromise = null;
}
