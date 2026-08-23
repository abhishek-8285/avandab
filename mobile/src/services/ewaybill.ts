import { getApiBaseURL } from '../constants/network';
import { useAuthStore } from '../stores/authStore';

export interface EWayBill {
  ewayBillNumber: string;
  validUntil: string;
  qrData: string;
  totalValue: number;
  shipToGstin?: string;
}

// GST rule: EWB is mandatory for consignment value ≥ ₹50,000
export function canGenerate(totalValue: number): boolean {
  return totalValue >= 50000;
}

function authHeaders(): Record<string, string> {
  const token = useAuthStore.getState().token;
  return {
    'Content-Type': 'application/json',
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
  };
}

/** Fetch existing EWB for a trip. Returns null on 404 (none generated yet). */
export async function getEwayBill(tripId: string): Promise<EWayBill | null> {
  const res = await fetch(`${getApiBaseURL()}/api/v1/trips/${tripId}/ewaybill`, {
    headers: authHeaders(),
  });
  if (res.status === 404) return null;
  if (!res.ok) {
    throw new Error(`Server returned HTTP ${res.status}`);
  }
  return (await res.json()) as EWayBill;
}

/**
 * Generate an EWB for a trip.
 * Client-side guard: throws before any network call when consignment value
 * is below the ₹50,000 GST threshold. shipToGstin is passed through per the
 * June 15 2026 rule when provided.
 */
export async function generateEwayBill(
  tripId: string,
  opts?: { force?: boolean; totalValue?: number; shipToGstin?: string }
): Promise<EWayBill> {
  if (opts?.totalValue != null && !canGenerate(opts.totalValue)) {
    throw new Error('EWB_NOT_REQUIRED');
  }

  const body: Record<string, unknown> = {};
  if (opts?.shipToGstin) {
    body.ship_to_gstin = opts.shipToGstin;
  }

  const res = await fetch(`${getApiBaseURL()}/api/v1/trips/${tripId}/ewaybill/generate`, {
    method: 'POST',
    headers: authHeaders(),
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    throw new Error(`Server returned HTTP ${res.status}`);
  }
  return (await res.json()) as EWayBill;
}
