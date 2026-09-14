import React from 'react';
import { render, act } from '@testing-library/react-native';
import { SyncStatusBar } from '../src/components/SyncStatusBar';
import { useSyncStore, type SyncStatus } from '../src/stores/syncStore';

const STATUSES: SyncStatus[] = ['online_synced', 'syncing', 'offline_saved', 'error'];

const STATUS_LABELS: Record<SyncStatus, string> = {
  online_synced: 'Online — All synced',
  syncing: 'Syncing…',
  offline_saved: 'Offline — Saved on device',
  error: 'Sync issue',
};

describe('SyncStatusBar', () => {
  beforeEach(() => {
    useSyncStore.setState({ status: 'online_synced', lastSyncAt: null, pendingCount: 0 });
  });

  test.each(STATUSES)('renders %s with a11y label', (status) => {
    useSyncStore.setState({ status });
    const { getByLabelText } = render(<SyncStatusBar />);

    expect(getByLabelText(STATUS_LABELS[status])).toBeTruthy();
  });

  test('updates when store transitions to offline_saved', () => {
    const { getByLabelText, rerender } = render(<SyncStatusBar />);
    expect(getByLabelText(STATUS_LABELS.online_synced)).toBeTruthy();

    act(() => {
      useSyncStore.setState({ status: 'offline_saved' });
    });
    rerender(<SyncStatusBar />);

    expect(getByLabelText(STATUS_LABELS.offline_saved)).toBeTruthy();
  });
});
