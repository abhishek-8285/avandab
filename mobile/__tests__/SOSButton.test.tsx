import React from 'react';
import { render, fireEvent, waitFor } from '@testing-library/react-native';
import { SOSButton } from '../src/components/SOSButton';
import { sosService } from '../src/services/sosService';

jest.mock('../src/services/sosService', () => ({
  sosService: {
    triggerSOS: jest.fn(),
  },
}));

describe('SOSButton Component', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('renders the emergency SOS trigger button', () => {
    const { getByTestId, getByText } = render(<SOSButton />);
    expect(getByTestId('driver-sos-button')).toBeTruthy();
    expect(getByText('SOS')).toBeTruthy();
  });

  it('opens confirmation modal on press and triggers SOS on confirm', async () => {
    (sosService.triggerSOS as jest.Mock).mockResolvedValue({
      success: true,
      queued: false,
      commandId: 'sos_123',
      sosId: 'sos_srv_123',
      message: 'Dispatched',
    });

    const onSentMock = jest.fn();
    const { getByTestId, getByText } = render(
      <SOSButton
        tripId="trip_55"
        vehicleId="KA01AB1234"
        latitude={12.9716}
        longitude={77.5946}
        accuracy={10}
        onSOSSent={onSentMock}
      />
    );

    // 1. Press SOS Button
    fireEvent.press(getByTestId('driver-sos-button'));
    expect(getByText('Trigger Emergency SOS?')).toBeTruthy();

    // 2. Confirm in modal
    fireEvent.press(getByTestId('sos-confirm-button'));

    await waitFor(() => {
      expect(sosService.triggerSOS).toHaveBeenCalledWith(
        expect.objectContaining({
          tripId: 'trip_55',
          vehicleId: 'KA01AB1234',
          latitude: 12.9716,
          longitude: 77.5946,
          accuracy: 10,
        })
      );
      expect(onSentMock).toHaveBeenCalled();
    });
  });

  it('cancels modal when Cancel is pressed without triggering SOS', () => {
    const { getByTestId, queryByText } = render(<SOSButton />);

    fireEvent.press(getByTestId('driver-sos-button'));
    fireEvent.press(getByTestId('sos-cancel-button'));

    expect(sosService.triggerSOS).not.toHaveBeenCalled();
  });
});
