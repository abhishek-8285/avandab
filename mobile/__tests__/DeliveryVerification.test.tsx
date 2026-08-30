import React from 'react';
import { render, fireEvent, waitFor, act } from '@testing-library/react-native';
import { DeliveryVerificationScreen } from '../src/components/DeliveryVerificationScreen';
import { OfflineQueue } from '../src/services/offlineQueue';
import { resetSQLiteMockState } from '../jest/setup';
import { useAuthStore } from '../src/stores/authStore';

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

    const otpInput = getByPlaceholderText('• • • •');
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

    const otpInput = getByPlaceholderText('• • • •');
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
