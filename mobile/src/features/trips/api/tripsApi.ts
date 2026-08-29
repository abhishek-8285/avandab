import { getApiBaseURL } from '../../../constants/network';
import { useAuthStore } from '../../../stores/authStore';
import { Trip } from '../../../types/api';
import { mapTripStatus, RawTrip } from '../../../utils/tripMapper';

// §14 OpenAPI contract — single source, not duplicated per screen
export type StartTripRequest = { idempotencyKey: string };
export type StartTripResponse = { trip_id: string; status: string; server_version: number; started_at: string };

async function authHeaders(extra: Record<string, string> = {}): Promise<Record<string, string>> {
  const token = useAuthStore.getState().token;
  return { 'Content-Type': 'application/json', ...(token ? { Authorization: `Bearer ${token}` } : {}), ...extra };
}

export const tripsApi = {
  async listMyTrips(): Promise<Trip[]> {
    const token = useAuthStore.getState().token;
    const res = await fetch(`${getApiBaseURL()}/api/v1/trips?driver_id=me&page=1&limit=50`, {
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    const json = await res.json();
    return ((json.trips as RawTrip[]) || []).map(mapTripStatus);
  },

  async startTrip(tripId: string, idempotencyKey: string): Promise<StartTripResponse> {
    const headers = await authHeaders({ 'Idempotency-Key': idempotencyKey });
    const res = await fetch(`${getApiBaseURL()}/api/v1/trips/${tripId}/start`, {
      method: 'POST',
      headers,
    });
    if (!res.ok) {
      const body = await res.json().catch(() => ({}));
      throw new Error(body.error || `HTTP ${res.status}`);
    }
    return res.json();
  },

  async cancelTrip(tripId: string, idempotencyKey: string): Promise<void> {
    const headers = await authHeaders({ 'Idempotency-Key': idempotencyKey });
    const res = await fetch(`${getApiBaseURL()}/api/v1/trips/${tripId}/cancel`, { method: 'POST', headers });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
  },
};
