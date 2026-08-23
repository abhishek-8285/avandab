import {
  scanReceipt,
  buildExpenseFromReceipt,
  normalizeReceiptText,
  NoOpReceiptOcrProvider,
  ReceiptOcrProvider,
} from '../src/services/receiptScan';

describe('normalizeReceiptText', () => {
  test('collapses newlines, tabs and runs of spaces to single spaces', () => {
    expect(normalizeReceiptText('HPCL PETROL\n\nPUMP \t ₹100')).toBe('HPCL PETROL PUMP ₹100');
    expect(normalizeReceiptText('  a   b  ')).toBe('a b');
  });

  test('whitespace-only input becomes empty string', () => {
    expect(normalizeReceiptText('\n \t')).toBe('');
  });
});

describe('scanReceipt', () => {
  test('provider receives the image uri', async () => {
    const provider: ReceiptOcrProvider = {
      extractText: jest.fn().mockResolvedValue('chai ₹20'),
    };

    await scanReceipt('file:///receipt.jpg', provider);

    expect(provider.extractText).toHaveBeenCalledWith('file:///receipt.jpg');
  });

  test('multiline receipt parses amount/vendor/category after normalize', async () => {
    const provider: ReceiptOcrProvider = {
      extractText: jest.fn().mockResolvedValue('HPCL PETROL PUMP\n₹2,450\n12/08/2026'),
    };

    const parsed = await scanReceipt('file:///fuel.jpg', provider);

    expect(parsed.amount).toBe(2450);
    expect(parsed.vendor).toBe('HPCL');
    expect(parsed.category).toBe('fuel');
  });

  test('NoOpReceiptOcrProvider rejects with OCR_PROVIDER_UNAVAILABLE', async () => {
    const provider = new NoOpReceiptOcrProvider();
    await expect(scanReceipt('file:///x.jpg', provider)).rejects.toThrow(
      'OCR_PROVIDER_UNAVAILABLE'
    );
  });
});

describe('buildExpenseFromReceipt', () => {
  test('mirrors buildExpenseDraft shape on normalized text', () => {
    const draft = buildExpenseFromReceipt(
      'HPCL PETROL PUMP\n₹2,450',
      'trip_r_1'
    );

    expect(draft.trip_id).toBe('trip_r_1');
    expect(draft.expense_type).toBe('fuel');
    expect(draft.amount).toBe(2450);
    expect(draft.notes).toBe('HPCL PETROL PUMP ₹2,450');
  });

  test('idempotency_key is uuid-v4 formatted (36 chars)', () => {
    const draft = buildExpenseFromReceipt('toll ₹120', 'trip_r_2');
    expect(draft.idempotency_key).toHaveLength(36);
  });
});
