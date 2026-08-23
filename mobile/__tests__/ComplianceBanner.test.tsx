import React from 'react';
import { render } from '@testing-library/react-native';
import { ComplianceBanner } from '../src/components/ComplianceBanner';

const globalFetch = global.fetch;

function mockDocuments(docs: { doc_type: string; expiry_date: string | null }[]) {
  return jest.fn().mockResolvedValue({
    ok: true,
    status: 200,
    json: async () => ({ documents: docs }),
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

  test('renders green score when all documents are valid', async () => {
    global.fetch = mockDocuments([
      { doc_type: 'rc', expiry_date: '2099-01-01' },
      { doc_type: 'fitness', expiry_date: '2099-02-01' },
      { doc_type: 'insurance', expiry_date: '2099-03-01' },
      { doc_type: 'puc', expiry_date: '2099-04-01' },
      { doc_type: 'permit', expiry_date: '2099-05-01' },
      { doc_type: 'road_tax', expiry_date: '2099-06-01' },
    ]);
    const { findByLabelText, findByText } = render(
      <ComplianceBannerHarness vehicleId="veh_1" />
    );

    expect(await findByLabelText('compliance.score_green')).toBeTruthy();
    expect(await findByText('compliance.score_green')).toBeTruthy();
  });

  test('renders red score when a document is expired', async () => {
    global.fetch = mockDocuments([
      { doc_type: 'rc', expiry_date: '2099-01-01' },
      { doc_type: 'fitness', expiry_date: '2099-02-01' },
      { doc_type: 'insurance', expiry_date: '2000-01-01' },
      { doc_type: 'puc', expiry_date: '2099-04-01' },
      { doc_type: 'permit', expiry_date: '2099-05-01' },
      { doc_type: 'road_tax', expiry_date: '2099-06-01' },
    ]);
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
