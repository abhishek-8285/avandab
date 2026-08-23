// ML Kit OCR adapter + expense parsing reuse from speech.ts.
import { buildExpenseDraft, parseExpenseUtterance, ParsedExpense } from './speech';

export interface ReceiptOcrProvider {
  extractText(imageUri: string): Promise<string>;
}

export class NoOpReceiptOcrProvider implements ReceiptOcrProvider {
  async extractText(_imageUri: string): Promise<string> {
    throw new Error('OCR_PROVIDER_UNAVAILABLE');
  }
}

// Receipt OCR output is multi-line; parser expects single-line utterance text
export function normalizeReceiptText(text: string): string {
  return text.replace(/\s+/g, ' ').trim();
}

export async function scanReceipt(
  imageUri: string,
  provider: ReceiptOcrProvider
): Promise<ParsedExpense> {
  const rawText = await provider.extractText(imageUri);
  return parseExpenseUtterance(normalizeReceiptText(rawText));
}

export function buildExpenseFromReceipt(text: string, tripId: string, now?: Date) {
  return buildExpenseDraft(normalizeReceiptText(text), tripId, now);
}
