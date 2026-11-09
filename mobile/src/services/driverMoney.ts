import { getApiBaseURL } from '../constants/network';
import { useAuthStore } from '../stores/authStore';

/* Spec 22 S7 — driver Paisa tab API client. */

export interface DriverBalance {
  running_balance: number;
  last_settlement_id?: string;
  last_settlement_at?: string;
  pending_advances: number;
}

export interface DriverSettlement {
  id: string;
  period: string;
  gross: number;
  deductions: number;
  net: number;
  status: 'pending' | 'processing' | 'paid' | 'disputed';
  tds: number;
  paid_at?: string;
}

export interface AdvanceRequest {
  id: string;
  trip_id?: string;
  amount: number;
  reason: string;
  status: 'pending' | 'approved' | 'rejected' | 'paid';
  requested_at: string;
  decided_by?: string;
  decided_at?: string;
}

function authHeaders(): Record<string, string> {
  const token = useAuthStore.getState().token;
  return {
    'Content-Type': 'application/json',
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
  };
}

/** GET /api/driver/balance — null when the flag is off or not linked. */
export async function getDriverBalance(): Promise<DriverBalance | null> {
  const res = await fetch(`${getApiBaseURL()}/api/driver/balance`, { headers: authHeaders() });
  if (res.status === 403 || res.status === 404) return null;
  if (!res.ok) throw new Error(`balance fetch failed (${res.status})`);
  return res.json();
}

/** GET /api/driver/settlements */
export async function getDriverSettlements(): Promise<DriverSettlement[]> {
  const res = await fetch(`${getApiBaseURL()}/api/driver/settlements`, { headers: authHeaders() });
  if (res.status === 403 || res.status === 404) return [];
  if (!res.ok) throw new Error(`settlements fetch failed (${res.status})`);
  const data = await res.json();
  return data.settlements ?? [];
}

/** GET /api/driver/advances */
export async function getAdvanceRequests(): Promise<AdvanceRequest[]> {
  const res = await fetch(`${getApiBaseURL()}/api/driver/advances`, { headers: authHeaders() });
  if (res.status === 403 || res.status === 404) return [];
  if (!res.ok) throw new Error(`advances fetch failed (${res.status})`);
  const data = await res.json();
  return data.advances ?? [];
}

/** POST /api/driver/advances — returns the created request id + status. */
export async function requestAdvance(
  body: { trip_id?: string; amount: number; reason: string },
): Promise<{ id: string; status: string }> {
  const res = await fetch(`${getApiBaseURL()}/api/driver/advances`, {
    method: 'POST',
    headers: authHeaders(),
    body: JSON.stringify(body),
  });
  if (res.status === 201) return res.json();
  throw new Error(`advance request failed (${res.status})`);
}

export interface DriverPayoutAccount {
  masked: string;
  bank_name: string;
  ifsc: string;
  verification_status: 'unverified' | 'penny_drop_pending' | 'verified' | 'rejected' | string;
}

export interface DriverPayout {
  id: string;
  amount: number;
  currency: string;
  status: 'initiated' | 'processing' | 'paid' | 'failed' | 'reversed' | 'cancelled' | string;
  utr?: string;
  provider_payout_id?: string;
  failure_reason?: string;
  initiated_at: string;
  completed_at?: string;
}

export interface DriverPayouts {
  rail_enabled: boolean;
  account?: DriverPayoutAccount | null;
  payouts: DriverPayout[];
}

/** GET /api/v1/drivers/me/payouts - real payout-rail state; null when disabled/forbidden. */
export async function getDriverPayouts(): Promise<DriverPayouts | null> {
  const res = await fetch(`${getApiBaseURL()}/api/v1/drivers/me/payouts`, { headers: authHeaders() });
  if (res.status === 403 || res.status === 404) return null;
  if (!res.ok) throw new Error(`payouts fetch failed (${res.status})`);
  const data = await res.json();
  return {
    rail_enabled: !!data.rail_enabled,
    account: data.account ?? null,
    payouts: data.payouts ?? [],
  };
}

/** POST /api/v1/drivers/me/payout-account/verify - starts penny-drop validation. */
export async function verifyPayoutAccount(): Promise<{ status: string; reference?: string }> {
  const res = await fetch(`${getApiBaseURL()}/api/v1/drivers/me/payout-account/verify`, {
    method: 'POST',
    headers: authHeaders(),
  });
  if (!res.ok) throw new Error(`payout verification failed (${res.status})`);
  return res.json();
}
