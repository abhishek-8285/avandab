import React from 'react';
import { render, fireEvent, waitFor, act } from '@testing-library/react-native';
import { VoiceKharchaSheet } from '../src/components/VoiceKharchaSheet';
import { OfflineQueue } from '../src/services/offlineQueue';
import { resetSQLiteMockState } from '../jest/setup';

describe('VoiceKharchaSheet', () => {
  beforeEach(async () => {
    resetSQLiteMockState();
    await OfflineQueue.init();
  });

  test('renders voice sheet when visible', () => {
    const onClose = jest.fn();
    const { getByText } = render(
      <VoiceKharchaSheet visible={true} onClose={onClose} tripId="TRP-1234" />
    );

    expect(getByText('VOICE KHARCHA (आवाज़ से खर्चा)')).toBeTruthy();
    expect(getByText('OR TAP A PRESET PHRASE:')).toBeTruthy();
  });

  test('parses test phrase on tap and enqueues expense', async () => {
    const onClose = jest.fn();
    const onSaved = jest.fn();

    const { getByText, getByDisplayValue } = render(
      <VoiceKharchaSheet
        visible={true}
        onClose={onClose}
        tripId="TRP-1234"
        onSaved={onSaved}
      />
    );

    // Tap the preset suggestion: '⛽ HPCL Diesel ₹2500'
    const chip = getByText('⛽ HPCL Diesel ₹2500');
    fireEvent.press(chip);

    // Expect parsed card to show extracted amount
    expect(getByText('RECORDED AUDIO & EXTRACTED DATA')).toBeTruthy();
    expect(getByDisplayValue('2500')).toBeTruthy();
    expect(getByText('HPCL')).toBeTruthy();

    // Confirm and Save
    const saveBtn = getByText('CONFIRM & ATTACH VOICE TO PASSBOOK');
    await act(async () => {
      fireEvent.press(saveBtn);
    });

    await waitFor(async () => {
      const pending = await OfflineQueue.pendingExpenses();
      expect(pending.length).toBeGreaterThan(0);
      expect(pending[0].amount).toBe(2500);
      expect(pending[0].expense_type).toBe('fuel');
    });
  });
});
