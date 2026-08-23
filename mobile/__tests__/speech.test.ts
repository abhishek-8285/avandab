import {
  buildExpenseDraft,
  parseExpenseUtterance,
  NoOpSpeechProvider,
  SpeechProvider,
} from '../src/services/speech';

describe('parseExpenseUtterance — amounts', () => {
  test('English spec example parses exactly', () => {
    const p = parseExpenseUtterance('Diesel ₹2500 at HPCL pump near Pune station');
    expect(p.amount).toBe(2500);
    expect(p.vendor).toBe('HPCL');
    expect(p.category).toBe('fuel');
    expect(p.dateHint).toBeNull();
  });

  test('Devanagari digits convert (₹२५००)', () => {
    const p = parseExpenseUtterance('₹२५०० का डीज़ल भरवाया');
    expect(p.amount).toBe(2500);
    expect(p.category).toBe('fuel');
  });

  test('comma-separated amount strips separator', () => {
    const p = parseExpenseUtterance('diesel ₹2,500 daala');
    expect(p.amount).toBe(2500);
  });

  test('Rs 300 toll NHAI', () => {
    const p = parseExpenseUtterance('Rs 300 toll NHAI');
    expect(p.amount).toBe(300);
    expect(p.category).toBe('toll');
    expect(p.vendor).toBe('NHAI');
  });

  test('Rs. with dot prefix', () => {
    expect(parseExpenseUtterance('Rs.2500 petrol').amount).toBe(2500);
  });

  test('INR prefix', () => {
    expect(parseExpenseUtterance('INR 2500 diesel bharwaya').amount).toBe(2500);
  });

  test('trailing "rupees"', () => {
    expect(parseExpenseUtterance('2500 rupees diesel').amount).toBe(2500);
  });

  test('Hinglish "rupaye" trailing', () => {
    const p = parseExpenseUtterance('500 rupaye ka khana');
    expect(p.amount).toBe(500);
    expect(p.category).toBe('food');
  });

  test('no amount yields null', () => {
    const p = parseExpenseUtterance('toll lag gaya aaj');
    expect(p.amount).toBeNull();
  });
});

describe('parseExpenseUtterance — categories', () => {
  test('diesel/petrol/डीज़ल/पेट्रोल → fuel', () => {
    expect(parseExpenseUtterance('petrol bharwaya').category).toBe('fuel');
    expect(parseExpenseUtterance('डीज़ल ₹100').category).toBe('fuel');
    expect(parseExpenseUtterance('पेट्रोल ₹200').category).toBe('fuel');
  });

  test('toll/toll tax/टोल → toll', () => {
    expect(parseExpenseUtterance('toll tax paid').category).toBe('toll');
    expect(parseExpenseUtterance('टोल ₹120').category).toBe('toll');
  });

  test('repair/puncture/mechanic/पंचर → repair', () => {
    expect(parseExpenseUtterance('puncture repair 500').category).toBe('repair');
    expect(parseExpenseUtterance('mechanic ko ₹800 diye').category).toBe('repair');
    expect(parseExpenseUtterance('पंचर ₹150').category).toBe('repair');
    expect(parseExpenseUtterance('मरम्मत ₹900').category).toBe('repair');
  });

  test('tyre puncher / टायर → tyre (wins over repair keywords)', () => {
    expect(parseExpenseUtterance('tyre puncher lag gaya').category).toBe('tyre');
    expect(parseExpenseUtterance('टायर बदलवाया ₹3000').category).toBe('tyre');
    expect(parseExpenseUtterance('tyre puncture ho gaya').category).toBe('tyre');
  });

  test('challan/चालान → challan', () => {
    expect(parseExpenseUtterance('challan kata 1000').category).toBe('challan');
    expect(parseExpenseUtterance('चालान ₹500').category).toBe('challan');
  });

  test('parking/पार्किंग → parking', () => {
    expect(parseExpenseUtterance('parking fee 50').category).toBe('parking');
    expect(parseExpenseUtterance('पार्किंग ₹30').category).toBe('parking');
  });

  test('food/chai/khana/खाना/चाय → food', () => {
    expect(parseExpenseUtterance('chai ₹20').category).toBe('food');
    expect(parseExpenseUtterance('खाना ₹120').category).toBe('food');
    expect(parseExpenseUtterance('चाय समोसा ₹45').category).toBe('food');
  });

  test('default other when no keyword hits', () => {
    expect(parseExpenseUtterance('misc kharcha ₹99').category).toBe('other');
  });
});

