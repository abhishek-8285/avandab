import React from 'react';
import { Alert } from 'react-native';
import { fireEvent, render, waitFor } from '@testing-library/react-native';
import { VoiceExpenseButton } from '../src/components/VoiceExpenseButton';
import { OfflineQueue } from '../src/services/offlineQueue';

describe('VoiceExpenseButton', () => {
  // Re-created per test: global restoreAllMocks() detaches spies after each run.
  let enqueueSpy: jest.SpyInstance;

  beforeEach(() => {
    enqueueSpy = jest.spyOn(OfflineQueue, 'enqueueExpense').mockResolvedValue(undefined);
  });

  test('parses "Diesel ₹2500 at HPCL" and queues the expense offline', async () => {
    const onSaved = jest.fn();
    const { getByLabelText, getByPlaceholderText, getByText } = render(
      <VoiceExpenseButton tripId={null} onSaved={onSaved} />
    );

    fireEvent.press(getByLabelText('voice-expense-mic'));
    fireEvent.changeText(getByPlaceholderText('voice.hint'), 'Diesel ₹2500 at HPCL');
    fireEvent.press(getByText('expense.submit'));

    await waitFor(() => expect(enqueueSpy).toHaveBeenCalledTimes(1));

    const arg = enqueueSpy.mock.calls[0][0];
    expect(arg.trip_id).toBe('');
    expect(arg.amount).toBe(2500);
    expect(arg.expense_type).toBe('fuel');
    expect(arg.notes).toBe('Diesel ₹2500 at HPCL');
    expect(arg.idempotency_key).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i
    );
    expect(onSaved).toHaveBeenCalledTimes(1);
  });

  test('empty submit does not enqueue or fire onSaved', () => {
    const onSaved = jest.fn();
    const { getByLabelText, getByText } = render(
      <VoiceExpenseButton tripId="trip_1" onSaved={onSaved} />
    );

    fireEvent.press(getByLabelText('voice-expense-mic'));
    fireEvent.press(getByText('expense.submit'));

    expect(enqueueSpy).not.toHaveBeenCalled();
    expect(onSaved).not.toHaveBeenCalled();
  });

  test('disabled prop renders non-interactive control that cannot open panel', () => {
    const onSaved = jest.fn();
    const { getByLabelText, queryByPlaceholderText } = render(
      <VoiceExpenseButton tripId="trip_1" onSaved={onSaved} disabled />
    );

    fireEvent.press(getByLabelText('voice-expense-mic'));
    expect(queryByPlaceholderText('voice.hint')).toBeNull();
    expect(enqueueSpy).not.toHaveBeenCalled();
  });

  test('enqueue failure alerts a generic error and skips onSaved', async () => {
    const alertSpy = jest.spyOn(Alert, 'alert').mockImplementation(() => {});
    const onSaved = jest.fn();
    enqueueSpy.mockRejectedValue(new Error('db locked'));
    const { getByLabelText, getByPlaceholderText, getByText } = render(
      <VoiceExpenseButton tripId="trip_1" onSaved={onSaved} />
    );

    fireEvent.press(getByLabelText('voice-expense-mic'));
    fireEvent.changeText(getByPlaceholderText('voice.hint'), 'Toll ₹200 at NHAI');
    fireEvent.press(getByText('expense.submit'));

    await waitFor(() => expect(alertSpy).toHaveBeenCalled());
    expect(onSaved).not.toHaveBeenCalled();
  });
});
