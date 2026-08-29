import { getApiBaseURL } from '../constants/network';
import { useAuthStore } from '../stores/authStore';

export interface FastagTxn {
  id: string;
  amount: number;
  plaza: string;
  timestamp: string;
  matched_trip_id?: string | null;
}

function authHeaders(): Record<string, string> {
  const token = useAuthStore.getState().token;
  return {
    'Content-Type': 'application/json',
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
  };
}

/** GET /api/fastag/transactions — honest 404 when FASTag add-on not linked; empty array offline. */
export async function getFastagTxns(): Promise<FastagTxn[]> {
  try {
    const res = await fetch(`${getApiBaseURL()}/api/fastag/transactions`, { headers: authHeaders() });
    if (!res.ok) return [];
    const j = await res.json();
    return (j.transactions ?? j.txns ?? []) as FastagTxn[];
  } catch {
    return [];
  }
}

export function reconcileFastagToTrip(txn: FastagTxn, tripRoute: string): boolean {
  if (!txn.plaza || !tripRoute) return false;
  const plaza = txn.plaza.toLowerCase();
  const route = tripRoute.toLowerCase();
  return route.includes(plaza.slice(0, 4)) || plaza.includes(route.split('→')[0]?.trim().slice(0, 4) || '');
}