describe('parseExpenseUtterance — vendor', () => {
  test('known network beats generic at-capture', () => {
    // Generic capture would yield "HPCL pump"; known network must win
    const p = parseExpenseUtterance('Diesel ₹2500 HPCL pump Pune');
    expect(p.vendor).toBe('HPCL');
  });

  test('at Shell petrol pump → Shell', () => {
    const p = parseExpenseUtterance('at Shell petrol pump bharwaya');
    expect(p.vendor).toBe('Shell');
  });

  test('Indian Oil canonicalizes casing', () => {
    expect(parseExpenseUtterance('indian oil pump pe ₹1000').vendor).toBe('Indian Oil');
  });

  test('generic at-capture for unknown vendor', () => {
    const p = parseExpenseUtterance('paid at Sharma Truck Station yesterday');
    expect(p.vendor).toBe('Sharma Truck');
  });

  test('generic at-capture stops at near/comma', () => {
    expect(parseExpenseUtterance('khana at dhaba near pune').vendor).toBe('dhaba');
  });

  test('no vendor signal → null', () => {
    expect(parseExpenseUtterance('chai ₹20').vendor).toBeNull();
  });

  test('Fastag and BPCL and IOCL recognized', () => {
    expect(parseExpenseUtterance('fastag se toll cut gaya').vendor).toBe('Fastag');
    expect(parseExpenseUtterance('BPCL pump ₹500').vendor).toBe('BPCL');
    expect(parseExpenseUtterance('iocl outlet ₹400').vendor).toBe('IOCL');
  });
});

describe('parseExpenseUtterance — dateHint', () => {
  test('today/aaj/आज → today', () => {
    expect(parseExpenseUtterance('aaj ka diesel').dateHint).toBe('today');
    expect(parseExpenseUtterance('toll today').dateHint).toBe('today');
    expect(parseExpenseUtterance('आज का खर्चा ₹50').dateHint).toBe('today');
  });

  test('tomorrow/kal/कल → tomorrow', () => {
    expect(parseExpenseUtterance('kal challan bharna hai').dateHint).toBe('tomorrow');
    expect(parseExpenseUtterance('parking tomorrow ₹50').dateHint).toBe('tomorrow');
    expect(parseExpenseUtterance('कल का टोल').dateHint).toBe('tomorrow');
  });

  test('no date word → null', () => {
    expect(parseExpenseUtterance('diesel ₹2500 HPCL').dateHint).toBeNull();
  });

  test('kal inside longer word does not trigger hint', () => {
    // token equality, not substring
    expect(parseExpenseUtterance('dekha kalpana nagar me ₹100 ka petrol').dateHint).toBeNull();
  });
});

describe('buildExpenseDraft', () => {
  test('maps parse result onto offline expense queue shape', () => {
    const draft = buildExpenseDraft(
      'Diesel ₹2500 at HPCL pump near Pune station',
      'trip_voice_1'
    );
    expect(draft.trip_id).toBe('trip_voice_1');
    expect(draft.expense_type).toBe('fuel');
    expect(draft.amount).toBe(2500);
    expect(draft.notes).toBe('Diesel ₹2500 at HPCL pump near Pune station');
  });

  test('null amount becomes 0 in draft, other preserved', () => {
    const draft = buildExpenseDraft('toll lag gaya aaj', 'trip_voice_2');
    expect(draft.amount).toBe(0);
    expect(draft.expense_type).toBe('toll');
  });

  test('idempotency_key is uuid-v4 formatted (36 chars, 4 dashes)', () => {
    const draft = buildExpenseDraft('chai ₹20', 'trip_uuid');
    const key = draft.idempotency_key;
    expect(key).toHaveLength(36);
    expect(key.match(/-/g)).toHaveLength(4);
    expect(key[14]).toBe('4'); // version nibble
    expect(/^[0-9a-f-]+$/.test(key)).toBe(true);
  });

  test('two drafts get distinct idempotency keys', () => {
    const a = buildExpenseDraft('chai ₹20', 't1');
    const b = buildExpenseDraft('chai ₹20', 't1');
    expect(a.idempotency_key).not.toBe(b.idempotency_key);
  });
});

describe('SpeechProvider interface', () => {
  test('NoOpSpeechProvider rejects with SPEECH_PROVIDER_UNAVAILABLE', async () => {
    const provider: SpeechProvider = new NoOpSpeechProvider();
    await expect(provider.transcribe('file:///audio.m4a', 'hi-IN')).rejects.toThrow(
      'SPEECH_PROVIDER_UNAVAILABLE'
    );
  });
});
