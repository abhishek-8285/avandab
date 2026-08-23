import { getApiBaseURL } from '../constants/network';
import { useAuthStore } from '../stores/authStore';

export const DOC_TYPES = ['aadhaar', 'pan', 'dl', 'bank_proof', 'medical', 'other'] as const;
export type VaultDocType = typeof DOC_TYPES[number];

export interface VaultDocument {
  id?: string;
  docType: VaultDocType;
  fileName?: string;
  expiryDate?: string | null;
}

export interface UploadVaultDocumentInput {
  docType: VaultDocType;
  fileUri: string;
  fileName?: string;
  expiryDate?: string;
}

const EXPIRY_DATE_RE = /^\d{4}-\d{2}-\d{2}$/;

function authHeaders(): Record<string, string> {
  const token = useAuthStore.getState().token;
  return {
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
  };
}

// Shared upload — multipart FormData like offlineQueue does
async function uploadDocument(
  ownerKind: 'driver' | 'vehicle',
  ownerId: string,
  input: UploadVaultDocumentInput
): Promise<void> {
  if (!(DOC_TYPES as readonly string[]).includes(input.docType)) {
    throw new Error('INVALID_DOC_TYPE');
  }
  if (input.expiryDate !== undefined && !EXPIRY_DATE_RE.test(input.expiryDate)) {
    throw new Error('INVALID_EXPIRY_DATE');
  }

  const form = new FormData();
  form.append('doc_type', input.docType);
  if (input.expiryDate !== undefined) {
    form.append('expiry_date', input.expiryDate);
  }
  form.append('file', {
    uri: input.fileUri,
    name: input.fileName || 'document.jpg',
    type: 'image/jpeg',
  } as any);

  const res = await fetch(`${getApiBaseURL()}/api/v1/documents/${ownerKind}/${ownerId}`, {
    method: 'POST',
    headers: authHeaders(),
    body: form,
  });
  if (!res.ok) {
    throw new Error(`Server returned HTTP ${res.status}`);
  }
}

export function uploadDriverDocument(driverId: string, input: UploadVaultDocumentInput): Promise<void> {
  return uploadDocument('driver', driverId, input);
}

export function uploadVehicleDocument(vehicleId: string, input: UploadVaultDocumentInput): Promise<void> {
  return uploadDocument('vehicle', vehicleId, input);
}

interface RawVaultDocument {
  id?: string;
  doc_type?: string;
  file_name?: string;
  expiry_date?: string | null;
}

// Defensive snake_case → camelCase mapping; unknown doc types dropped
function mapRawDocuments(rawDocs: RawVaultDocument[]): VaultDocument[] {
  return rawDocs
    .filter((d) => d && typeof d.doc_type === 'string' && (DOC_TYPES as readonly string[]).includes(d.doc_type))
    .map((d) => ({
      id: d.id,
      docType: d.doc_type as VaultDocType,
      fileName: d.file_name,
      expiryDate: d.expiry_date ?? null,
    }));
}

async function listDocuments(ownerKind: 'driver' | 'vehicle', ownerId: string): Promise<VaultDocument[]> {
  const res = await fetch(`${getApiBaseURL()}/api/v1/documents/${ownerKind}/${ownerId}`, {
    headers: authHeaders(),
  });
  if (!res.ok) {
    throw new Error(`Server returned HTTP ${res.status}`);
  }
  const json = await res.json();
  const rawDocs: RawVaultDocument[] = Array.isArray(json?.documents) ? json.documents : [];
  return mapRawDocuments(rawDocs);
}

export async function listDriverDocuments(driverId: string): Promise<VaultDocument[]> {
  return listDocuments('driver', driverId);
}

export async function listVehicleDocuments(vehicleId: string): Promise<VaultDocument[]> {
  return listDocuments('vehicle', vehicleId);
}
