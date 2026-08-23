// Bilingual integrity gate: en.json and hi.json must expose IDENTICAL key sets.
// Guards against translation drift when either file gains or loses keys.
import en from '../src/locales/en.json';
import hi from '../src/locales/hi.json';

type Nested = Record<string, unknown>;

function flatten(obj: Nested, prefix = ''): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [key, value] of Object.entries(obj)) {
    const path = prefix ? `${prefix}.${key}` : key;
    if (value && typeof value === 'object') {
      Object.assign(out, flatten(value as Nested, path));
    } else {
      out[path] = String(value);
    }
  }
  return out;
}

const enFlat = flatten(en as Nested);
const hiFlat = flatten(hi as Nested);

describe('Locale parity (en ⇄ hi)', () => {
  it('en.json has no keys missing from hi.json', () => {
    const missingInHi = Object.keys(enFlat).filter((k) => !(k in hiFlat));
    if (missingInHi.length > 0) {
      throw new Error(
        `hi.json is missing ${missingInHi.length} key(s) present in en.json:\n  ${missingInHi.join('\n  ')}`
      );
    }
    expect(missingInHi).toEqual([]);
  });

  it('hi.json has no keys missing from en.json', () => {
    const missingInEn = Object.keys(hiFlat).filter((k) => !(k in enFlat));
    if (missingInEn.length > 0) {
      throw new Error(
        `en.json is missing ${missingInEn.length} key(s) present in hi.json:\n  ${missingInEn.join('\n  ')}`
      );
    }
    expect(missingInEn).toEqual([]);
  });

  it('every hi value is a non-empty string', () => {
    const empties = Object.entries(hiFlat)
      .filter(([, v]) => typeof v !== 'string' || v.trim().length === 0)
      .map(([k]) => k);
    expect(empties).toEqual([]);
  });

  it('hi translations are genuinely Hindi (Devanagari present across the file)', () => {
    const values = Object.values(hiFlat);
    const devanagariCount = values.filter((v) => /[\u0900-\u097F]/.test(v)).length;
    // Wholesale English copy-paste would push this to ~0; real translations
    // cover nearly every string, so require a strong majority.
    expect(devanagariCount).toBeGreaterThanOrEqual(Math.floor(values.length * 0.8));
  });

  it('interpolation placeholders match between locales per key', () => {
    const placeholderRe = /\{\{(\w+)\}\}/g;
    const mismatches: string[] = [];
    for (const key of Object.keys(enFlat)) {
      const enP = (enFlat[key].match(placeholderRe) ?? []).sort();
      const hiP = ((hiFlat[key] ?? '').match(placeholderRe) ?? []).sort();
      if (JSON.stringify(enP) !== JSON.stringify(hiP)) mismatches.push(key);
    }
    if (mismatches.length > 0) {
      throw new Error(`Placeholder mismatch for key(s):\n  ${mismatches.join('\n  ')}`);
    }
    expect(mismatches).toEqual([]);
  });
});
