import { create } from 'zustand';

export type SyncStatus = 'online_synced' | 'syncing' | 'offline_saved' | 'error';

interface SyncState {
  status: SyncStatus;
  lastSyncAt: string | null;
  pendingCount: number;
  setStatus: (status: SyncStatus) => void;
  setPendingCount: (count: number) => void;
  markSynced: () => void;
}

export const useSyncStore = create<SyncState>((set) => ({
  status: 'online_synced',
  lastSyncAt: null,
  pendingCount: 0,

  setStatus: (status) => set({ status }),
  setPendingCount: (pendingCount) => set({ pendingCount }),
  markSynced: () =>
    set({ status: 'online_synced', lastSyncAt: new Date().toISOString() }),
}));

/**
 * Map a NetInfo connection state onto the sync store.
 * Going offline flips the bar to `offline_saved` — data keeps queueing locally.
 */
export function applyNetInfoState(isConnected: boolean): void {
  if (!isConnected) {
    useSyncStore.setState({ status: 'offline_saved' });
  }
}
