import {
  applyNetInfoState,
  useSyncStore,
  type SyncStatus,
} from '../src/stores/syncStore';

const INITIAL = {
  status: 'online_synced' as SyncStatus,
  lastSyncAt: null as string | null,
  pendingCount: 0,
};

describe('syncStore', () => {
  beforeEach(() => {
    useSyncStore.setState({ ...INITIAL });
  });

  test('initial state is online_synced with no lastSync and zero pending', () => {
    const state = useSyncStore.getState();
    expect(state.status).toBe('online_synced');
    expect(state.lastSyncAt).toBeNull();
    expect(state.pendingCount).toBe(0);
  });

  test('setStatus transitions between all statuses', () => {
    const statuses: SyncStatus[] = ['syncing', 'offline_saved', 'error', 'online_synced'];
    for (const status of statuses) {
      useSyncStore.getState().setStatus(status);
      expect(useSyncStore.getState().status).toBe(status);
    }
  });

  test('setPendingCount updates count without touching status', () => {
    useSyncStore.getState().setStatus('offline_saved');
    useSyncStore.getState().setPendingCount(7);
    const state = useSyncStore.getState();
    expect(state.pendingCount).toBe(7);
    expect(state.status).toBe('offline_saved');
  });

  test('markSynced sets online_synced and stamps lastSyncAt', () => {
    useSyncStore.setState({ status: 'syncing' as SyncStatus, lastSyncAt: null });
    const before = Date.now();
    useSyncStore.getState().markSynced();
    const state = useSyncStore.getState();
    expect(state.status).toBe('online_synced');
    expect(state.lastSyncAt).not.toBeNull();
    expect(new Date(state.lastSyncAt as string).getTime()).toBeGreaterThanOrEqual(before);
  });

  test('applyNetInfoState maps offline to offline_saved', () => {
    applyNetInfoState(false);
    expect(useSyncStore.getState().status).toBe('offline_saved');
  });

  test('applyNetInfoState leaves status untouched when online', () => {
    useSyncStore.setState({ status: 'error' as SyncStatus });
    applyNetInfoState(true);
    expect(useSyncStore.getState().status).toBe('error');
  });
});
