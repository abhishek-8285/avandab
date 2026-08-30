export type DocumentUploadState =
  | 'LOCAL_PENDING'
  | 'QUEUED'
  | 'UPLOADING'
  | 'RETRY_WAIT'
  | 'AUTH_REQUIRED'
  | 'REJECTED'
  | 'UPLOADED'
  | 'SERVER_VERIFIED'
  | 'COMPLETE';

export type DocumentCategory =
  | 'dl_front'
  | 'dl_back'
  | 'aadhaar_front'
  | 'aadhaar_back'
  | 'pan_card'
  | 'profile_photo'
  | 'vehicle_rc'
  | 'vehicle_insurance'
  | 'vehicle_fitness'
  | 'vehicle_permit'
  | 'vehicle_puc';

export interface DocumentUploadTask {
  id: string; // upload_id (UUID)
  documentCategory: DocumentCategory;
  localUri: string;
  mimeType: string;
  fileSizeBytes: number;
  clientSha256: string;
  state: DocumentUploadState;
  retryCount: number;
  maxRetries: number;
  progressPct: number;
  errorMessage?: string;
  serverDocId?: string;
  serverSha256?: string;
  createdAt: number;
  updatedAt: number;
}

export interface ValidationResult {
  valid: boolean;
  error?: string;
  detectedMime?: string;
}

export interface PresignedUploadSession {
  uploadId: string;
  uploadUrl: string;
  storageKey: string;
  headers: Record<string, string>;
  expiresAt: number;
}
