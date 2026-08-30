import { getApiBaseURL } from '../../../constants/network';
import { DocumentCategory, DocumentUploadTask } from '../types/document';

export class DocumentUploadApiClient {
  private getHeaders(token: string) {
    return {
      Authorization: `Bearer ${token}`,
    };
  }

  async uploadDocumentDirect(
    token: string,
    task: DocumentUploadTask
  ): Promise<{ document_id: string; storage_key: string }> {
    const formData = new FormData();
    const filename = task.localUri.split('/').pop() || `${task.documentCategory}.jpg`;

    formData.append('file', {
      uri: task.localUri,
      name: filename,
      type: task.mimeType,
    } as any);

    formData.append('document_type', task.documentCategory);
    formData.append('client_hash', task.clientSha256);
    formData.append('upload_id', task.id);

    const res = await fetch(`${getApiBaseURL()}/api/v1/drivers/me/documents`, {
      method: 'POST',
      headers: this.getHeaders(token),
      body: formData,
    });

    if (!res.ok) {
      if (res.status === 401 || res.status === 403) {
        throw new Error('AUTH_EXPIRED');
      }
      const err = await res.json().catch(() => ({ error: 'Upload failed' }));
      throw new Error(err.error || `Server responded with ${res.status}`);
    }

    return res.json();
  }
}

export const documentUploadApi = new DocumentUploadApiClient();
