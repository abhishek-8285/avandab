import { commandQueue, QueuedCommand } from './commandQueue';
import { getApiBaseURL } from '../../constants/network';

export class CommandProcessor {
  private isProcessing = false;

  async flush(token: string): Promise<{ synced: number; failed: number }> {
    if (this.isProcessing || !token) {
      return { synced: 0, failed: 0 };
    }

    this.isProcessing = true;
    let synced = 0;
    let failed = 0;

    try {
      const pending = await commandQueue.getPendingCommands();
      for (const cmd of pending) {
        await commandQueue.updateCommandState(cmd.commandId, 'SYNCING');
        try {
          let url = `${getApiBaseURL()}/api/v1/drivers/me/commands`;
          let bodyPayload: any = {
            command_id: cmd.commandId,
            type: cmd.type,
            payload: cmd.payload,
          };

          // Route commands to authoritative domain endpoints
          if (cmd.type === 'SOS') {
            url = `${getApiBaseURL()}/api/v1/sos`;
            bodyPayload = cmd.payload;
          } else if (cmd.type === 'REACH_STOP') {
            url = `${getApiBaseURL()}/trips/${cmd.payload.trip_id}/stops/${cmd.payload.stop_id}/reach`;
            bodyPayload = cmd.payload;
          } else if (cmd.type === 'SUBMIT_STOP_POD') {
            url = `${getApiBaseURL()}/trips/${cmd.payload.trip_id}/stops/${cmd.payload.stop_id}/pod`;
            bodyPayload = cmd.payload;
          } else if (cmd.type === 'COMPLETE_STOP') {
            url = `${getApiBaseURL()}/trips/${cmd.payload.trip_id}/stops/${cmd.payload.stop_id}/complete`;
            bodyPayload = cmd.payload;
          } else if (cmd.type === 'START_TRIP') {
            url = `${getApiBaseURL()}/api/v1/trips/${cmd.payload.trip_id}/start`;
            bodyPayload = cmd.payload;
          } else if (cmd.type === 'COMPLETE_TRIP') {
            url = `${getApiBaseURL()}/api/v1/trips/${cmd.payload.trip_id}/complete`;
            bodyPayload = cmd.payload;
          }

          const res = await fetch(url, {
            method: 'POST',
            headers: {
              'Content-Type': 'application/json',
              Authorization: `Bearer ${token}`,
              'X-Command-Id': cmd.commandId,
            },
            body: JSON.stringify(bodyPayload),
          });

          if (!res.ok) {
            if (res.status === 401 || res.status === 403) {
              await commandQueue.updateCommandState(cmd.commandId, 'FAILED', {
                errorMessage: `Auth error (${res.status})`,
              });
              failed++;
              continue;
            }

            const errData = await res.json().catch(() => ({ error: res.statusText }));
            await commandQueue.updateCommandState(cmd.commandId, 'FAILED', {
              errorMessage: errData.message || errData.error || `HTTP ${res.status}`,
            });
            failed++;
            break; // Stop on unexpected network/server error to preserve ordering
          }

          const data = await res.json();
          await commandQueue.updateCommandState(cmd.commandId, 'SYNCED', { response: data });
          synced++;
        } catch (err: any) {
          await commandQueue.updateCommandState(cmd.commandId, 'PENDING', {
            errorMessage: err.message || 'Network connection failed',
          });
          failed++;
          break; // Offline: preserve queue
        }
      }
    } finally {
      this.isProcessing = false;
    }

    return { synced, failed };
  }
}

export const commandProcessor = new CommandProcessor();
