import fs from 'fs';
import path from 'path';

/**
 * P0 honesty ratchet (mobile): fabricated business data must never ship.
 * These literals were real fake values rendered as if they came from the
 * backend (demo plate, fake e-way bill, demo wallet balance, mock settlements,
 * hardcoded demo credentials). If one comes back, this test fails.
 */
const FORBIDDEN = [
  'DL-01-AB-1234',
  '#7291-8841-0294',
  '+91 98200 12345',
  '+91 98765 43210',
  'TRP-8491',
  'driver@avandab.com',
  'password123',
  'Tata Prima',
  'Industrial Goods',
  '18 Tons',
  'JNPT Port',
  'Chakan MIDC',
  'Tata AutoComp',
  '₹4,250',
  '₹18,500',
  'STL-9842',
  'STL-9811',
];

const SRC = path.join(__dirname, '..', 'src');

function walk(dir: string, out: string[] = []): string[] {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      walk(full, out);
    } else if (/\.(ts|tsx)$/.test(entry.name)) {
      out.push(full);
    }
  }
  return out;
}

describe('mobile honesty ratchet', () => {
  it('contains no fabricated business-data literals in source', () => {
    const offenders: string[] = [];
    for (const file of walk(SRC)) {
      const content = fs.readFileSync(file, 'utf8');
      for (const token of FORBIDDEN) {
        if (content.includes(token)) {
          offenders.push(`${path.relative(SRC, file)}: ${token}`);
        }
      }
    }
    expect(offenders).toEqual([]);
  });
});
