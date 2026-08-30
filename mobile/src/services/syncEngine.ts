import NetInfo from '@react-native-community/netinfo';
import { getApiBaseURL } from '../constants/network';
import { DB } from './storage';
import { OfflineQueue } from './offlineQueue';
import { sosService } from './sosService';
import { useAuthStore } from '../stores/authStore';
import { applyNetInfoState, useSyncStore } from '../stores/syncStore';

let wasConnected = true;
let unsubscribeNetInfo: (() => void) | null = null;

export function startNetworkWatcher(): void {
  if (unsubscribeNetInfo) return;

  unsubscribeNetInfo = NetInfo.addEventListener((state) => {
    const isConnected = state.isConnected ?? false;

    try {
      // Offline flips the bar to offline_saved; online leaves status untouched
      applyNetInfoState(isConnected);
    } catch {
      // status updates must never break the watcher
    }

    if (isConnected && !wasConnected) {
      // 1. Priority: Immediately flush any pending emergency SOS commands
      sosService.retryPendingSOS().catch((err) => {
        console.warn('[SOS] Immediate reconnect retry failed:', err);
      });

      // 2. Flush offline queues in batch (pods/expenses/gps)
      OfflineQueue.flush()
        .then(({ podsFlushed, gpsFlushed, expensesFlushed }) => {
          if (podsFlushed > 0 || gpsFlushed > 0 || (expensesFlushed ?? 0) > 0) {
            console.log(`[OfflineQueue] Flushed ${podsFlushed} PODs, ${expensesFlushed ?? 0} Expenses, ${gpsFlushed} GPS logs on reconnect`);
          }
        })
        .catch((err) => {
          console.warn('[OfflineQueue] Flush failed:', err);
        });

      const driverId = useAuthStore.getState().user?.driverId || useAuthStore.getState().user?.id;
      if (driverId) {
        SyncEngine.syncPendingLogs(driverId).catch(() => {});
        // Also flush offlineQueue via sync engine batch
        SyncEngine.flushOfflineQueues().catch(() => {});
      }
    }

    wasConnected = isConnected;
  });
}

export function stopNetworkWatcher(): void {
  if (unsubscribeNetInfo) {
    unsubscribeNetInfo();
    unsubscribeNetInfo = null;
  }
}

const OFFLINE_FLUSH_BATCH = 20;

class SyncEngineService {
  private syncTimer: ReturnType<typeof setInterval> | null = null;
  private isSyncing = false;

  startAutoSync(driverId: string, intervalMs = 15000): void {
    if (this.syncTimer) return;

    this.syncTimer = setInterval(() => {
      this.syncPendingLogs(driverId);
      this.flushOfflineQueues();
    }, intervalMs);

    console.log(`[SYNC ENGINE] Auto-sync background service started (${intervalMs / 1000}s interval)`);
  }

  stopAutoSync(): void {
    if (this.syncTimer) {
      clearInterval(this.syncTimer);
      this.syncTimer = null;
    }
  }

  /**
   * Flush OfflineQueue pods/expenses/gps in batch with continue-on-failure semantics.
   * Each item is retried independently — one failure does not block the rest.
   */
  async flushOfflineQueues(): Promise<{ podsFlushed: number; expensesFlushed: number; gpsFlushed: number }> {
    let result = { podsFlushed: 0, expensesFlushed: 0, gpsFlushed: 0 };
    try {
      const prevStatus = useSyncStore.getState().status;
      try {
        useSyncStore.getState().setStatus('syncing');
      } catch {
        // status bar is best-effort
      }
      result = await OfflineQueue.flush();
      const flushedTotal = result.podsFlushed + result.expensesFlushed + result.gpsFlushed;
      if (flushedTotal > 0) {
        try {
          useSyncStore.getState().markSynced();
        } catch {
          // status bar is best-effort
        }
      } else {
        // Nothing flushed — don't leave the bar stuck on 'syncing'
        try {
          useSyncStore.getState().setStatus(prevStatus);
        } catch {
          // status bar is best-effort
        }
      }
    } catch {
      try {
        useSyncStore.getState().setStatus('error');
      } catch {
        // status bar is best-effort
      }
    }

    // Refresh pending badge count after every flush attempt (all paths benefit)
    try {
      const [pods, expenses, gps] = await Promise.all([
        OfflineQueue.pendingPODs(),
        OfflineQueue.pendingExpenses(),
        OfflineQueue.pendingGPS(),
      ]);
      useSyncStore.getState().setPendingCount(pods.length + expenses.length + gps.length);
    } catch {
      // pending count is best-effort
    }
    return result;
  }

