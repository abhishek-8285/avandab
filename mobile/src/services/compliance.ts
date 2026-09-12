import { getApiBaseURL } from '../constants/network';
import { useAuthStore } from '../stores/authStore';

// Automated compliance checks for vehicle documents (RC/fitness/insurance/PUC/permit/tax)
export type DocType = 'rc' | 'fitness' | 'insurance' | 'puc' | 'permit' | 'road_tax';

export interface VehicleDocument {
  docType: DocType;
  expiryDate: string | null; // ISO date (YYYY-MM-DD)
}

export const REQUIRED_DOCS: DocType[] = ['rc', 'fitness', 'insurance', 'puc', 'permit', 'road_tax'];

export interface ComplianceResult {
  score: 'green' | 'amber' | 'red';
  canStartTrip: boolean;
  missing: DocType[];
  expired: VehicleDocument[];
  expiringSoon: VehicleDocument[];
}

const EXPIRING_SOON_DAYS = 7;
const MS_PER_DAY = 24 * 60 * 60 * 1000;

// UTC day number so comparisons are timezone-independent ("expiryDate < now day")
function dayNumber(isoDate: string): number | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(isoDate);
  if (!m) return null;
  return Date.UTC(Number(m[1]), Number(m[2]) - 1, Number(m[3])) / MS_PER_DAY;
}

/**
 * Pure evaluation — safe to unit test without network.
 * Rules: expired → red → block; expiring ≤7d → amber → block;
 * missing → amber → allow (soft warning); none → green → allow.
 */
export function evaluateCompliance(docs: VehicleDocument[], now: Date = new Date()): ComplianceResult {
  const byType = new Map<DocType, VehicleDocument>();
  for (const doc of docs) {
    if (REQUIRED_DOCS.includes(doc.docType)) {
      byType.set(doc.docType, doc);
    }
  }

  const missing: DocType[] = REQUIRED_DOCS.filter((t) => !byType.has(t));

  const todayStr = now.toISOString();
  const today = dayNumber(todayStr) ?? 0;

  const expired: VehicleDocument[] = [];
  const expiringSoon: VehicleDocument[] = [];

  for (const doc of byType.values()) {
    // Present but undated docs cannot be checked — neither expired nor missing
    if (!doc.expiryDate) continue;
    const expiryDay = dayNumber(doc.expiryDate);
    if (expiryDay == null) continue;

    const diffDays = expiryDay - today;
    if (diffDays < 0) {
      expired.push(doc);
    } else if (diffDays <= EXPIRING_SOON_DAYS) {
      // Same-day expiry still blocks (expires today counts as within the window)
      expiringSoon.push(doc);
    }
  }

  if (expired.length > 0) {
    return { score: 'red', canStartTrip: false, missing, expired, expiringSoon };
  }
  if (expiringSoon.length > 0) {
    return { score: 'amber', canStartTrip: false, missing, expired, expiringSoon };
  }
  if (missing.length > 0) {
    // Missing docs are a soft warning — driver may start the trip
    return { score: 'amber', canStartTrip: true, missing, expired, expiringSoon };
  }
  return { score: 'green', canStartTrip: true, missing, expired, expiringSoon };
}

interface RawVehicle {
  insurance_expiry?: string | null;
  fitness_expiry?: string | null;
  permit_expiry?: string | null;
  rc_expiry?: string | null;
  puc_expiry?: string | null;
}

/** GET vehicle expiries and evaluate compliance. Fetch errors propagate. */
export async function fetchCompliance(vehicleId: string): Promise<ComplianceResult> {
  const token = useAuthStore.getState().token;
  // NOTE: there is no /api/v1/documents/vehicle route (never mounted).
  // Compliance expiries live on the vehicle row: GET /api/v1/vehicles/{id}.
  const res = await fetch(`${getApiBaseURL()}/api/v1/vehicles/${encodeURIComponent(vehicleId)}`, {
    headers: {
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
  });
  if (!res.ok) {
    throw new Error(`Server returned HTTP ${res.status}`);
  }
  const json = (await res.json()) as RawVehicle;
  const docs: VehicleDocument[] = [];
  const pick = (docType: DocType, v: unknown) => {
    if (typeof v === 'string' && v) docs.push({ docType, expiryDate: v.slice(0, 10) });
  };
  // road_tax has no server field: stays missing (soft warning by design).
  pick('insurance', json?.insurance_expiry);
  pick('fitness', json?.fitness_expiry);
  pick('permit', json?.permit_expiry);
  pick('rc', json?.rc_expiry);
  pick('puc', json?.puc_expiry);

  return evaluateCompliance(docs);
}
