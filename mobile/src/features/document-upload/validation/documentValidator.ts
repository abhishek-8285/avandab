import { ValidationResult, DocumentCategory } from '../types/document';

const MAX_FILE_SIZE_BYTES = 15 * 1024 * 1024; // 15MB
const MIN_FILE_SIZE_BYTES = 512; // 512 bytes

const ALLOWED_MIME_TYPES = new Set([
  'image/jpeg',
  'image/png',
  'image/webp',
  'application/pdf',
]);

export function validateLocalDocument(
  fileSizeBytes: number,
  mimeType: string,
  uri: string,
  category: DocumentCategory
): ValidationResult {
  if (!uri || uri.trim() === '') {
    return { valid: false, error: 'Document URI is empty or invalid' };
  }

  if (fileSizeBytes < MIN_FILE_SIZE_BYTES) {
    return { valid: false, error: 'Document file is empty or corrupted' };
  }

  if (fileSizeBytes > MAX_FILE_SIZE_BYTES) {
    return {
      valid: false,
      error: `File size exceeds 15MB limit (${(fileSizeBytes / (1024 * 1024)).toFixed(1)}MB)`,
    };
  }

  const normalizedMime = (mimeType || '').toLowerCase().trim();
  const ext = uri.split('.').pop()?.toLowerCase() || '';

  let detectedMime = normalizedMime;
  if (!detectedMime || detectedMime === 'application/octet-stream') {
    if (ext === 'jpg' || ext === 'jpeg') detectedMime = 'image/jpeg';
    else if (ext === 'png') detectedMime = 'image/png';
    else if (ext === 'webp') detectedMime = 'image/webp';
    else if (ext === 'pdf') detectedMime = 'application/pdf';
  }

  if (!ALLOWED_MIME_TYPES.has(detectedMime)) {
    return {
      valid: false,
      error: `Unsupported file format (${detectedMime || ext}). Please upload JPG, PNG, or PDF.`,
    };
  }

  return {
    valid: true,
    detectedMime,
  };
}
