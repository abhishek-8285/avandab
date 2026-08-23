import React from 'react';
import { fireEvent, render, waitFor } from '@testing-library/react-native';
import { EWayBillCard } from '../src/components/EWayBillCard';

const globalFetch = global.fetch;

const EWB_PAYLOAD = {
  eway_bill_number: '112233',
  valid_until: '2026-09-01',
  qr_data: 'EWB|112233',
  total_value: 60000,
};

function ewbFetchMock() {
  let getCalls = 0;
  return jest.fn().mockImplementation(async (url: any, opts?: any) => {
    const u = String(url);
    if (u.includes('/ewaybill/generate')) {
      return {
        ok: true,
        status: 200,
        json: async () => EWB_PAYLOAD,
      };
    }
    if (u.includes('/ewaybill')) {
      getCalls += 1;
      if (getCalls === 1) {
        // First GET: no EWB generated yet
        return { ok: false, status: 404, json: async () => ({}) };
      }
      return { ok: true, status: 200, json: async () => EWB_PAYLOAD };
    }
    return { ok: false, status: 404, json: async () => ({}) };
  }) as any;
}

describe('EWayBillCard', () => {
  afterEach(() => {
    global.fetch = globalFetch;
  });

  test('404 → generate → refetch flows into card showing the EWB number', async () => {
    const fetchMock = ewbFetchMock();
    global.fetch = fetchMock;

    const { findByText, getByLabelText, getByText, queryByText } = render(
      <EWayBillCard tripId="trip_ewb_1" totalValue={60000} />
    );

    // First GET returned 404 → generate button visible
    await findByText('ewb.generate');
    expect(queryByText('112233')).toBeNull();

    fireEvent.press(getByLabelText('ewb-generate'));

    // POST then refetch GET → card renders with number
    await waitFor(() => expect(getByText('112233')).toBeTruthy());

    const methods = fetchMock.mock.calls.map((c: any[]) => c[1]?.method ?? 'GET');
    expect(methods).toContain('POST');
    expect(methods.filter((m: string) => m === 'POST')).toHaveLength(1);
  });

  test('below threshold shows hint and never issues a generate POST', async () => {
    const fetchMock = ewbFetchMock();
    global.fetch = fetchMock;

    const { findByText, queryByText } = render(
      <EWayBillCard tripId="trip_ewb_2" totalValue={1000} />
    );

    expect(await findByText('ewb.below_threshold')).toBeTruthy();
    expect(queryByText('ewb.generate')).toBeNull();

    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    const generateCalls = fetchMock.mock.calls.filter((c: any[]) =>
      String(c[0]).includes('/ewaybill/generate')
    );
    expect(generateCalls).toHaveLength(0);
  });
});
