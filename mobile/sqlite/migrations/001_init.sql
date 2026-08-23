-- migration: 001_init
-- Canonical local schema mirroring the tables the app creates at runtime:
--   queued_pods / queued_gps / offline_expenses  -> offline_queue.db (src/services/offlineQueue.ts)
--   trips / offline_gps_logs                     -> avandab_offline.db (src/services/storage.ts)

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

-- down: DROP TABLE IF EXISTS offline_gps_logs;
-- down: DROP TABLE IF EXISTS trips;
-- down: DROP TABLE IF EXISTS offline_expenses;
-- down: DROP TABLE IF EXISTS queued_gps;
-- down: DROP TABLE IF EXISTS queued_pods;
