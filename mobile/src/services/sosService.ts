import { commandQueue, QueuedCommand } from '../core/sync/commandQueue';
import { generateCommandId } from '../core/sync/idempotency';
import { getApiBaseURL } from '../constants/network';
import { useAuthStore } from '../stores/authStore';

export interface SOSPayload {
  command_id: string;
  trip_id?: string;
  vehicle_id?: string;
  latitude: number;
  longitude: number;
  accuracy?: number;
  battery_level?: number;
  reason?: string;
  occurred_at: string;
}

export interface SOSTriggerResult {
  success: boolean;
  queued: boolean;
  commandId: string;
  sosId?: string;
  message: string;
  error?: string;
}

export class SOSService {
  /**
   * Triggers an emergency SOS.
   * Immediately persists the command locally in the durable queue (surviving app kills/restarts),
   * then attempts immediate transmission to the backend.
   */
  async triggerSOS(opts: {
    tripId?: string;
    vehicleId?: string;
    latitude: number;
    longitude: number;
    accuracy?: number;
    batteryLevel?: number;
    reason?: string;
    tokenOverride?: string;
  }): Promise<SOSTriggerResult> {
    const occurredAt = new Date().toISOString();
    const commandId = generateCommandId('sos');

    const payload: SOSPayload = {
      command_id: commandId,
      trip_id: opts.tripId,
      vehicle_id: opts.vehicleId,
      latitude: opts.latitude,
      longitude: opts.longitude,
      accuracy: opts.accuracy,
      battery_level: opts.batteryLevel,
      reason: opts.reason || 'Emergency SOS from Driver App',
      occurred_at: occurredAt,
    };

    // 1. Immediately persist locally in the durable CommandQueue
    const queuedCmd = await commandQueue.enqueueCommand('SOS', payload, commandId);
    const finalCommandId = queuedCmd.commandId;

    const token = opts.tokenOverride ?? useAuthStore.getState().token;

    // 2. Attempt immediate network transmission
    try {
      await commandQueue.updateCommandState(finalCommandId, 'SYNCING');

      const headers: Record<string, string> = {
        'Content-Type': 'application/json',
      };
      if (token) {
        headers['Authorization'] = `Bearer ${token}`;
      }

      const res = await fetch(`${getApiBaseURL()}/api/v1/sos`, {
        method: 'POST',
        headers,
        body: JSON.stringify(payload),
      });

      if (!res.ok) {
        if (res.status === 401 || res.status === 403) {
          // Authentication error — fail closed to prevent endless retry storm
          await commandQueue.updateCommandState(finalCommandId, 'FAILED', {
            errorMessage: `Auth failed (${res.status}): Please re-login`,
          });
          return {
            success: false,
            queued: false,
            commandId: finalCommandId,
            error: 'Authentication failed',
            message: 'Authentication failed. Please re-login to deliver SOS.',
          };
        }

        const errData = await res.json().catch(() => ({ error: res.statusText }));
        await commandQueue.updateCommandState(finalCommandId, 'PENDING', {
          errorMessage: errData.message || errData.error || `HTTP ${res.status}`,
        });
        return {
          success: true,
          queued: true,
          commandId: finalCommandId,
          message: 'SOS recorded locally. Will retry when connection improves.',
        };
      }

      const data = await res.json();
      await commandQueue.updateCommandState(finalCommandId, 'SYNCED', { response: data });

      return {
        success: true,
        queued: false,
        commandId: finalCommandId,
        sosId: data.sos_id,
        message: data.message || 'Emergency response team has been alerted.',
      };
    } catch (err: any) {
      // Network failure / Offline -> command is already safely queued in PENDING state
      await commandQueue.updateCommandState(finalCommandId, 'PENDING', {
        errorMessage: err.message || 'Network unreachable',
      });
      return {
        success: true,
        queued: true,
        commandId: finalCommandId,
        message: 'Network offline. SOS saved locally and will transmit automatically.',
      };
    }
  }

  /**
   * Flushes all pending SOS commands in the queue.
   * Called on network restoration or app launch.
   */
  async retryPendingSOS(tokenOverride?: string): Promise<{ synced: number; failed: number; pending: number }> {
    const token = tokenOverride ?? useAuthStore.getState().token;
    const allPending = await commandQueue.getPendingCommands();
    const pendingSOS = allPending.filter((c) => c.type === 'SOS');

    if (pendingSOS.length === 0) {
      return { synced: 0, failed: 0, pending: 0 };
    }

    let synced = 0;
    let failed = 0;

    for (const cmd of pendingSOS) {
      await commandQueue.updateCommandState(cmd.commandId, 'SYNCING');
      try {
        const headers: Record<string, string> = {
          'Content-Type': 'application/json',
        };
        if (token) {
          headers['Authorization'] = `Bearer ${token}`;
        }

        const res = await fetch(`${getApiBaseURL()}/api/v1/sos`, {
          method: 'POST',
          headers,
          body: JSON.stringify(cmd.payload),
        });

        if (!res.ok) {
          if (res.status === 401 || res.status === 403) {
            await commandQueue.updateCommandState(cmd.commandId, 'FAILED', {
              errorMessage: `Auth failed (${res.status})`,
            });
            failed++;
            continue;
          }
          await commandQueue.updateCommandState(cmd.commandId, 'PENDING', {
            errorMessage: `Server error ${res.status}`,
          });
          failed++;
          break; // Stop iteration if server is rejecting
        }

        const data = await res.json();
        await commandQueue.updateCommandState(cmd.commandId, 'SYNCED', { response: data });
        synced++;
      } catch (err: any) {
        await commandQueue.updateCommandState(cmd.commandId, 'PENDING', {
          errorMessage: err.message || 'Network connection failed',
        });
        failed++;
        break; // Stop iteration if still offline
      }
    }

    const remaining = (await commandQueue.getPendingCommands()).filter((c) => c.type === 'SOS').length;
    return { synced, failed, pending: remaining };
  }

  /**
   * Returns list of currently pending SOS commands.
   */
  async getPendingSOS(): Promise<QueuedCommand[]> {
    const pending = await commandQueue.getPendingCommands();
    return pending.filter((c) => c.type === 'SOS');
  }
}

export const sosService = new SOSService();
