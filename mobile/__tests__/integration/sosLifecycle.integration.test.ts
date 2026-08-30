import AsyncStorage from '@react-native-async-storage/async-storage';
import { CommandQueue } from '../../src/core/sync/commandQueue';
import { SOSService } from '../../src/services/sosService';

describe('Mobile SOS Full Lifecycle & Kill-Restart Simulation', () => {
  const originalFetch = global.fetch;

  beforeEach(async () => {
    await AsyncStorage.clear();
    jest.clearAllMocks();
  });

  afterEach(() => {
    global.fetch = originalFetch;
  });

  it('survives app kill immediately after SOS press, restarts, and syncs upon network recovery with exactly 1 backend event', async () => {
    const receivedBackendCalls: any[] = [];

    // Step 1: App is running, Driver is OFFLINE (e.g. cellular dead zone in tunnel)
    global.fetch = jest.fn().mockImplementation(async (url: string, opts: any) => {
      // Simulate network disconnection
      throw new Error('Network request failed: unreachable');
    });

    const appInstance1_Queue = new CommandQueue();
    const appInstance1_SOS = new SOSService();

    // Driver presses emergency SOS button
    const triggerResult = await appInstance1_SOS.triggerSOS({
      tripId: 'TRIP-9901',
      vehicleId: 'MH-12-Q-9999',
      latitude: 19.0760,
      longitude: 72.8777,
      accuracy: 8.5,
      batteryLevel: 42,
      reason: 'Brake failure on highway',
      tokenOverride: 'driver_jwt_token',
    });

    expect(triggerResult.success).toBe(true);
    expect(triggerResult.queued).toBe(true);
    const commandId = triggerResult.commandId;
    expect(commandId).toBeDefined();

    // Step 2: Simulate IMMEDIATE APP KILL / OS Process Death
    // In-memory JavaScript heap is completely destroyed.
    // The only thing surviving is persisted AsyncStorage (disk SQLite).
    const persistedRaw = await AsyncStorage.getItem('avandab_offline_command_queue_v1');
    expect(persistedRaw).not.toBeNull();
    const persistedCommands = JSON.parse(persistedRaw!);
    expect(persistedCommands).toHaveLength(1);
    expect(persistedCommands[0].commandId).toBe(commandId);
    expect(persistedCommands[0].state).toBe('PENDING');

    // Step 3: Simulate APP RESTART
    // New fresh app session boots up and creates fresh service instances
    const appInstance2_Queue = new CommandQueue();
    const appInstance2_SOS = new SOSService();

    // Verify revived queue loaded from disk
    const pendingOnBoot = await appInstance2_SOS.getPendingSOS();
    expect(pendingOnBoot).toHaveLength(1);
    expect(pendingOnBoot[0].commandId).toBe(commandId);
    expect(pendingOnBoot[0].payload.trip_id).toBe('TRIP-9901');
    expect(pendingOnBoot[0].payload.latitude).toBe(19.0760);

    // Step 4: Network Connectivity is Restored
    global.fetch = jest.fn().mockImplementation(async (url: string, opts: any) => {
      if (url.includes('/api/v1/sos')) {
        const body = JSON.parse(opts.body);
        receivedBackendCalls.push(body);
        return {
          ok: true,
          status: 201,
          json: async () => ({
            status: 'acknowledged',
            sos_id: `sos_${body.command_id}`,
            driver_id: 'D-9901',
            vehicle_id: body.vehicle_id,
            received_at: new Date().toISOString(),
            message: 'Dispatchers notified',
          }),
        };
      }
      return { ok: false, status: 404 };
    });

    // Auto-sync / reconnect watcher fires
    const syncResult = await appInstance2_SOS.retryPendingSOS('driver_jwt_token');
    expect(syncResult.synced).toBe(1);
    expect(syncResult.pending).toBe(0);

    // Step 5: Verify exactly one SOS reached the backend
    expect(receivedBackendCalls).toHaveLength(1);
    expect(receivedBackendCalls[0].command_id).toBe(commandId);
    expect(receivedBackendCalls[0].trip_id).toBe('TRIP-9901');
    expect(receivedBackendCalls[0].latitude).toBe(19.0760);
    expect(receivedBackendCalls[0].reason).toBe('Brake failure on highway');

    // Step 6: 5x Replay idempotency test (simulate aggressive network retries)
    for (let i = 0; i < 5; i++) {
      await appInstance2_SOS.retryPendingSOS('driver_jwt_token');
    }

    // No duplicate requests queued or resent
    const finalPending = await appInstance2_SOS.getPendingSOS();
    expect(finalPending).toHaveLength(0);

    const finalDiskCommands = await appInstance2_Queue.getCommands();
    expect(finalDiskCommands).toHaveLength(1);
    expect(finalDiskCommands[0].state).toBe('SYNCED');
    expect(finalDiskCommands[0].response?.status).toBe('acknowledged');
  });
});
