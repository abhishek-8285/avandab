import * as SQLite from 'expo-sqlite';
import { getApiBaseURL } from '../constants/network';
import { useAuthStore } from '../stores/authStore';

const DB_NAME = 'offline_queue.db';

export interface QueuedPOD {
  id: number;
  trip_id: string;
  stop_id?: string | null;
  stop_sequence?: number | null;
  otp?: string | null;
  consignee_name: string;
  consignee_phone: string | null;
  notes: string;
  photo_uri: string | null;
  latitude: number | null;
  longitude: number | null;
  pod_signature_data: string | null;
  quantity_short: number | null;
  damage_qty: number | null;
  refusal_reason: string | null;
  created_at: string;
}

export interface QueuedExpense {
  id: number;
  trip_id: string;
  expense_type: string;
  amount: number;
  receipt_uri: string | null;
  notes: string;
  latitude: number | null;
  longitude: number | null;
  idempotency_key: string | null;
  created_at: string;
}

export interface QueuedGPS {
  id: number;
  driver_id: string;
  latitude: number;
  longitude: number;
  timestamp: string;
  accuracy_m: number | null;
  created_at: string;
}

const OFFLINE_FLUSH_BATCH = 20;

class OfflineQueueService {
  private db: SQLite.SQLiteDatabase | null = null;

  async init(): Promise<void> {
    if (this.db) return;
    this.db = await SQLite.openDatabaseAsync(DB_NAME);
    await this.db.execAsync(`
      CREATE TABLE IF NOT EXISTS queued_pods (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        trip_id TEXT NOT NULL,
        stop_id TEXT,
        stop_sequence INTEGER,
        otp TEXT,
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
    `);
    // Upgrade path for existing databases missing new columns
    try {
      await this.db.execAsync(`ALTER TABLE queued_pods ADD COLUMN stop_id TEXT`);
    } catch {}
    try {
      await this.db.execAsync(`ALTER TABLE queued_pods ADD COLUMN stop_sequence INTEGER`);
    } catch {}
    try {
      await this.db.execAsync(`ALTER TABLE queued_pods ADD COLUMN otp TEXT`);
    } catch {}
    try {
      await this.db.execAsync(`ALTER TABLE queued_pods ADD COLUMN consignee_phone TEXT`);
    } catch {}
    try {
      await this.db.execAsync(`ALTER TABLE queued_pods ADD COLUMN pod_signature_data TEXT`);
    } catch {}
    try {
      await this.db.execAsync(`ALTER TABLE queued_pods ADD COLUMN quantity_short REAL DEFAULT 0`);
    } catch {}
    try {
      await this.db.execAsync(`ALTER TABLE queued_pods ADD COLUMN damage_qty REAL DEFAULT 0`);
    } catch {}
    try {
      await this.db.execAsync(`ALTER TABLE queued_pods ADD COLUMN refusal_reason TEXT`);
    } catch {}
    try {
      await this.db.execAsync(`ALTER TABLE offline_expenses ADD COLUMN idempotency_key TEXT`);
    } catch {}
    // Expire pods older than 7 days
    try {
      await this.db.execAsync(`DELETE FROM queued_pods WHERE created_at < datetime('now','-7 days')`);
      await this.db.execAsync(`DELETE FROM offline_expenses WHERE created_at < datetime('now','-7 days')`);
    } catch {}
  }

