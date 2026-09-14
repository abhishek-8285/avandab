import React from 'react';
import { render, fireEvent, waitFor } from '@testing-library/react-native';
import { ForgotPasswordScreen } from '../src/components/ForgotPasswordScreen';

const globalFetch = global.fetch;

describe('ForgotPasswordScreen', () => {
  afterEach(() => {
    global.fetch = globalFetch;
    jest.restoreAllMocks();
  });

  const fillEmail = (getByPlaceholderText: any) =>
    fireEvent.changeText(getByPlaceholderText('driver@avandab.com'), 'driver@avandab.com');

  test('successful request shows submitted state', async () => {
    global.fetch = jest.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ ok: true, message: 'If an account exists...' }),
    }) as any;

    const onBackToLogin = jest.fn();
    const { getByPlaceholderText, getByText } = render(
      <ForgotPasswordScreen onBackToLogin={onBackToLogin} />
    );

    fillEmail(getByPlaceholderText);
    fireEvent.press(getByText('SEND RESET LINK'));

    await waitFor(() => expect(getByText('REQUEST SUBMITTED')).toBeTruthy());
    const body = JSON.parse((global.fetch as jest.Mock).mock.calls[0][1].body);
    expect(body).toEqual({ email: 'driver@avandab.com' });
  });

  test('network failure shows an inline error and stays on the form', async () => {
    global.fetch = jest.fn().mockRejectedValue(new Error('Offline connection failed')) as any;

    const { getByPlaceholderText, getByText, queryByText } = render(
      <ForgotPasswordScreen onBackToLogin={jest.fn()} />
    );

    fillEmail(getByPlaceholderText);
    fireEvent.press(getByText('SEND RESET LINK'));

    await waitFor(() => expect(getByText('Offline connection failed')).toBeTruthy());
    // Honest failure: no fake success screen.
    expect(queryByText('REQUEST SUBMITTED')).toBeNull();
  });

  test('server error response surfaces the API message inline', async () => {
    global.fetch = jest.fn().mockResolvedValue({
      ok: false,
      status: 400,
      json: async () => ({ error: 'email is required' }),
    }) as any;

    const { getByPlaceholderText, getByText } = render(
      <ForgotPasswordScreen onBackToLogin={jest.fn()} />
    );

    fillEmail(getByPlaceholderText);
    fireEvent.press(getByText('SEND RESET LINK'));

    await waitFor(() => expect(getByText('email is required')).toBeTruthy());
  });
});