  async syncPendingLogs(driverId: string): Promise<{ syncedCount: number; error: string | null }> {
    if (this.isSyncing) return { syncedCount: 0, error: 'Sync already in progress' };

    this.isSyncing = true;
    try {
      const unsyncedLogs = await DB.getUnsyncedGPSLogs();
      if (!unsyncedLogs || unsyncedLogs.length === 0) {
        // Even if no DB gps logs, try to flush OfflineQueue batch
        await this.flushOfflineQueues();
        this.isSyncing = false;
        return { syncedCount: 0, error: null };
      }

      const syncEndpoint = `${getApiBaseURL()}/api/v1/telemetry/sync`;
      const token = useAuthStore.getState().token;

      // Batch flush in chunks of OFFLINE_FLUSH_BATCH with continue on partial failure
      let totalSynced = 0;
      let lastError: string | null = null;

      for (let i = 0; i < unsyncedLogs.length; i += OFFLINE_FLUSH_BATCH) {
        const batch = unsyncedLogs.slice(i, i + OFFLINE_FLUSH_BATCH);
        try {
          const response = await fetch(syncEndpoint, {
            method: 'POST',
            headers: {
              'Content-Type': 'application/json',
              ...(token ? { Authorization: `Bearer ${token}` } : {}),
            },
            body: JSON.stringify({
              driver_id: driverId,
              logs: batch.map((b: any) => ({
                id: b.id, // echoed in synced_ids so the row gets marked synced
                latitude: b.latitude,
                longitude: b.longitude,
                timestamp: b.timestamp,
                ...(b.accuracy_m != null ? { accuracy_m: b.accuracy_m } : {}),
              })),
            }),
          });

          if (!response.ok) {
            lastError = `Server returned HTTP ${response.status}`;
            continue;
          }

          const result = await response.json();
          if (result.success && Array.isArray(result.synced_ids)) {
            await DB.markLogsAsSynced(result.synced_ids);
            console.log(`[SYNC ENGINE SUCCESS] Synced & marked ${result.synced_ids.length} records in SQLite DB`);
            totalSynced += result.synced_ids.length;
          } else if (result.synced_ids) {
            await DB.markLogsAsSynced(result.synced_ids);
            totalSynced += result.synced_ids.length;
          } else {
            // No synced_ids but success true — mark whole batch
            if (result.success) {
              await DB.markLogsAsSynced(batch.map((b: any) => b.id));
              totalSynced += batch.length;
            } else {
              lastError = 'Unexpected server response';
              continue;
            }
          }
        } catch (err: any) {
          lastError = err.message;
          continue;
        }
      }

      // After DB GPS, also flush OfflineQueue pods/expenses/gps batch
      try {
        await this.flushOfflineQueues();
      } catch {
        // continue even if flush fails
      }

      this.isSyncing = false;
      if (totalSynced > 0) {
        return { syncedCount: totalSynced, error: null };
      }
      return { syncedCount: 0, error: lastError };
    } catch (err: any) {
      this.isSyncing = false;
      console.log('[SYNC ENGINE WARNING] Auto-sync failed (re-queueing for offline retention):', err.message);
      return { syncedCount: 0, error: err.message };
    }
  }
}

export const SyncEngine = new SyncEngineService();
