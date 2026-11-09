import React from 'react';
import { render } from '@testing-library/react-native';
import { DispatchOfferCard } from '../src/features/dispatch/components/DispatchOfferCard';

const emptyOffer = {
  id: 'offer-1',
  booking_id: 'booking-12345678',
  driver_id: 'drv-1',
  vehicle_id: '',
  status: 'offered' as const,
  offered_at: '2026-11-09T10:00:00Z',
  expires_at: '2026-11-09T10:05:00Z',
  origin: '',
  destination: '',
  cargo_type: '',
  payout: 0,
};

describe('DispatchOfferCard honesty', () => {
  it('renders em dashes instead of fabricated cargo and vehicle values', () => {
    const { queryByText, getAllByText } = render(
      <DispatchOfferCard offer={emptyOffer} onAccept={jest.fn()} onReject={jest.fn()} />
    );
    expect(queryByText('MH04AB1234')).toBeNull();
    expect(queryByText('Industrial Goods')).toBeNull();
    expect(getAllByText('—').length).toBeGreaterThanOrEqual(2);
  });
});
