import { validateLocalDocument } from '../src/features/document-upload/validation/documentValidator';
import { computeFileSha256 } from '../src/features/document-upload/hashing/sha256';
import { UploadQueue } from '../src/features/document-upload/queue/uploadQueue';
import { DocumentUploadTask } from '../src/features/document-upload/types/document';

describe('Document Upload Subsystem', () => {
  describe('Document Validator', () => {
    it('validates a correct JPEG document', () => {
      const result = validateLocalDocument(2048, 'image/jpeg', 'file:///test/dl.jpg', 'dl_front');
      expect(result.valid).toBe(true);
      expect(result.detectedMime).toBe('image/jpeg');
    });

    it('rejects files smaller than minimum threshold (corrupted)', () => {
      const result = validateLocalDocument(100, 'image/jpeg', 'file:///test/dl.jpg', 'dl_front');
      expect(result.valid).toBe(false);
      expect(result.error).toContain('empty or corrupted');
    });

    it('rejects files larger than 15MB', () => {
      const result = validateLocalDocument(16 * 1024 * 1024, 'image/jpeg', 'file:///test/huge.jpg', 'dl_front');
      expect(result.valid).toBe(false);
      expect(result.error).toContain('exceeds 15MB limit');
    });

    it('rejects unsupported file formats like executable or zip', () => {
      const result = validateLocalDocument(2048, 'application/zip', 'file:///test/dl.zip', 'dl_front');
      expect(result.valid).toBe(false);
      expect(result.error).toContain('Unsupported file format');
    });
  });

  describe('Integrity Hashing', () => {
    it('computes SHA-256 hash string', async () => {
      const hash = await computeFileSha256('file:///mock/path.jpg');
      expect(typeof hash).toBe('string');
      expect(hash.length).toBe(64);
    });
  });

  describe('Upload State Machine & Queue', () => {
    it('notifies listeners on enqueue', async () => {
      const queue = new UploadQueue();
      const task: DocumentUploadTask = {
        id: 'task_1',
        documentCategory: 'dl_front',
        localUri: 'file:///mock/dl.jpg',
        mimeType: 'image/jpeg',
        fileSizeBytes: 2048,
        clientSha256: 'hash123',
        state: 'LOCAL_PENDING',
        retryCount: 0,
        maxRetries: 3,
        progressPct: 0,
        createdAt: Date.now(),
        updatedAt: Date.now(),
      };

      const states: string[] = [];
      const unsub = queue.addListener((t) => {
        states.push(t.state);
      });

      // Enqueue will set state to QUEUED and notify
      await queue.enqueueTask(task, 'mock-token');
      expect(states).toContain('QUEUED');
      unsub();
    });
  });
});
