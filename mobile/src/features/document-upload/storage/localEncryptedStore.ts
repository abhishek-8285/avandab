// Encrypted at-rest storage for the document-upload queue.
//
// The previous implementation kept queued document metadata (local URI,
// category, hashes) in plain AsyncStorage while the filename claimed
// "encrypted". This version stores every value in expo-secure-store, which
// maps to the iOS Keychain / Android Keystore. The index is chunked because
// SecureStore values are advisory-limited to ~2 KB on iOS.
import AsyncStorage from '@react-native-async-storage/async-storage';
import * as SecureStore from 'expo-secure-store';
// ponytail: legacy FileSystem namespace; migrate to File/Directory/Paths classes if expo drops the legacy subpath.
import * as FileSystem from 'expo-file-system/legacy';
import { DocumentUploadTask } from '../types/document';

const TASK_KEY_PREFIX = 'avandab_doc_upload_task_v2_';
const INDEX_CHUNK_PREFIX = 'avandab_doc_upload_index_v2_';
const INDEX_CHUNK_COUNT_KEY = 'avandab_doc_upload_index_v2_chunks';
const INDEX_CHUNK_SIZE = 20;
// iOS Keychain rejects values above ~2 KB; keep a safety margin.
const SECURE_VALUE_LIMIT = 1800;

// One-time migration source: the old plaintext queue. Removed only after the
// encrypted copy is durable, so an interrupted upgrade does not lose uploads.
const LEGACY_QUEUE_KEY = 'avandab_doc_upload_queue_v1';
let legacyQueueChecked = false;

export class LocalDocumentStore {
  private taskKey(id: string): string {
    return TASK_KEY_PREFIX + id;
  }

  private indexKey(index: number): string {
    return INDEX_CHUNK_PREFIX + index;
  }

  private async readIndex(): Promise<string[]> {
    const countRaw = await SecureStore.getItemAsync(INDEX_CHUNK_COUNT_KEY);
    const count = countRaw ? parseInt(countRaw, 10) : 0;
    if (!Number.isFinite(count) || count <= 0) {
      return [];
    }

    const chunks = await Promise.all(
      Array.from({ length: count }, (_, i) => SecureStore.getItemAsync(this.indexKey(i)))
    );

    return chunks.flatMap((raw) => {
      if (!raw) {
        return [];
      }
      try {
        const ids = JSON.parse(raw);
        return Array.isArray(ids) ? ids.filter((id): id is string => typeof id === 'string') : [];
      } catch {
        return [];
      }
    });
  }

  private async writeIndex(ids: string[]): Promise<void> {
    const previousCountRaw = await SecureStore.getItemAsync(INDEX_CHUNK_COUNT_KEY);
    const previousCount = previousCountRaw ? parseInt(previousCountRaw, 10) : 0;

    const groups: string[][] = [];
    for (let i = 0; i < ids.length; i += INDEX_CHUNK_SIZE) {
      groups.push(ids.slice(i, i + INDEX_CHUNK_SIZE));
    }

    await Promise.all(
      groups.map((group, i) => {
        const raw = JSON.stringify(group);
        if (raw.length > SECURE_VALUE_LIMIT) {
          throw new Error('document upload index chunk exceeds secure storage limit');
        }
        return SecureStore.setItemAsync(this.indexKey(i), raw);
      })
    );

    // Drop chunks that no longer exist after a shrink.
    const stale: number[] = [];
    for (let i = groups.length; i < previousCount; i += 1) {
      stale.push(i);
    }
    await Promise.all(stale.map((i) => SecureStore.deleteItemAsync(this.indexKey(i))));

    if (groups.length === 0) {
      await SecureStore.deleteItemAsync(INDEX_CHUNK_COUNT_KEY);
    } else {
      await SecureStore.setItemAsync(INDEX_CHUNK_COUNT_KEY, String(groups.length));
    }
  }

  private async persist(tasks: DocumentUploadTask[]): Promise<void> {
    const previousIds = await this.readIndex();
    const nextIds = tasks.map((task) => task.id);
    const removedIds = previousIds.filter((id) => !nextIds.includes(id));

    await Promise.all(
      tasks.map((task) => {
        const raw = JSON.stringify(task);
        if (raw.length > SECURE_VALUE_LIMIT) {
          throw new Error('document upload task exceeds secure storage limit');
        }
        return SecureStore.setItemAsync(this.taskKey(task.id), raw);
      })
    );

    await this.writeIndex(nextIds);
    await Promise.all(removedIds.map((id) => SecureStore.deleteItemAsync(this.taskKey(id))));
  }

  private async migrateLegacyQueue(): Promise<void> {
    if (legacyQueueChecked) {
      return;
    }
    legacyQueueChecked = true;
    try {
      const raw = await AsyncStorage.getItem(LEGACY_QUEUE_KEY);
      if (!raw) {
        return;
      }
      const parsed = JSON.parse(raw);
      if (Array.isArray(parsed) && parsed.length > 0) {
        const tasks = parsed.filter(
          (task): task is DocumentUploadTask => !!task && typeof task.id === 'string'
        );
        if (tasks.length > 0) {
          await this.persist(tasks);
        }
      }
      await AsyncStorage.removeItem(LEGACY_QUEUE_KEY);
    } catch {
      // Keep the legacy blob for a later retry: a SecureStore failure must not
      // silently destroy the queue. It stays plaintext only until migration
      // succeeds, which is still better than the old permanent plaintext state.
      legacyQueueChecked = false;
    }
  }

  async getTasks(): Promise<DocumentUploadTask[]> {
    try {
      await this.migrateLegacyQueue();
      const ids = await this.readIndex();
      const entries = await Promise.all(
        ids.map(async (id) => {
          try {
            const raw = await SecureStore.getItemAsync(this.taskKey(id));
            if (!raw) {
              return null;
            }
            const task = JSON.parse(raw) as DocumentUploadTask;
            // The index is untrusted after an interrupted write; never return
            // a task whose stored id disagrees with its key.
            return task && task.id === id ? task : null;
          } catch {
            return null;
          }
        })
      );
      return entries.filter((task): task is DocumentUploadTask => task !== null);
    } catch {
      return [];
    }
  }

  async saveTasks(tasks: DocumentUploadTask[]): Promise<void> {
    try {
      await this.persist(tasks);
    } catch {}
  }

  async upsertTask(task: DocumentUploadTask): Promise<void> {
    const tasks = await this.getTasks();
    const idx = tasks.findIndex((t) => t.id === task.id);
    if (idx >= 0) {
      tasks[idx] = task;
    } else {
      tasks.push(task);
    }
    await this.saveTasks(tasks);
  }

  async deleteTask(taskId: string): Promise<void> {
    const tasks = await this.getTasks();
    const filtered = tasks.filter((t) => t.id !== taskId);
    await this.saveTasks(filtered);
  }

  async cleanupTemporaryFile(localUri: string): Promise<void> {
    if (!localUri || !localUri.startsWith(FileSystem.cacheDirectory || 'file://')) {
      return;
    }
    try {
      const info = await FileSystem.getInfoAsync(localUri);
      if (info.exists) {
        await FileSystem.deleteAsync(localUri, { idempotent: true });
      }
    } catch {}
  }
}

export const localDocumentStore = new LocalDocumentStore();
