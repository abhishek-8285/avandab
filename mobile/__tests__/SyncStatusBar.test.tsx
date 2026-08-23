import React from 'react';
import { render, act } from '@testing-library/react-native';
import { SyncStatusBar } from '../src/components/SyncStatusBar';
import { useSyncStore, type SyncStatus } from '../src/stores/syncStore';

const STATUSES: SyncStatus[] = ['online_synced', 'syncing', 'offline_saved', 'error'];

describe('SyncStatusBar', () => {
  beforeEach(() => {
    useSyncStore.setState({ status: 'online_synced', lastSyncAt: null, pendingCount: 0 });
  });

  test.each(STATUSES)('renders %s with echoed i18n key and a11y label', (status) => {
    useSyncStore.setState({ status });
    const { getByText, getByLabelText } = render(<SyncStatusBar />);

    expect(getByText(`sync.status_${status}`)).toBeTruthy();
    expect(getByLabelText(`sync.status_${status}`)).toBeTruthy();
  });

  test('updates when store transitions to offline_saved', () => {
    const { getByText, rerender } = render(<SyncStatusBar />);
    expect(getByText('sync.status_online_synced')).toBeTruthy();

    act(() => {
      useSyncStore.setState({ status: 'offline_saved' });
    });
    rerender(<SyncStatusBar />);

    expect(getByText('sync.status_offline_saved')).toBeTruthy();
  });
});
