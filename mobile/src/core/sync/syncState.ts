import { commandQueue } from './commandQueue';

export interface SyncStatus {
  isOnline: boolean;
  pendingCommandsCount: number;
}

export async function getSyncStatus(isOnline: boolean): Promise<SyncStatus> {
  const pendingCount = await commandQueue.getPendingCount();
  return {
    isOnline,
    pendingCommandsCount: pendingCount,
  };
}
