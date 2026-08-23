// Aadhaar eSign client — UIDAI flow is backend-proxied (product spec).
import { getApiBaseURL } from '../constants/network';
import { useAuthStore } from '../stores/authStore';

export type ESignStatus = 'pending' | 'otp_sent' | 'signed' | 'failed';

export interface ESignAuditTrail {
  timestamp: string;
  ipAddress?: string;
  certificateDetails?: string;
}

export interface ESignRequest {
  requestId: string;
  documentId: string;
  status: ESignStatus;
  esignUrl?: string;
  maskedAadhaar?: string;
  auditTrail?: ESignAuditTrail;
}

const VALID_STATUSES: ESignStatus[] = ['pending', 'otp_sent', 'signed', 'failed'];

interface RawAuditTrail {
  timestamp?: string;
  ip_address?: string;
  ipAddress?: string;
  certificate_details?: string;
  certificateDetails?: string;
}

// Defensive snake_case-first mapping (camelCase tolerated)
interface RawESignRequest {
  request_id?: string;
  requestId?: string;
  document_id?: string;
  documentId?: string;
  status?: string;
  esign_url?: string;
  esignUrl?: string;
  masked_aadhaar?: string;
  maskedAadhaar?: string;
  audit_trail?: RawAuditTrail;
  auditTrail?: RawAuditTrail;
}

function authHeaders(): Record<string, string> {
  const token = useAuthStore.getState().token;
  return {
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
  };
}

function mapRawRequest(raw: RawESignRequest): ESignRequest {
  const trail = raw.audit_trail ?? raw.auditTrail;
  const status = VALID_STATUSES.includes(raw.status as ESignStatus)
    ? (raw.status as ESignStatus)
    : 'pending';
  return {
    requestId: raw.request_id ?? raw.requestId ?? '',
    documentId: raw.document_id ?? raw.documentId ?? '',
    status,
    esignUrl: raw.esign_url ?? raw.esignUrl,
    maskedAadhaar: raw.masked_aadhaar ?? raw.maskedAadhaar,
    auditTrail: trail
      ? {
          timestamp: trail.timestamp ?? '',
          ipAddress: trail.ip_address ?? trail.ipAddress,
          certificateDetails: trail.certificate_details ?? trail.certificateDetails,
        }
      : undefined,
  };
}

export async function initateESign(documentId: string): Promise<ESignRequest> {
  const res = await fetch(`${getApiBaseURL()}/api/v1/documents/${documentId}/esign`, {
    method: 'POST',
    headers: { ...authHeaders(), 'Content-Type': 'application/json' },
    body: JSON.stringify({}),
  });
  if (!res.ok) {
    throw new Error('ESIGN_INIT_FAILED');
  }
  const json = await res.json();
  return mapRawRequest(json);
}

export async function pollESignStatus(requestId: string): Promise<ESignRequest> {
  const res = await fetch(`${getApiBaseURL()}/api/v1/documents/esign/${requestId}/status`, {
    headers: authHeaders(),
  });
  if (!res.ok) {
    if (res.status === 404) {
      throw new Error('ESIGN_REQUEST_NOT_FOUND');
    }
    throw new Error(`Server returned HTTP ${res.status}`);
  }
  const json = await res.json();
  return mapRawRequest(json);
}

const AADHAAR_RE = /^\d{12}$/;

// '123456781234' → 'XXXX XXXX 1234'. Already-masked input passes through trimmed.
export function maskAadhaar(aadhaar: string): string | null {
  const trimmed = aadhaar.trim();
  if (trimmed.includes('X')) return trimmed;
  if (!AADHAAR_RE.test(trimmed)) return null;
  return `XXXX XXXX ${trimmed.slice(-4)}`;
}
