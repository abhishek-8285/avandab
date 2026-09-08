import { resetSQLiteMockState } from '../jest/setup';

// The upgrade ALTERs run once per process inside initDatabase. When columns
// already exist (normal restart path) every ALTER throws — init must swallow
// and continue, otherwise the app loses all offline storage on every launch
// after the first upgrade.
describe('offline_gps_logs upgrade path', () => {
  beforeEach(() => {
    resetSQLiteMockState();
    jest.resetModules();
  });

  test('ALTER failures (columns already exist) do not break init', async () => {
    const SQLite = require('expo-sqlite');
    const execAsync = jest
      .fn()
      .mockResolvedValueOnce(undefined) // initial CREATE TABLE batch succeeds
      .mockRejectedValue(new Error('duplicate column name')); // every ALTER: columns already exist
    const runAsync = jest.fn().mockResolvedValue(undefined);
    (SQLite.openDatabaseAsync as jest.Mock).mockResolvedValueOnce({ execAsync, runAsync, getAllAsync: jest.fn().mockResolvedValue([]) });
    const { DB } = require('../src/services/storage');
    await expect(DB.logGPSLocation(1, 2, 3)).resolves.toBeUndefined();
    // 1 PRAGMA/DDL batch + 1 accuracy ALTER + 4 parity ALTERs.
    expect(execAsync.mock.calls.length).toBeGreaterThanOrEqual(6);
  });
});
