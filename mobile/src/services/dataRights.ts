// DPDP Data Principal rights: export + erasure.
import { getApiBaseURL } from '../constants/network';
import { useAuthStore } from '../stores/authStore';
import * as SecureStore from 'expo-secure-store';

function authHeaders(): Record<string, string> {
  const token = useAuthStore.getState().token;
  return {
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
  };
}

export async function exportMyData(): Promise<Record<string, unknown>> {
  const res = await fetch(`${getApiBaseURL()}/api/v1/users/me/data`, {
    method: 'GET',
    headers: authHeaders(),
  });
  if (!res.ok) {
    throw new Error('DATA_EXPORT_FAILED');
  }
  return (await res.json()) as Record<string, unknown>;
}

export async function eraseMyData(): Promise<boolean> {
  const res = await fetch(`${getApiBaseURL()}/api/v1/users/me/data`, {
    method: 'DELETE',
    headers: authHeaders(),
  });
  // 204 no body OR {success:true} both count as erased
  if (res.status === 204) return true;
  if (!res.ok) {
    throw new Error('DATA_ERASE_FAILED');
  }
  const json = await res.json();
  if (json?.success === true) return true;
  throw new Error('DATA_ERASE_FAILED');
}

// Best-effort local PII purge; sqlite queues owned by offlineQueue module
export async function wipeLocalPii(): Promise<void> {
  try {
    await SecureStore.deleteItemAsync('auth_token');
  } catch {
    // best-effort — keep purging remaining keys
  }
  try {
    await SecureStore.deleteItemAsync('auth_user');
  } catch {
    // best-effort
  }
}
