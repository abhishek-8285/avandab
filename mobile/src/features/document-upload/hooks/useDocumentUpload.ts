import { useState, useEffect, useCallback } from 'react';
import { DocumentCategory, DocumentUploadTask } from '../types/document';
import { validateLocalDocument } from '../validation/documentValidator';
import { computeFileSha256 } from '../hashing/sha256';
import { captureDocumentFromCamera, pickDocumentFromGallery } from '../capture/documentCapture';
import { localDocumentStore } from '../storage/localEncryptedStore';
import { uploadQueue } from '../queue/uploadQueue';

export function useDocumentUpload(token?: string) {
  const [tasks, setTasks] = useState<DocumentUploadTask[]>([]);
  const [busy, setBusy] = useState<boolean>(false);

  const loadTasks = useCallback(async () => {
    const stored = await localDocumentStore.getTasks();
    setTasks(stored);
  }, []);

  useEffect(() => {
    loadTasks();
    const unsubscribe = uploadQueue.addListener((updatedTask) => {
      setTasks((prev) => {
        const idx = prev.findIndex((t) => t.id === updatedTask.id);
        if (idx >= 0) {
          const next = [...prev];
          next[idx] = updatedTask;
          return next;
        }
        return [...prev, updatedTask];
      });
    });
    return unsubscribe;
  }, [loadTasks]);

  const initiateUpload = async (
    category: DocumentCategory,
    source: 'camera' | 'gallery'
  ): Promise<DocumentUploadTask | null> => {
    if (!token) {
      throw new Error('Authentication token required for upload.');
    }

    setBusy(true);
    try {
      const captured =
        source === 'camera'
          ? await captureDocumentFromCamera()
          : await pickDocumentFromGallery();

      if (!captured) {
        setBusy(false);
        return null;
      }

      const validation = validateLocalDocument(
        captured.fileSizeBytes,
        captured.mimeType,
        captured.uri,
        category
      );

      if (!validation.valid) {
        throw new Error(validation.error || 'Invalid file');
      }

      // Compute client-side SHA-256 integrity hash
      const hash = await computeFileSha256(captured.uri);

      const taskId = `upload_${Date.now()}_${Math.random().toString(36).substr(2, 6)}`;
      const task: DocumentUploadTask = {
        id: taskId,
        documentCategory: category,
        localUri: captured.uri,
        mimeType: validation.detectedMime || captured.mimeType,
        fileSizeBytes: captured.fileSizeBytes,
        clientSha256: hash,
        state: 'LOCAL_PENDING',
        retryCount: 0,
        maxRetries: 3,
        progressPct: 0,
        createdAt: Date.now(),
        updatedAt: Date.now(),
      };

      await uploadQueue.enqueueTask(task, token);
      return task;
    } finally {
      setBusy(false);
    }
  };

  return {
    tasks,
    busy,
    initiateUpload,
    refreshTasks: loadTasks,
  };
}
