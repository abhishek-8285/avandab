// Coverage strengthening for ConsentManager lazy-init guards and failure
// containment paths (recordConsent must never throw).
// Each scenario loads a FRESH singleton so its very first call exercises the
// `if (!this.db) await this.init()` guard that persistent singletons skip.
import { getSQLiteMockState, resetSQLiteMockState } from '../jest/setup';

type ConsentModule = typeof import('../src/services/consentManager');

async function freshConsent(failWritesWith?: Error): Promise<ConsentModule> {
  let mod!: ConsentModule;
  await jest.isolateModulesAsync(async () => {
    if (failWritesWith) {
      // eslint-disable-next-line @typescript-eslint/no-var-requires
      const SQLiteMod = require('expo-sqlite');
      const db = await (SQLiteMod.openDatabaseAsync as jest.Mock)('probe');
      (db.runAsync as jest.Mock).mockRejectedValue(failWritesWith);
    }
    // eslint-disable-next-line @typescript-eslint/no-var-requires
    mod = require('../src/services/consentManager') as ConsentModule;
  });
  return mod;
}

describe('ConsentManager lazy init + resilience', () => {
  let warnSpy: jest.SpyInstance;

  beforeEach(() => {
    resetSQLiteMockState();
    warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => {});
  });

  afterEach(() => {
    warnSpy.mockRestore();
  });

  test('first recordConsent call self-initializes the ledger', async () => {
    const { ConsentManager } = await freshConsent();

    await ConsentManager.recordConsent('microphone', true);

    const rows = getSQLiteMockState().consent_log;
    expect(rows).toHaveLength(1);
    expect(rows[0]).toMatchObject({ purpose: 'microphone', user_response: 'granted' });
  });

  test('failed consent writes are swallowed with a warning — never crash the guarded flow', async () => {
    const { ConsentManager } = await freshConsent(new Error('ledger locked'));

    await expect(ConsentManager.recordConsent('location', true)).resolves.toBeUndefined();
    expect(warnSpy).toHaveBeenCalledWith(
      '[ConsentManager] failed to record consent:',
      expect.any(Error)
    );
    expect(getSQLiteMockState().consent_log).toHaveLength(0);
  });

  test('hasGranted returns false for an empty ledger without explicit init', async () => {
    const { ConsentManager } = await freshConsent();

    await expect(ConsentManager.hasGranted('camera')).resolves.toBe(false);
  });

  test('latest denial supersedes an earlier grant per purpose', async () => {
    const { ConsentManager } = await freshConsent();

    await ConsentManager.recordConsent('location', true);
    await ConsentManager.recordConsent('location', false);

    await expect(ConsentManager.hasGranted('location')).resolves.toBe(false);
    // Other purposes remain independent.
    await expect(ConsentManager.hasGranted('camera')).resolves.toBe(false);
  });

  test('purgeExpired enforces the 3-year DPDP retention window on first use', async () => {
    const fourYearsAgo = new Date(Date.now() - 4 * 365.25 * 24 * 60 * 60 * 1000).toISOString();

    const { ConsentManager } = await freshConsent();
    const state = getSQLiteMockState();
    state.consent_log.push(
      { id: 1, purpose: 'location', user_response: 'granted', timestamp: fourYearsAgo },
      { id: 2, purpose: 'camera', user_response: 'granted', timestamp: new Date().toISOString() }
    );

    await ConsentManager.purgeExpired();

    const remaining = getSQLiteMockState().consent_log;
    expect(remaining).toHaveLength(1);
    expect(remaining[0].purpose).toBe('camera');
  });

  test('exportConsentLog self-initializes and serializes full history newest-first', async () => {
    const { ConsentManager } = await freshConsent();

    await ConsentManager.recordConsent('data_processing', true);
    await ConsentManager.recordConsent('microphone', false);

    const parsed = JSON.parse(await ConsentManager.exportConsentLog());

    expect(parsed.map((r: any) => r.purpose)).toEqual(['microphone', 'data_processing']);
    expect(parsed.map((r: any) => r.user_response)).toEqual(['denied', 'granted']);
  });
});
