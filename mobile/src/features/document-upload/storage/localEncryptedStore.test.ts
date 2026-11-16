// Security ratchet for audit low 5 (2026-11-12): the document-upload queue
// claimed to be encrypted but wrote plaintext to AsyncStorage. This test
// asserts every queue value lands in SecureStore instead, and that the old
// plaintext blob is migrated once and then removed.
const mockSecureValues = new Map<string, string>();
const mockLegacyValues = new Map<string, string>();
const LEGACY_KEY = 'avandab_doc_upload_queue_v1';

jest.mock('expo-secure-store', () => ({
  getItemAsync: jest.fn(async (key: string) =>
    mockSecureValues.has(key) ? mockSecureValues.get(key)! : null
  ),
  setItemAsync: jest.fn(async (key: string, value: string) => {
    mockSecureValues.set(key, value);
  }),
  deleteItemAsync: jest.fn(async (key: string) => {
    mockSecureValues.delete(key);
  }),
}));

jest.mock('@react-native-async-storage/async-storage', () => ({
  getItem: jest.fn(async (key: string) =>
    mockLegacyValues.has(key) ? mockLegacyValues.get(key)! : null
  ),
  removeItem: jest.fn(async (key: string) => {
    mockLegacyValues.delete(key);
  }),
}));

jest.mock('expo-file-system/legacy', () => ({
  cacheDirectory: 'file:///cache/',
  getInfoAsync: jest.fn(async () => ({ exists: false })),
  deleteAsync: jest.fn(async () => undefined),
}));

import { LocalDocumentStore } from './localEncryptedStore';
import { DocumentUploadTask } from '../types/document';

function task(id: string): DocumentUploadTask {
  return {
    id,
    documentCategory: 'dl_front',
    localUri: 'file:///cache/dl.jpg',
    mimeType: 'image/jpeg',
    fileSizeBytes: 1024,
    clientSha256: 'a'.repeat(64),
    state: 'QUEUED',
    retryCount: 0,
    maxRetries: 3,
    progressPct: 0,
    createdAt: 1,
    updatedAt: 1,
  };
}

describe('LocalDocumentStore encrypted queue', () => {
  beforeEach(() => {
    mockSecureValues.clear();
    mockLegacyValues.clear();
  });

  it('migrates the legacy plaintext queue into SecureStore and removes it', async () => {
    mockLegacyValues.set(LEGACY_KEY, JSON.stringify([task('legacy_1')]));

    const store = new LocalDocumentStore();
    const tasks = await store.getTasks();

    expect(tasks.map((t) => t.id)).toEqual(['legacy_1']);
    expect(mockLegacyValues.has(LEGACY_KEY)).toBe(false);
    expect(Array.from(mockSecureValues.values()).join('|')).toContain('legacy_1');
  });

  it('round-trips upsert and delete through the encrypted index', async () => {
    const store = new LocalDocumentStore();
    await store.upsertTask(task('upload_1'));
    await store.upsertTask(task('upload_2'));

    const tasks = await store.getTasks();
    expect(tasks.map((t) => t.id).sort()).toEqual(['upload_1', 'upload_2']);

    await store.deleteTask('upload_1');
    const remaining = await store.getTasks();
    expect(remaining.map((t) => t.id)).toEqual(['upload_2']);
  });
});
