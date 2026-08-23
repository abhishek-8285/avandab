import { getApiBaseURL } from '../constants/network';
import { useAuthStore } from '../stores/authStore';
import { RetryableOperation } from '../utils/retry';

interface PendingTrip {
  id: string;
}

export interface TripAcceptedEvent {
  tripId: string;
}

type TripAcceptedListener = (event: TripAcceptedEvent) => void;

export interface TripPollerOptions {
  assignMaxRetries?: number;
  assignBaseDelayMs?: number;
}

// Hermes may lack crypto.randomUUID — fall back to a manual RFC 4122 v4 impl.
function generateUuid(): string {
  const g = globalThis as { crypto?: { randomUUID?: () => string } };
  if (g.crypto && typeof g.crypto.randomUUID === 'function') {
    return g.crypto.randomUUID();
  }
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0;
    const v = c === 'x' ? r : (r & 0x3) | 0x8;
    return v.toString(16);
  });
}

/**
 * Autonomous trip-acceptance loop: polls pending trips every interval and
 * auto-claims each one via POST /trips/{id}/assign-driver with an idempotency
 * key cached per trip. All failures are swallowed + warned — never crashes.
 */
class TripPollerService {
  private pollTimer: ReturnType<typeof setInterval> | null = null;
  private driverId: string | null = null;
  private isPolling = false;
  private lastPollAt: number | null = null;
  private acceptedCount = 0;
  private acceptedTripIds: Set<string> = new Set();
  private idempotencyKeys: Map<string, string> = new Map();
  private listeners: Set<TripAcceptedListener> = new Set();
  private assignMaxRetries = 3;
  private assignBaseDelayMs = 5000;

  start(driverId?: string, intervalMs = 30000, options: TripPollerOptions = {}): void {
    if (this.pollTimer) return;
    this.driverId = driverId ?? this.resolveDriverId();
    this.assignMaxRetries = options.assignMaxRetries ?? 3;
    this.assignBaseDelayMs = options.assignBaseDelayMs ?? 5000;
    this.pollTimer = setInterval(() => {
      void this.pollOnce();
    }, intervalMs);
    console.log(`[TRIP POLLER] Auto-accept polling started (${intervalMs / 1000}s interval)`);
  }

  stop(): void {
    if (this.pollTimer) {
      clearInterval(this.pollTimer);
      this.pollTimer = null;
      console.log('[TRIP POLLER] Auto-accept polling stopped');
    }
  }

  getStatus(): { running: boolean; lastPollAt: number | null; acceptedCount: number } {
    return {
      running: this.pollTimer !== null,
      lastPollAt: this.lastPollAt,
      acceptedCount: this.acceptedCount,
    };
  }

  onTripAccepted(listener: TripAcceptedListener): () => void {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  }

  /** Test/diagnostic hook: full state reset including the timer. */
  reset(): void {
    this.stop();
    this.driverId = null;
    this.isPolling = false;
    this.lastPollAt = null;
    this.acceptedCount = 0;
    this.acceptedTripIds.clear();
    this.idempotencyKeys.clear();
    this.listeners.clear();
  }

  private resolveDriverId(): string | null {
    const user = useAuthStore.getState().user;
    return user?.driverId ?? user?.id ?? null;
  }

  // One key per trip id, generated once and reused across retries/polls.
  private getIdempotencyKey(tripId: string): string {
    let key = this.idempotencyKeys.get(tripId);
    if (!key) {
      key = generateUuid();
      this.idempotencyKeys.set(tripId, key);
    }
    return key;
  }

  private emitAccepted(event: TripAcceptedEvent): void {
    this.listeners.forEach((fn) => {
      try {
        fn(event);
      } catch {
        // listener errors never break the poll loop
      }
    });
  }

  private async pollOnce(): Promise<void> {
    if (this.isPolling) return;
    this.isPolling = true;
    try {
      const token = useAuthStore.getState().token;
      // Server resolves "me" from the bearer token
      const url = `${getApiBaseURL()}/api/v1/trips?driver_id=me&status=pending`;
      const res = await fetch(url, {
        headers: {
          Accept: 'application/json',
          ...(token ? { Authorization: `Bearer ${token}` } : {}),
        },
      });
      if (!res.ok) {
        console.warn(`[TRIP POLLER] Poll failed: HTTP ${res.status}`);
        return;
      }
      const data: unknown = await res.json();
      const trips = this.extractTrips(data);
      for (const trip of trips) {
        await this.assign(trip.id, token);
      }
    } catch (err) {
      // Never let the loop crash
      console.warn('[TRIP POLLER] Poll failed:', err instanceof Error ? err.message : err);
    } finally {
      this.lastPollAt = Date.now();
      this.isPolling = false;
    }
  }

  // Tolerate array root or {trips: []} / {data: []} envelope shapes.
  private extractTrips(data: unknown): PendingTrip[] {
    let raw: unknown = data;
    if (raw && typeof raw === 'object' && !Array.isArray(raw)) {
      const obj = raw as Record<string, unknown>;
      raw = Array.isArray(obj.trips) ? obj.trips : Array.isArray(obj.data) ? obj.data : [];
    }
    if (!Array.isArray(raw)) return [];
    return raw.filter((t): t is PendingTrip => !!t && typeof (t as PendingTrip).id === 'string');
  }

  private async assign(tripId: string, token: string | null): Promise<void> {
    const key = this.getIdempotencyKey(tripId);
    const op = new RetryableOperation({
      maxRetries: this.assignMaxRetries,
      baseDelayMs: this.assignBaseDelayMs,
    });
    try {
      const status = await op.execute(async () => {
        const res = await fetch(
          `${getApiBaseURL()}/api/v1/trips/${encodeURIComponent(tripId)}/assign-driver`,
          {
            method: 'POST',
            headers: {
              Accept: 'application/json',
              'Content-Type': 'application/json',
              'Idempotency-Key': key,
              ...(token ? { Authorization: `Bearer ${token}` } : {}),
            },
            body: JSON.stringify({}),
          },
        );
        // 409 = already claimed — success, don't retry
        if (res.status === 409) return res.status;
        if (!res.ok) throw new Error(`Assign failed: HTTP ${res.status}`);
        return res.status;
      });
      // Notify once per accepted trip even if later polls re-POST it
      if (!this.acceptedTripIds.has(tripId)) {
        this.acceptedTripIds.add(tripId);
        this.acceptedCount += 1;
        this.emitAccepted({ tripId });
      }
      console.log(`[TRIP POLLER SUCCESS] Trip ${tripId} auto-accepted (HTTP ${status})`);
    } catch (err) {
      console.warn(
        `[TRIP POLLER] Auto-accept failed for ${tripId}:`,
        err instanceof Error ? err.message : err,
      );
    }
  }
}

export const TripPoller = new TripPollerService();
