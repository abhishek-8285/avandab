import * as fs from 'fs';
import * as path from 'path';

// Guards the class of bug where logGPSLocation INSERTed speed/heading/
// motion/battery_level while offline_gps_logs was created without them:
// the jest sqlite mock accepts anything, so only a schema-vs-usage
// cross-check of the real source catches it.
const SRC = fs.readFileSync(path.join(__dirname, '..', 'src', 'services', 'storage.ts'), 'utf8');

const SQL_KEYWORDS = new Set([
  'select', 'from', 'where', 'and', 'or', 'insert', 'into', 'values', 'update', 'set',
  'order', 'by', 'asc', 'desc', 'limit', 'as', 'not', 'null', 'default', 'if', 'exists',
  'create', 'table', 'alter', 'add', 'column', 'in',
]);

function schemaColumns(): Set<string> {
  const cols = new Set<string>();
  const create = SRC.match(/CREATE TABLE IF NOT EXISTS offline_gps_logs \(([\s\S]*?)\);/);
  if (create) {
    for (const line of create[1].split('\n')) {
      const m = line.trim().match(/^(\w+)\s+(INTEGER|REAL|TEXT)/i);
      if (m) cols.add(m[1].toLowerCase());
    }
  }
  for (const m of SRC.matchAll(/ALTER TABLE offline_gps_logs ADD COLUMN (\w+)/gi)) {
    cols.add(m[1].toLowerCase());
  }
  return cols;
}

function usedColumns(): Set<string> {
  const used = new Set<string>();
  // Statements mentioning offline_gps_logs: harvest identifiers from the
  // INSERT column list, SELECT list, and UPDATE assignments/WHERE.
  for (const m of SRC.matchAll(/INSERT INTO offline_gps_logs \((.*?)\)/gi)) {
    for (const c of m[1].split(',')) used.add(c.trim().toLowerCase());
  }
  for (const m of SRC.matchAll(/SELECT ([^`;]*?) FROM offline_gps_logs/gi)) {
    for (const part of m[1].split(',')) {
      const col = part.trim().split(/\s+AS\s+/i)[0].trim().split('.').pop()!;
      if (col !== '*') used.add(col.toLowerCase());
    }
  }
  for (const m of SRC.matchAll(/UPDATE offline_gps_logs SET (.*?)(?:WHERE|;)/gis)) {
    for (const assign of m[1].split(',')) {
      const col = assign.trim().split('=')[0].trim();
      if (col && !SQL_KEYWORDS.has(col.toLowerCase())) used.add(col.toLowerCase());
    }
  }
  const where = SRC.match(/UPDATE offline_gps_logs SET[\s\S]*?WHERE synced = 0([\s\S]*?);/i);
  void where;
  used.add('id');
  used.add('synced');
  return used;
}

describe('offline_gps_logs schema-vs-usage consistency', () => {
  test('every column written or read exists in CREATE TABLE or upgrade ADD COLUMN', () => {
    const schema = schemaColumns();
    expect(schema.size).toBeGreaterThan(0);
    const missing = [...usedColumns()].filter((c) => !schema.has(c) && !SQL_KEYWORDS.has(c));
    expect(missing).toEqual([]);
  });

  test('parity columns logGPSLocation writes are present', () => {
    const schema = schemaColumns();
    for (const c of ['speed', 'heading', 'motion', 'battery_level']) {
      expect(schema.has(c)).toBe(true);
    }
  });
});
