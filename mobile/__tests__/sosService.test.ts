import { SOSService } from '../src/services/sosService';
import { commandQueue } from '../src/core/sync/commandQueue';
import AsyncStorage from '@react-native-async-storage/async-storage';

describe('Mobile SOS Service', () => {
  let sos: SOSService;
  const originalFetch = global.fetch;

  beforeEach(async () => {
    await AsyncStorage.clear();
    await commandQueue.saveCommands([]);
    sos = new SOSService();
    jest.clearAllMocks();
  });

  afterEach(() => {
    global.fetch = originalFetch;
  });

  it('immediately persists and sends SOS when online', async () => {
    const mockResponse = {
      status: 'acknowledged',
      sos_id: 'sos_12345',
      driver_id: 'driver_001',
      vehicle_id: 'KA01AB1234',
      message: 'Emergency response team and dispatchers have been alerted.',
    };

    global.fetch = jest.fn().mockResolvedValue({
      ok: true,
      status: 201,
      json: async () => mockResponse,
    });

    const result = await sos.triggerSOS({
      tripId: 'trip_100',
      vehicleId: 'KA01AB1234',
      latitude: 12.9716,
      longitude: 77.5946,
      accuracy: 15,
      batteryLevel: 88,
      tokenOverride: 'test_token',
    });

    expect(result.success).toBe(true);
    expect(result.queued).toBe(false);
    expect(result.sosId).toBe('sos_12345');

    // Verify command queue persistence
    const commands = await commandQueue.getCommands();
    expect(commands).toHaveLength(1);
    expect(commands[0].type).toBe('SOS');
    expect(commands[0].state).toBe('SYNCED');
    expect(commands[0].payload.latitude).toBe(12.9716);
    expect(commands[0].payload.longitude).toBe(77.5946);
  });

  it('persists SOS in PENDING state when offline without throwing', async () => {
    // Simulate network error
    global.fetch = jest.fn().mockRejectedValue(new Error('Network request failed'));

    const result = await sos.triggerSOS({
      tripId: 'trip_100',
      vehicleId: 'KA01AB1234',
      latitude: 12.9716,
      longitude: 77.5946,
      accuracy: 20,
      tokenOverride: 'test_token',
    });

    expect(result.success).toBe(true);
    expect(result.queued).toBe(true);
    expect(result.message).toContain('offline');

    // Verify command queue kept in PENDING state
    const pending = await sos.getPendingSOS();
    expect(pending).toHaveLength(1);
    expect(pending[0].state).toBe('PENDING');
    expect(pending[0].payload.latitude).toBe(12.9716);
  });

  it('flushes pending SOS when network connection is restored', async () => {
    // 1. Trigger while offline
    global.fetch = jest.fn().mockRejectedValue(new Error('No internet connection'));
    await sos.triggerSOS({
      tripId: 'trip_100',
      latitude: 12.9716,
      longitude: 77.5946,
      tokenOverride: 'test_token',
    });

    expect(await sos.getPendingSOS()).toHaveLength(1);

    // 2. Restore network
    global.fetch = jest.fn().mockResolvedValue({
      ok: true,
      status: 201,
      json: async () => ({ status: 'acknowledged', sos_id: 'sos_reconnected' }),
    });

    const retryResult = await sos.retryPendingSOS('test_token');
    expect(retryResult.synced).toBe(1);
    expect(retryResult.pending).toBe(0);

    const pendingAfter = await sos.getPendingSOS();
    expect(pendingAfter).toHaveLength(0);

    const allCommands = await commandQueue.getCommands();
    expect(allCommands[0].state).toBe('SYNCED');
  });

  it('marks FAILED on authentication error without infinite loop', async () => {
    global.fetch = jest.fn().mockResolvedValue({
      ok: false,
      status: 401,
      json: async () => ({ error: 'unauthorized', message: 'token expired' }),
    });

    const result = await sos.triggerSOS({
      latitude: 12.9716,
      longitude: 77.5946,
      tokenOverride: 'expired_token',
    });

    expect(result.success).toBe(false);
    expect(result.error).toBe('Authentication failed');

    const allCommands = await commandQueue.getCommands();
    expect(allCommands[0].state).toBe('FAILED');
    expect(allCommands[0].errorMessage).toContain('Auth failed (401)');
  });
});