  // ── POD queue ──────────────────────────────────────────
  async enqueuePOD(
    tripId: string,
    data: {
      stop_id?: string | null;
      stop_sequence?: number | null;
      otp?: string | null;
      consignee_name: string;
      consignee_phone?: string | null;
      notes?: string;
      photo_uri?: string | null;
      latitude?: number | null;
      longitude?: number | null;
      pod_signature_data?: string | null;
      quantity_short?: number | null;
      damage_qty?: number | null;
      refusal_reason?: string | null;
    }
  ): Promise<void> {
    if (!this.db) await this.init();
    // Dedupe: don't queue twice for the same trip and stop
    const existing = await this.db!.getFirstAsync<QueuedPOD>(
      'SELECT id FROM queued_pods WHERE trip_id = ? AND ((stop_id IS NULL AND ? IS NULL) OR stop_id = ?)',
      [tripId, data.stop_id ?? null, data.stop_id ?? null]
    );
    if (existing) return;

    await this.db!.runAsync(
      `INSERT INTO queued_pods (trip_id, stop_id, stop_sequence, otp, consignee_name, consignee_phone, notes, photo_uri, latitude, longitude, pod_signature_data, quantity_short, damage_qty, refusal_reason)
       VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
      [
        tripId,
        data.stop_id ?? null,
        data.stop_sequence ?? null,
        data.otp ?? null,
        data.consignee_name,
        data.consignee_phone ?? null,
        data.notes || '',
        data.photo_uri || null,
        data.latitude ?? null,
        data.longitude ?? null,
        data.pod_signature_data ?? null,
        data.quantity_short ?? null,
        data.damage_qty ?? null,
        data.refusal_reason ?? null,
      ]
    );
  }

  async clearPOD(tripId: string, stopId?: string): Promise<void> {
    if (!this.db) await this.init();
    if (stopId) {
      await this.db!.runAsync('DELETE FROM queued_pods WHERE trip_id = ? AND (stop_id = ? OR stop_id IS NULL)', [tripId, stopId]);
    } else {
      await this.db!.runAsync('DELETE FROM queued_pods WHERE trip_id = ?', [tripId]);
    }
  }

  async pendingPODs(): Promise<QueuedPOD[]> {
    if (!this.db) await this.init();
    return await this.db!.getAllAsync<QueuedPOD>('SELECT * FROM queued_pods ORDER BY created_at ASC');
  }

  // ── Expense queue ───────────────────────────────────────
  async enqueueExpense(data: {
    trip_id: string;
    expense_type: string;
    amount: number;
    receipt_uri?: string | null;
    notes?: string;
    latitude?: number | null;
    longitude?: number | null;
    idempotency_key?: string | null;
  }): Promise<void> {
    if (!this.db) await this.init();
    await this.db!.runAsync(
      `INSERT INTO offline_expenses (trip_id, expense_type, amount, receipt_uri, notes, latitude, longitude, idempotency_key)
       VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
      [
        data.trip_id,
        data.expense_type,
        data.amount,
        data.receipt_uri || null,
        data.notes || '',
        data.latitude ?? null,
        data.longitude ?? null,
        data.idempotency_key || null,
      ]
    );
  }

  async pendingExpenses(): Promise<QueuedExpense[]> {
    if (!this.db) await this.init();
    return await this.db!.getAllAsync<QueuedExpense>('SELECT * FROM offline_expenses ORDER BY created_at ASC');
  }

  async clearExpense(id: number): Promise<void> {
    if (!this.db) await this.init();
    await this.db!.runAsync('DELETE FROM offline_expenses WHERE id = ?', [id]);
  }

  async clearExpenses(ids: number[]): Promise<void> {
    if (!this.db) await this.init();
    if (ids.length === 0) return;
    const placeholders = ids.map(() => '?').join(',');
    await this.db!.runAsync(`DELETE FROM offline_expenses WHERE id IN (${placeholders})`, ids);
  }

  // ── GPS queue ──────────────────────────────────────────
  async enqueueGPS(log: {
    driver_id: string;
    latitude: number;
    longitude: number;
    timestamp: string;
    accuracy_m?: number | null;
  }): Promise<void> {
    if (!this.db) await this.init();
    await this.db!.runAsync(
      `INSERT INTO queued_gps (driver_id, latitude, longitude, timestamp, accuracy_m)
       VALUES (?, ?, ?, ?, ?)`,
      [log.driver_id, log.latitude, log.longitude, log.timestamp, log.accuracy_m ?? null]
    );
  }

  async pendingGPS(): Promise<QueuedGPS[]> {
    if (!this.db) await this.init();
    return await this.db!.getAllAsync<QueuedGPS>('SELECT * FROM queued_gps ORDER BY created_at ASC');
  }

  async clearGPS(ids: number[]): Promise<void> {
    if (!this.db) await this.init();
    if (ids.length === 0) return;
    const placeholders = ids.map(() => '?').join(',');
    await this.db!.runAsync(`DELETE FROM queued_gps WHERE id IN (${placeholders})`, ids);
  }

