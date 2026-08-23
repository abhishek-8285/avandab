import * as SQLite from 'expo-sqlite';
import { ConsentManager } from '../src/services/consentManager';
import { getSQLiteMockState, resetSQLiteMockState } from '../jest/setup';

describe('ConsentManager (DPDP Act 2023)', () => {
  let db: any;

  beforeAll(async () => {
    // Setup mocks expo-sqlite so every open resolves the same shared mock db
    db = await SQLite.openDatabaseAsync('consent.db');
  });

  beforeEach(async () => {
    resetSQLiteMockState();
    await ConsentManager.init();
  });

  const runAsyncCalls = () =>
    (db.runAsync as jest.Mock).mock.calls.map((c) => ({ query: String(c[0]), params: c[1] }));

  test('init creates consent_log table via execAsync', async () => {
    (ConsentManager as any).db = null; // force re-init so execAsync fires again
    await ConsentManager.init();
    const execCalls = (db.execAsync as jest.Mock).mock.calls.map((c) => String(c[0]));
    expect(execCalls.some((sql) => sql.includes('CREATE TABLE IF NOT EXISTS consent_log'))).toBe(true);
    expect(execCalls.some((sql) => sql.includes("DEFAULT (datetime('now'))"))).toBe(true);
  });

  test('recordConsent inserts row with correct params order', async () => {
    await ConsentManager.recordConsent('location', true);
    const inserts = runAsyncCalls().filter((c) => c.query.includes('INSERT INTO consent_log'));
    expect(inserts).toHaveLength(1);
    expect(inserts[0].params).toEqual(['location', 'granted']);

    await ConsentManager.recordConsent('camera', false);
    const insertsAfter = runAsyncCalls().filter((c) => c.query.includes('INSERT INTO consent_log'));
    expect(insertsAfter[1].params).toEqual(['camera', 'denied']);
  });

  test('recordConsent never throws when sqlite fails', async () => {
    const warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => {});
    (db.runAsync as jest.Mock).mockImplementationOnce(async () => {
      throw new Error('disk full');
    });
    await expect(ConsentManager.recordConsent('microphone', true)).resolves.toBeUndefined();
    expect(warnSpy).toHaveBeenCalled();
    warnSpy.mockRestore();
  });

  test('getConsentHistory returns rows newest-first', async () => {
    await ConsentManager.recordConsent('location', true);
    await ConsentManager.recordConsent('camera', false);

    const history = await ConsentManager.getConsentHistory();
    expect(history).toHaveLength(2);
    expect(history[0].purpose).toBe('camera');
    expect(history[0].user_response).toBe('denied');
    expect(history[1].purpose).toBe('location');
    expect(typeof history[1].timestamp).toBe('string');
    expect(history.every((r) => typeof r.id === 'number')).toBe(true);
  });

  test('getConsentHistory issues ORDER BY DESC query', async () => {
    await ConsentManager.getConsentHistory();
    const selects = (db.getAllAsync as jest.Mock).mock.calls
      .map((c) => String(c[0]))
      .filter((q) => q.includes('SELECT') && q.includes('consent_log'));
    expect(selects[0].toUpperCase()).toContain('DESC');
  });

  test('hasGranted true only when LATEST record for purpose is granted', async () => {
    await ConsentManager.recordConsent('microphone', false);
    await ConsentManager.recordConsent('microphone', true);
    await expect(ConsentManager.hasGranted('microphone')).resolves.toBe(true);

    // A later denial supersedes the earlier grant
    await ConsentManager.recordConsent('microphone', false);
    await expect(ConsentManager.hasGranted('microphone')).resolves.toBe(false);
  });

  test('hasGranted false for unknown purpose or denied-only purpose', async () => {
    await ConsentManager.recordConsent('camera', false);
    await expect(ConsentManager.hasGranted('camera')).resolves.toBe(false);
    await expect(ConsentManager.hasGranted('data_processing')).resolves.toBe(false);
  });

  test('purgeExpired issues DELETE with -3 years clause and removes stale rows', async () => {
    const state = getSQLiteMockState();
    state.consent_log.push({
      id: 999,
      purpose: 'location',
      user_response: 'granted',
      timestamp: '2020-01-01T00:00:00.000Z',
    });
    await ConsentManager.recordConsent('location', true);

    await ConsentManager.purgeExpired();

    const deletes = runAsyncCalls().filter((c) => c.query.includes('DELETE FROM consent_log'));
    expect(deletes).toHaveLength(1);
    expect(deletes[0].query).toContain('-3 years');
    expect(state.consent_log.some((c) => c.id === 999)).toBe(false);
    expect(state.consent_log.some((c) => c.purpose === 'location')).toBe(true);
  });

  test('exportConsentLog returns valid JSON of full history', async () => {
    await ConsentManager.recordConsent('data_processing', true);
    await ConsentManager.recordConsent('location', false);

    const json = await ConsentManager.exportConsentLog();
    const parsed = JSON.parse(json);
    expect(Array.isArray(parsed)).toBe(true);
    expect(parsed).toHaveLength(2);

    const history = await ConsentManager.getConsentHistory();
    expect(parsed).toEqual(history);
    expect(parsed[0]).toMatchObject({
      purpose: 'location',
      user_response: 'denied',
    });
  });
});
