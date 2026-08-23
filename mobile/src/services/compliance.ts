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

interface RawVehicleDocument {
  doc_type?: string;
  expiry_date?: string | null;
}

/** GET vehicle documents and evaluate compliance. Fetch errors propagate. */
export async function fetchCompliance(vehicleId: string): Promise<ComplianceResult> {
  const token = useAuthStore.getState().token;
  const res = await fetch(`${getApiBaseURL()}/api/v1/documents/vehicle/${vehicleId}`, {
    headers: {
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
  });
  if (!res.ok) {
    throw new Error(`Server returned HTTP ${res.status}`);
  }
  const json = await res.json();
  const rawDocs: RawVehicleDocument[] = Array.isArray(json?.documents) ? json.documents : [];

  // Defensive snake_case → camelCase mapping; unknown doc types ignored
  const docs: VehicleDocument[] = [];
  for (const raw of rawDocs) {
    if (!raw || typeof raw.doc_type !== 'string') continue;
    if (!(REQUIRED_DOCS as string[]).includes(raw.doc_type)) continue;
    docs.push({
      docType: raw.doc_type as DocType,
      expiryDate: typeof raw.expiry_date === 'string' ? raw.expiry_date : null,
    });
  }

  return evaluateCompliance(docs);
}
