import React from 'react';
import { render, fireEvent, waitFor } from '@testing-library/react-native';
import { PaisaScreen } from '../src/components/PaisaScreen';
import * as driverMoney from '../src/services/driverMoney';

jest.mock('../src/services/driverMoney', () => ({
  getDriverBalance: jest.fn(),
  getDriverSettlements: jest.fn(),
  getAdvanceRequests: jest.fn(),
  requestAdvance: jest.fn(),
  getDriverPayouts: jest.fn(),
  verifyPayoutAccount: jest.fn(),
}));

const mocked = driverMoney as jest.Mocked<typeof driverMoney>;

describe('PaisaScreen honesty + payout rail', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mocked.getDriverBalance.mockResolvedValue(null);
    mocked.getDriverSettlements.mockResolvedValue([]);
    mocked.getAdvanceRequests.mockResolvedValue([]);
    mocked.getDriverPayouts.mockResolvedValue(null);
  });

  it('renders no fabricated balance or settlements when APIs return nothing', async () => {
    const { getByText, queryByText } = render(<PaisaScreen />);
    await waitFor(() => expect(mocked.getDriverPayouts).toHaveBeenCalled());
    expect(getByText('—')).toBeTruthy();
    expect(queryByText('₹4,250')).toBeNull();
    expect(queryByText('₹18,500')).toBeNull();
    expect(queryByText('Settlement #STL-9842')).toBeNull();
    expect(getByText('No settlements recorded yet.')).toBeTruthy();
  });

  it('renders real payout account + instructions and starts penny-drop', async () => {
    mocked.getDriverPayouts.mockResolvedValue({
      rail_enabled: true,
      account: {
        masked: 'XXXX4455',
        bank_name: 'Test Bank',
        ifsc: 'SBIN0001234',
        verification_status: 'unverified',
      },
      payouts: [
        {
          id: 'pout-1',
          amount: 500,
          currency: 'INR',
          status: 'paid',
          utr: 'UTR999',
          initiated_at: '2026-11-09T10:00:00Z',
        },
      ],
    });
    mocked.verifyPayoutAccount.mockResolvedValue({ status: 'penny_drop_pending' });

    const { findByText, getByText } = render(<PaisaScreen />);
    expect(await findByText('Test Bank')).toBeTruthy();
    expect(getByText('XXXX4455 · SBIN0001234')).toBeTruthy();
    expect(getByText('UTR UTR999')).toBeTruthy();

    fireEvent.press(getByText('START PENNY-DROP VERIFICATION'));
    await waitFor(() => expect(mocked.verifyPayoutAccount).toHaveBeenCalledTimes(1));
  });

  it('shows an honest error instead of preview data when the fetch fails', async () => {
    mocked.getDriverBalance.mockRejectedValue(new Error('boom'));
    const { findByText, queryByText } = render(<PaisaScreen />);
    expect(await findByText('Could not load wallet data. Pull to refresh.')).toBeTruthy();
    expect(queryByText('₹4,250')).toBeNull();
  });
});
