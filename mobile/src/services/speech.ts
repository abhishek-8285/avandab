// Vernacular voice expense parsing (offline, on-device NER-style).
// STT itself is pluggable via SpeechProvider; this module owns the
// deterministic parsing layer so it can be unit-tested without audio.

export interface SpeechProvider {
  transcribe(audioUri: string, lang: string): Promise<string>;
}

export class NoOpSpeechProvider implements SpeechProvider {
  async transcribe(_audioUri: string, _lang: string): Promise<string> {
    throw new Error('SPEECH_PROVIDER_UNAVAILABLE');
  }
}

export type ExpenseCategory =
  | 'fuel'
  | 'toll'
  | 'repair'
  | 'tyre'
  | 'challan'
  | 'parking'
  | 'food'
  | 'other';

export interface ParsedExpense {
  amount: number | null;
  vendor: string | null;
  category: ExpenseCategory;
  dateHint: 'today' | 'tomorrow' | null;
}

const DEVANAGARI_DIGITS: Record<string, string> = {
  '०': '0', '१': '1', '२': '2', '३': '3', '४': '4',
  '५': '5', '६': '6', '७': '7', '८': '8', '९': '9',
};

// Ordered — first hit wins. tyre before repair so "tyre puncture" → tyre.
const CATEGORY_KEYWORDS: [ExpenseCategory, string[]][] = [
  ['fuel', ['fuel', 'diesel', 'petrol', 'डीज़ल', 'पेट्रोल']],
  ['toll', ['toll tax', 'toll', 'टोल']],
  ['tyre', ['tyre', 'टायर']],
  ['repair', ['repair', 'puncture', 'mechanic', 'मरम्मत', 'पंचर']],
  ['challan', ['challan', 'चालान']],
  ['parking', ['parking', 'पार्किंग']],
  ['food', ['food', 'chai', 'khana', 'खाना', 'चाय']],
];

// Known fuel/toll networks take precedence over generic "at X" capture.
const KNOWN_NETWORKS: [RegExp, string][] = [
  [/indian\s*oil/i, 'Indian Oil'],
  [/hpcl/i, 'HPCL'],
  [/bpcl/i, 'BPCL'],
  [/iocl/i, 'IOCL'],
  [/reliance/i, 'Reliance'],
  [/shell/i, 'Shell'],
  [/nhai/i, 'NHAI'],
  [/fastag/i, 'Fastag'],
];

const AMOUNT_PATTERNS = [
  /(?:₹|rs\.?|inr)\s*(\d+(?:\.\d+)?)/i,
  /(\d+(?:\.\d+)?)\s*(?:rupees?|rupaye|रुपये)/i,
];

function normalizeText(text: string): string {
  let out = text.replace(/[०-९]/g, (d) => DEVANAGARI_DIGITS[d] ?? d);
  // "₹2,500" / "Rs 2,500" — strip thousands separators inside numbers.
  out = out.replace(/(\d),(\d)/g, '$1$2');
  return out;
}

export function parseExpenseUtterance(text: string, _now?: Date): ParsedExpense {
  const normalized = normalizeText(text);

  // ── Amount: first currency-marked number wins ──
  let amount: number | null = null;
  for (const pattern of AMOUNT_PATTERNS) {
    const m = normalized.match(pattern);
    if (m) {
      amount = parseFloat(m[1]);
      break;
    }
  }

  // ── Category: ordered keyword map ──
  let category: ExpenseCategory = 'other';
  const lower = normalized.toLowerCase();
  for (const [cat, keywords] of CATEGORY_KEYWORDS) {
    if (keywords.some((k) => lower.includes(k))) {
      category = cat;
      break;
    }
  }

  // ── Vendor: known network first, else generic "at X" capture ──
  let vendor: string | null = null;
  for (const [pattern, name] of KNOWN_NETWORKS) {
    if (pattern.test(normalized)) {
      vendor = name;
      break;
    }
  }
  if (!vendor) {
    const atMatch = normalized.match(
      /\bat ([A-Za-z\u0900-\u097F][\w\s]{0,30}?)( pump| station| toll| near|,|\.|$)/i
    );
    if (atMatch) {
      vendor = atMatch[1].trim();
    }
  }

  // ── Date hint: token equality avoids Devanagari \b pitfalls ──
  let dateHint: ParsedExpense['dateHint'] = null;
  const tokens = lower.split(/[^a-z\u0900-\u097F]+/).filter(Boolean);
  if (tokens.some((t) => t === 'today' || t === 'aaj' || t === 'आज')) {
    dateHint = 'today';
  } else if (tokens.some((t) => t === 'tomorrow' || t === 'kal' || t === 'कल')) {
    dateHint = 'tomorrow';
  }

  return { amount, vendor, category, dateHint };
}

// Local uuid v4 — no external dependency.
function uuidV4(): string {
  const bytes = new Uint8Array(16);
  const g = globalThis.crypto as Crypto | undefined;
  if (g && typeof g.getRandomValues === 'function') {
    g.getRandomValues(bytes);
  } else {
    for (let i = 0; i < 16; i++) bytes[i] = Math.floor(Math.random() * 256);
  }
  bytes[6] = (bytes[6] & 0x0f) | 0x40; // version 4
  bytes[8] = (bytes[8] & 0x3f) | 0x80; // variant 10xx
  const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

// Shape mirrors OfflineQueue.enqueueExpense's insert payload.
export function buildExpenseDraft(
  text: string,
  tripId: string,
  now?: Date
): {
  trip_id: string;
  expense_type: ExpenseCategory;
  amount: number;
  notes: string;
  idempotency_key: string;
} {
  const parsed = parseExpenseUtterance(text, now);
  return {
    trip_id: tripId,
    expense_type: parsed.category,
    amount: parsed.amount ?? 0,
    notes: text,
    idempotency_key: uuidV4(),
  };
}
