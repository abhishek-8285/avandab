import React from 'react';
import { render, fireEvent, waitFor, act } from '@testing-library/react-native';
import { DeliveryVerificationScreen } from '../src/components/DeliveryVerificationScreen';
import { OfflineQueue } from '../src/services/offlineQueue';
import { resetSQLiteMockState } from '../jest/setup';
import { useAuthStore } from '../src/stores/authStore';
import { useLanguageStore } from '../src/stores/languageStore';

const globalFetch = global.fetch;

describe('DeliveryVerificationScreen', () => {
  beforeEach(async () => {
    resetSQLiteMockState();
    await OfflineQueue.init();
    await useAuthStore.getState().setAuth('mock_token_123', {
      id: 'u_1',
      name: 'Rajesh Kumar',
      role: 'driver',
      email: 'driver@avandab.com',
      driverId: 'drv_1',
    });
  });

  afterEach(() => {
    global.fetch = globalFetch;
  });

  test('renders Hindi header and progress strip when locale is hi', async () => {
    await useLanguageStore.getState().setLanguage('hi');
    try {
      const { getByText } = render(
        <DeliveryVerificationScreen tripId="trip_hi" onComplete={jest.fn()} onBack={jest.fn()} />
      );
      expect(getByText('डिलीवरी प्रमाण (e-POD)')).toBeTruthy();
      expect(getByText('0/3 प्रमाण लगे')).toBeTruthy();
    } finally {
      await useLanguageStore.getState().setLanguage('en');
    }
  });

  test('submits multipart form to /api/v1/trips/{tripId}/deliver-pod on confirm with OTP', async () => {
    const onComplete = jest.fn();
    const onBack = jest.fn();

    const fetchMock = jest.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ trip_number: 'TRP-8492', status: 'delivered', pod_url: '' }),
    });
    global.fetch = fetchMock as any;

    const { getByText, getByPlaceholderText } = render(
      <DeliveryVerificationScreen tripId="trip_99" onComplete={onComplete} onBack={onBack} />
    );

    // Switch to OTP mode
    fireEvent.press(getByText('🔢 4-Digit OTP'));

    const otpInput = getByPlaceholderText('e.g. 1234');
    fireEvent.changeText(otpInput, '4819');

    const submitBtn = getByText('CONFIRM DELIVERY & CLOSE TRIP');
    await act(async () => {
      fireEvent.press(submitBtn);
    });

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalled();
    });

    const calledUrl = fetchMock.mock.calls[0][0];
    expect(calledUrl).toContain('/api/v1/trips/trip_99/deliver-pod');
  });

  test('renders header and auto-verified consignee details', () => {
    const onComplete = jest.fn();
    const onBack = jest.fn();

    const { getByText } = render(
      <DeliveryVerificationScreen tripId="trip_back" onComplete={onComplete} onBack={onBack} />
    );

    expect(getByText('PROOF OF DELIVERY (e-POD)')).toBeTruthy();
    expect(getByText('Tata AutoComp Systems Ltd')).toBeTruthy();
  });

  test('falls back to OfflineQueue.enqueuePOD on network failure', async () => {
    const onComplete = jest.fn();
    const onBack = jest.fn();

    global.fetch = jest.fn().mockRejectedValue(new Error('Offline connection failed')) as any;

    const { getByText, getByPlaceholderText } = render(
      <DeliveryVerificationScreen tripId="trip_offline_77" onComplete={onComplete} onBack={onBack} />
    );

    // Switch to OTP mode
    fireEvent.press(getByText('🔢 4-Digit OTP'));

    const otpInput = getByPlaceholderText('e.g. 1234');
    fireEvent.changeText(otpInput, '1234');

    const submitBtn = getByText('CONFIRM DELIVERY & CLOSE TRIP');
    await act(async () => {
      fireEvent.press(submitBtn);
    });

    await waitFor(async () => {
      const pending = await OfflineQueue.pendingPODs();
      expect(pending.some((p) => p.trip_id === 'trip_offline_77')).toBe(true);
    });
  });
});
