import AsyncStorage from '@react-native-async-storage/async-storage';
import * as FileSystem from 'expo-file-system';
import { DocumentUploadTask } from '../types/document';

const UPLOAD_QUEUE_STORAGE_KEY = 'avandab_doc_upload_queue_v1';

export class LocalDocumentStore {
  async getTasks(): Promise<DocumentUploadTask[]> {
    try {
      const json = await AsyncStorage.getItem(UPLOAD_QUEUE_STORAGE_KEY);
      return json ? JSON.parse(json) : [];
    } catch {
      return [];
    }
  }

  async saveTasks(tasks: DocumentUploadTask[]): Promise<void> {
    try {
      await AsyncStorage.setItem(UPLOAD_QUEUE_STORAGE_KEY, JSON.stringify(tasks));
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