  // ── Flush all queues (batch) ───────────────────────────────────
  async flush(): Promise<{ podsFlushed: number; gpsFlushed: number; expensesFlushed: number }> {
    let podsFlushed = 0;
    let gpsFlushed = 0;
    let expensesFlushed = 0;

    // Flush queued PODs in batch, continue on partial failure
    const pods = await this.pendingPODs();
    const podsBatch = pods.slice(0, OFFLINE_FLUSH_BATCH);
    for (const pod of podsBatch) {
      try {
        const token = useAuthStore.getState().token;
        if (!token) continue;

        const form = new FormData();
        form.append('consignee_name', pod.consignee_name);
        if (pod.consignee_phone) {
          form.append('consignee_phone', pod.consignee_phone);
        }
        form.append('notes', pod.notes);
        if (pod.photo_uri) {
          form.append('pod_photo', {
            uri: pod.photo_uri,
            name: 'pod.jpg',
            type: 'image/jpeg',
          } as any);
        }
        if (pod.latitude != null && pod.longitude != null) {
          form.append('latitude', String(pod.latitude));
          form.append('longitude', String(pod.longitude));
        }
        if (pod.pod_signature_data) {
          form.append('pod_signature_data', pod.pod_signature_data);
          form.append('signature_dataurl', pod.pod_signature_data);
        }
        if (pod.quantity_short != null) {
          form.append('quantity_short', String(pod.quantity_short));
        }
        if (pod.damage_qty != null) {
          form.append('damage_qty', String(pod.damage_qty));
        }
        if (pod.refusal_reason) {
          form.append('refusal_reason', pod.refusal_reason);
        }
        if (pod.otp) {
          form.append('otp', pod.otp);
        }
        if (pod.photo_uri) {
          form.append('pod_url', pod.photo_uri);
        }
        if (pod.pod_signature_data) {
          form.append('signature_url', pod.pod_signature_data);
        }

        const url = pod.stop_id
          ? `${getApiBaseURL()}/trips/${pod.trip_id}/stops/${pod.stop_id}/pod`
          : `${getApiBaseURL()}/api/v1/trips/${pod.trip_id}/deliver-pod`;

        const res = await fetch(url, {
          method: 'POST',
          headers: { Authorization: `Bearer ${token}` },
          body: form,
        });

        if (res.ok) {
          await this.clearPOD(pod.trip_id, pod.stop_id || undefined);
          podsFlushed++;
        } else {
          // Server rejected this POD — log and continue to next
          continue;
        }
      } catch {
        continue;
      }
    }

    // Flush queued Expenses in batch, continue on partial failure
    const expenses = await this.pendingExpenses();
    const expensesBatch = expenses.slice(0, OFFLINE_FLUSH_BATCH);
    for (const exp of expensesBatch) {
      try {
        const token = useAuthStore.getState().token;
        if (!token) continue;

        const form = new FormData();
        form.append('trip_id', exp.trip_id);
        form.append('type', exp.expense_type);
        form.append('expense_type', exp.expense_type);
        form.append('amount', String(exp.amount));
        form.append('notes', exp.notes || '');
        if (exp.receipt_uri) {
          form.append('receipt_photo', {
            uri: exp.receipt_uri,
            name: 'receipt.jpg',
            type: 'image/jpeg',
          } as any);
        }
        if (exp.latitude != null && exp.longitude != null) {
          form.append('latitude', String(exp.latitude));
          form.append('longitude', String(exp.longitude));
        }
        // Backend dedupes on this key (unique index) — safe across retries
        if (exp.idempotency_key) {
          form.append('idempotency_key', exp.idempotency_key);
        }

        const res = await fetch(`${getApiBaseURL()}/api/v1/kharcha/expense`, {
          method: 'POST',
          headers: { Authorization: `Bearer ${token}` },
          body: form,
        });

        if (res.ok) {
          await this.clearExpense(exp.id);
          expensesFlushed++;
        } else {
          continue;
        }
      } catch {
        continue;
      }
    }

    // Flush queued GPS in batch
    const gpsLogs = await this.pendingGPS();
    const gpsBatch = gpsLogs.slice(0, OFFLINE_FLUSH_BATCH);
    if (gpsBatch.length > 0) {
      try {
        const token = useAuthStore.getState().token;
        const driverId = useAuthStore.getState().user?.driverId || useAuthStore.getState().user?.id;
        if (token && driverId) {
          const res = await fetch(`${getApiBaseURL()}/api/v1/telemetry/sync`, {
            method: 'POST',
            headers: {
              Authorization: `Bearer ${token}`,
              'Content-Type': 'application/json',
            },
            body: JSON.stringify({
              driver_id: driverId,
              logs: gpsBatch.map((g) => ({
                latitude: g.latitude,
                longitude: g.longitude,
                timestamp: g.timestamp,
                accuracy_m: g.accuracy_m,
              })),
            }),
          });
          if (res.ok) {
            const json = await res.json();
            if (json.success) {
              await this.clearGPS(gpsBatch.map((g) => g.id));
              gpsFlushed = gpsBatch.length;
            } else if (Array.isArray(json.synced_ids)) {
              await this.clearGPS(json.synced_ids);
              gpsFlushed = json.synced_ids.length;
            }
          }
        }
      } catch {
        // Network still down — continue, gps remains queued
      }
    }

    return { podsFlushed, gpsFlushed, expensesFlushed };
  }
}

export const OfflineQueue = new OfflineQueueService();
