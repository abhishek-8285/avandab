import { documentUploadApi } from '../api/documentUploadApi';
import { localDocumentStore } from '../storage/localEncryptedStore';
import { DocumentUploadTask } from '../types/document';

export type TaskUpdateListener = (task: DocumentUploadTask) => void;

export class UploadQueue {
  private isProcessing = false;
  private listeners = new Set<TaskUpdateListener>();

  addListener(fn: TaskUpdateListener): () => void {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  }

  private notify(task: DocumentUploadTask) {
    this.listeners.forEach((listener) => {
      try {
        listener(task);
      } catch {}
    });
  }

  async enqueueTask(task: DocumentUploadTask, token: string): Promise<void> {
    task.state = 'QUEUED';
    task.updatedAt = Date.now();
    await localDocumentStore.upsertTask(task);
    this.notify(task);
    this.processNext(token);
  }

  async processNext(token?: string): Promise<void> {
    if (this.isProcessing || !token) return;
    this.isProcessing = true;

    try {
      const tasks = await localDocumentStore.getTasks();
      const pendingTask = tasks.find(
        (t) => t.state === 'QUEUED' || (t.state === 'RETRY_WAIT' && t.retryCount < t.maxRetries)
      );

      if (!pendingTask) {
        this.isProcessing = false;
        return;
      }

      await this.executeTask(pendingTask, token);
    } finally {
      this.isProcessing = false;
    }
  }

  private async executeTask(task: DocumentUploadTask, token: string): Promise<void> {
    task.state = 'UPLOADING';
    task.progressPct = 25;
    task.updatedAt = Date.now();
    await localDocumentStore.upsertTask(task);
    this.notify(task);

    try {
      const result = await documentUploadApi.uploadDocumentDirect(token, task);
      task.serverDocId = result.document_id;
      task.progressPct = 80;
      task.state = 'UPLOADED';
      task.updatedAt = Date.now();
      await localDocumentStore.upsertTask(task);
      this.notify(task);

      // Transition to COMPLETE & Clean up temporary file
      task.state = 'COMPLETE';
      task.progressPct = 100;
      task.updatedAt = Date.now();
      await localDocumentStore.upsertTask(task);
      this.notify(task);

      await localDocumentStore.cleanupTemporaryFile(task.localUri);
    } catch (err: any) {
      if (err.message === 'AUTH_EXPIRED') {
        task.state = 'AUTH_REQUIRED';
        task.errorMessage = 'Session expired. Please log in again.';
      } else if (task.retryCount >= task.maxRetries) {
        task.state = 'REJECTED';
        task.errorMessage = err.message || 'Maximum upload retries reached.';
      } else {
        task.state = 'RETRY_WAIT';
        task.retryCount += 1;
        task.errorMessage = err.message || 'Network failure. Retrying...';
      }
      task.updatedAt = Date.now();
      await localDocumentStore.upsertTask(task);
      this.notify(task);
    }
  }
}

export const uploadQueue = new UploadQueue();
