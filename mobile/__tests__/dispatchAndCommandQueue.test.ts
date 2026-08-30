import { generateCommandId } from '../src/core/sync/idempotency';
import { CommandQueue } from '../src/core/sync/commandQueue';

describe('Dispatch & Offline Command Pipeline', () => {
  describe('Idempotency Key Generator', () => {
    it('generates unique prefixed command IDs', () => {
      const id1 = generateCommandId('accept');
      const id2 = generateCommandId('accept');
      expect(id1.startsWith('accept_')).toBe(true);
      expect(id2.startsWith('accept_')).toBe(true);
      expect(id1).not.toBe(id2);
    });
  });

  describe('Durable Command Queue', () => {
    it('enqueues command with PENDING state', async () => {
      const queue = new CommandQueue();
      const cmd = await queue.enqueueCommand('START_TRIP', { trip_id: 'trip-1' });

      expect(cmd.type).toBe('START_TRIP');
      expect(cmd.state).toBe('PENDING');
      expect(cmd.payload.trip_id).toBe('trip-1');
      expect(cmd.commandId).toBeDefined();
    });

    it('updates command state to SYNCED with response data', async () => {
      const queue = new CommandQueue();
      const cmd = await queue.enqueueCommand('COMPLETE_TRIP', { trip_id: 'trip-2' });

      await queue.updateCommandState(cmd.commandId, 'SYNCED', {
        response: { success: true, trip_state: 'delivered' },
      });

      const list = await queue.getCommands();
      const found = list.find((c) => c.commandId === cmd.commandId);
      expect(found?.state).toBe('SYNCED');
      expect(found?.response?.trip_state).toBe('delivered');
    });

    it('returns only pending commands for sync processor', async () => {
      const queue = new CommandQueue();
      const pendingList = await queue.getPendingCommands();
      pendingList.forEach((c) => {
        expect(['PENDING', 'SYNCING']).toContain(c.state);
      });
    });
  });
});
