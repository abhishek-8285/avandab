import React from 'react';
import { render } from '@testing-library/react-native';
import { ComplianceBanner } from '../src/components/ComplianceBanner';

const globalFetch = global.fetch;

function mockVehicle(v: {
  insurance_expiry?: string | null;
  fitness_expiry?: string | null;
  permit_expiry?: string | null;
  rc_expiry?: string | null;
  puc_expiry?: string | null;
}) {
  return jest.fn().mockResolvedValue({
    ok: true,
    status: 200,
    json: async () => v,
  }) as any;
}

function ComplianceBannerHarness({ vehicleId }: { vehicleId?: string | null }) {
  return <ComplianceBanner vehicleId={vehicleId} />;
}

describe('ComplianceBanner', () => {
  afterEach(() => {
    global.fetch = globalFetch;
  });

  test('renders null when vehicleId is falsy (no fetch)', async () => {
    const fetchMock = jest.fn();
    global.fetch = fetchMock as any;
    const { queryByText, queryByLabelText } = render(<ComplianceBannerHarness vehicleId={null} />);

    expect(queryByText(/compliance\.score_/)).toBeNull();
    expect(queryByLabelText(/compliance\.score_/)).toBeNull();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  // NOTE: road_tax has no server field, so it is always "missing" (soft
  // warning): the best a fully-documented vehicle can score is amber with
  // canStartTrip=true. Pure-eval green is covered in compliance.test.ts.
  test('renders amber (start allowed) when all server-known documents are valid', async () => {
    global.fetch = mockVehicle({
      rc_expiry: '2099-01-01',
      fitness_expiry: '2099-02-01',
      insurance_expiry: '2099-03-01',
      puc_expiry: '2099-04-01',
      permit_expiry: '2099-05-01',
    });
    const { findByLabelText, findByText } = render(
      <ComplianceBannerHarness vehicleId="veh_1" />
    );

    expect(await findByLabelText('compliance.score_amber')).toBeTruthy();
    expect(await findByText('compliance.score_amber')).toBeTruthy();
  });

  test('renders red score when a document is expired', async () => {
    global.fetch = mockVehicle({
      rc_expiry: '2099-01-01',
      fitness_expiry: '2099-02-01',
      insurance_expiry: '2000-01-01',
      puc_expiry: '2099-04-01',
      permit_expiry: '2099-05-01',
    });
    const { findByLabelText } = render(<ComplianceBannerHarness vehicleId="veh_2" />);

    expect(await findByLabelText('compliance.score_red')).toBeTruthy();
  });

  test('fetch failure renders null instead of crashing', async () => {
    global.fetch = jest.fn().mockRejectedValue(new Error('network down')) as any;
    const { queryByLabelText } = render(<ComplianceBannerHarness vehicleId="veh_3" />);

    // Let the rejection settle
    await new Promise((r) => setTimeout(r, 0));
    expect(queryByLabelText(/compliance\.score_/)).toBeNull();
  });
});
