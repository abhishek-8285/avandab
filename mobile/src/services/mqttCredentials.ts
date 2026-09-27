import * as SecureStore from 'expo-secure-store';
import { getApiBaseURL } from '../constants/network';
import { useAuthStore } from '../stores/authStore';

/**
 * Per-driver MQTT broker credential (docs/13 §5.2).
 *
 * The production broker runs `allow_anonymous false` with
 * `pattern write avandab/telemetry/drivers/%u/gps`, so a phone must
 * authenticate and must publish under the same identity the ACL binds. The
 * backend issues the secret exactly once — only its mosquitto hash is stored
 * server-side — so this service keeps the plaintext in secure storage and asks
 * for a rotation when it is missing.
 */
export interface BrokerCredential {
  username: string;
  password: string;
}

const STORAGE_KEY = 'avandab_mqtt_credential';

// Stashed for the session: SecureStore is a round trip to the keystore and
// connect() is called on every reconnect.
let cached: BrokerCredential | null = null;

async function store(cred: BrokerCredential): Promise<void> {
  cached = cred;
  try {
    await SecureStore.setItemAsync(STORAGE_KEY, JSON.stringify(cred));
  } catch (e: any) {
    // A keystore that refuses must not silently downgrade the app to an
    // anonymous connection: the caller decides what to do about no secret.
    console.log('[MQTT CRED] secure storage unavailable:', e?.message);
  }
}

export async function storedBrokerCredential(): Promise<BrokerCredential | null> {
  if (cached) return cached;
  try {
    const raw = await SecureStore.getItemAsync(STORAGE_KEY);
    if (raw) cached = JSON.parse(raw) as BrokerCredential;
  } catch (e: any) {
    console.log('[MQTT CRED] secure read failed:', e?.message);
  }
  return cached;
}

export function clearCachedBrokerCredential(): void {
  cached = null;
}

/**
 * Resolve the credential for the signed-in driver, provisioning it on first
 * use and recovering a lost secret through rotation. Returns null when the
 * server has nothing to hand out (no session, or the driver identity is not a
 * valid broker username) — the caller then connects without one, which only
 * works against an anonymous dev broker.
 */
export async function getBrokerCredential(opts: { force?: boolean } = {}): Promise<BrokerCredential | null> {
  if (!opts.force) {
    const existing = await storedBrokerCredential();
    if (existing) return existing;
  }

  const token = useAuthStore.getState().token;
  if (!token) return null;

  // First call: the response carries the plaintext secret. Repeat calls report
  // `provisioned: false` because the server keeps only the hash — that is the
  // signal to rotate, not a failure.
  const first = await fetchCredential(`${getApiBaseURL()}/api/v1/telemetry/mqtt-credentials`, token, 'GET');
  if (first?.provisioned && first.username && first.password) {
    await store({ username: first.username, password: first.password });
    return cached;
  }

  if (first?.username) {
    const rotated = await fetchCredential(`${getApiBaseURL()}/api/v1/telemetry/mqtt-credentials/rotate`, token, 'POST');
    if (rotated?.provisioned && rotated.username && rotated.password) {
      await store({ username: rotated.username, password: rotated.password });
      return cached;
    }
  }
  return null;
}

type CredentialResponse = { username?: string; password?: string; provisioned?: boolean } | null;

async function fetchCredential(url: string, token: string, method: 'GET' | 'POST'): Promise<CredentialResponse> {
  try {
    const res = await fetch(url, {
      method,
      headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    });
    if (!res.ok) {
      console.log(`[MQTT CRED] ${method} ${url} -> HTTP ${res.status}`);
      return null;
    }
    return (await res.json()) as CredentialResponse;
  } catch (e: any) {
    console.log(`[MQTT CRED] ${method} failed:`, e?.message);
    return null;
  }
}
