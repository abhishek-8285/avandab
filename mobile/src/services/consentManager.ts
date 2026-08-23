import * as SQLite from 'expo-sqlite';

// DPDP Act 2023 consent ledger.
// Consent artefacts retained 3 years per DPDP Rules; purgeExpired() enforces it.
const DB_NAME = 'consent.db';
const RETENTION_YEARS = 3;

export type ConsentPurpose = 'location' | 'camera' | 'microphone' | 'data_processing';

export interface ConsentRecord {
  id: number;
  purpose: string;
  user_response: 'granted' | 'denied';
  timestamp: string;
}

class ConsentManagerService {
  private db: SQLite.SQLiteDatabase | null = null;

  async init(): Promise<void> {
    if (this.db) return;
    this.db = await SQLite.openDatabaseAsync(DB_NAME);
    await this.db.execAsync(`
      CREATE TABLE IF NOT EXISTS consent_log (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        purpose TEXT NOT NULL,
        user_response TEXT NOT NULL,
        timestamp TEXT NOT NULL DEFAULT (datetime('now'))
      );
    `);
  }

  // Never throws — a failed consent write must not crash the UX flow it guards.
  async recordConsent(purpose: ConsentPurpose, granted: boolean): Promise<void> {
    try {
      if (!this.db) await this.init();
      await this.db!.runAsync(
        'INSERT INTO consent_log (purpose, user_response) VALUES (?, ?)',
        [purpose, granted ? 'granted' : 'denied']
      );
    } catch (err) {
      console.warn('[ConsentManager] failed to record consent:', err);
    }
  }

  // Newest-first audit trail (Data Principal right to access).
  async getConsentHistory(): Promise<ConsentRecord[]> {
    if (!this.db) await this.init();
    return await this.db!.getAllAsync<ConsentRecord>(
      'SELECT * FROM consent_log ORDER BY timestamp DESC, id DESC'
    );
  }

  // True only if the LATEST record for the purpose is a grant
  // (a later denial supersedes an earlier grant).
  async hasGranted(purpose: ConsentPurpose): Promise<boolean> {
    if (!this.db) await this.init();
    const row = await this.db!.getFirstAsync<{ user_response: string }>(
      'SELECT user_response FROM consent_log WHERE purpose = ? ORDER BY id DESC LIMIT 1',
      [purpose]
    );
    return row?.user_response === 'granted';
  }

  // DPDP Rules retention: delete rows older than 3 years.
  async purgeExpired(): Promise<void> {
    if (!this.db) await this.init();
    await this.db!.runAsync(
      `DELETE FROM consent_log WHERE timestamp < datetime('now','-${RETENTION_YEARS} years')`
    );
  }

  // Full history as JSON for Data Principal access/portability requests.
  async exportConsentLog(): Promise<string> {
    const history = await this.getConsentHistory();
    return JSON.stringify(history);
  }
}

export const ConsentManager = new ConsentManagerService();
// Default alias — App.tsx entrypoint imports the singleton as default.
export default ConsentManager;
