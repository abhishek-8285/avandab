import { CommandQueue } from '../src/core/sync/commandQueue';
import { CommandProcessor } from '../src/core/sync/commandProcessor';
import { OfflineQueue } from '../src/services/offlineQueue';
import { deriveNavState } from '../src/utils/navState';
import { Trip, TripStop } from '../src/types/api';
import { resetSQLiteMockState } from '../jest/setup';
import { useAuthStore } from '../src/stores/authStore';

const globalFetch = global.fetch;

describe('Priority 5C — Mobile Multi-Stop Workflow & Offline Sync', () => {
  const tripStops: TripStop[] = [
    {
      id: 'stop-delhi',
      tripId: 'trip-p5c-1',
      stopSequence: 1,
      stopType: 'pickup',
      locationName: 'Delhi Warehouse Hub',
      address: 'Mayapuri Industrial Area, New Delhi',
      latitude: 28.628,
      longitude: 77.112,
      geofenceRadiusM: 300,
      status: 'pending',
      requiresPOD: true,
      requiresOTP: false,
      consigneeName: 'Delhi Central Depot',
      consigneePhone: '+919810011001',
    },
    {
      id: 'stop-jaipur',
      tripId: 'trip-p5c-1',
      stopSequence: 2,
      stopType: 'drop',
      locationName: 'Jaipur Delivery Center',
      address: 'Sitapura Industrial Area, Jaipur',
      latitude: 26.772,
      longitude: 75.864,
      geofenceRadiusM: 300,
      status: 'pending',
      requiresPOD: true,
      requiresOTP: true,
      consigneeName: 'Jaipur Retail Hub',
      consigneePhone: '+919820022002',
    },
    {
      id: 'stop-udaipur',
      tripId: 'trip-p5c-1',
      stopSequence: 3,
      stopType: 'drop',
      locationName: 'Udaipur Distribution Center',
      address: 'Sukher Industrial Area, Udaipur',
      latitude: 24.638,
      longitude: 73.712,
      geofenceRadiusM: 300,
      status: 'pending',
      requiresPOD: true,
      requiresOTP: true,
      consigneeName: 'Udaipur Final Consignee',
      consigneePhone: '+919830033003',
    },
  ];

  const initialTrip: Trip = {
    id: 'trip-p5c-1',
    tripNumber: 'TRP-P5C-001',
    driverName: 'Rajesh Sharma',
    vehiclePlate: 'DL-01-AB-1234',
    origin: 'Delhi Warehouse Hub',
    destination: 'Udaipur Distribution Center',
    status: 'IN_TRANSIT',
    startTime: '2026-08-30T10:00:00Z',
    stops: tripStops,
  };

  beforeEach(async () => {
    resetSQLiteMockState();
    await new CommandQueue().saveCommands([]);
    await OfflineQueue.init();
    await useAuthStore.getState().setAuth('mock_driver_token_jwt', {
      id: 'usr-1',
      name: 'Rajesh Sharma',
      role: 'driver',
      email: 'rajesh@avandab.com',
      driverId: 'drv-1',
    });
  });

  afterEach(() => {
    global.fetch = globalFetch;
  });

  test('1. Sequential Multi-Stop Navigation & UI Active Stop Locking', () => {
    // Initial state: Stop 1 of 3 active
    const nav1 = deriveNavState(initialTrip, 15);
    expect(nav1.hasTrip).toBe(true);
    expect(nav1.stepLabel).toBe('STOP 01/03 · PICKUP');
    expect(nav1.legTitle).toBe('HEAD TO PICKUP');
    expect(nav1.nextStopAddress).toBe('Delhi Warehouse Hub');
    expect(nav1.activeStop?.id).toBe('stop-delhi');
    expect(nav1.activeStopIndex).toBe(0);
    expect(nav1.totalStops).toBe(3);
    expect(nav1.allStopsCompleted).toBe(false);

    // Stop 1 Arrived
    const tripStop1Arrived: Trip = {
      ...initialTrip,
      stops: initialTrip.stops!.map((s) => (s.id === 'stop-delhi' ? { ...s, status: 'arrived' as const } : s)),
    };
    const nav1Arrived = deriveNavState(tripStop1Arrived);
    expect(nav1Arrived.stepLabel).toBe('STOP 01/03 · PICKUP');
    expect(nav1Arrived.legTitle).toBe('AT PICKUP');
    expect(nav1Arrived.activeStop?.id).toBe('stop-delhi');

    // Stop 1 Completed -> Active stop advances to Stop 2 of 3
    const tripStop1Completed: Trip = {
      ...initialTrip,
      stops: initialTrip.stops!.map((s) => (s.id === 'stop-delhi' ? { ...s, status: 'completed' as const } : s)),
    };
    const nav2 = deriveNavState(tripStop1Completed);
    expect(nav2.stepLabel).toBe('STOP 02/03 · DROP');
    expect(nav2.legTitle).toBe('DELIVER TO');
    expect(nav2.nextStopAddress).toBe('Jaipur Delivery Center');
    expect(nav2.activeStop?.id).toBe('stop-jaipur');
    expect(nav2.activeStopIndex).toBe(1);

    // Stop 2 Completed -> Active stop advances to Stop 3 of 3
    const tripStop2Completed: Trip = {
      ...initialTrip,
      stops: initialTrip.stops!.map((s) =>
        s.id === 'stop-delhi' || s.id === 'stop-jaipur' ? { ...s, status: 'completed' as const } : s
      ),
    };
    const nav3 = deriveNavState(tripStop2Completed);
    expect(nav3.stepLabel).toBe('STOP 03/03 · DROP');
    expect(nav3.nextStopAddress).toBe('Udaipur Distribution Center');
    expect(nav3.activeStop?.id).toBe('stop-udaipur');

    // All stops completed -> Terminal navigation state
    const tripAllCompleted: Trip = {
      ...initialTrip,
      stops: initialTrip.stops!.map((s) => ({ ...s, status: 'completed' as const })),
    };
    const navDone = deriveNavState(tripAllCompleted);
    expect(navDone.stepLabel).toBe('COMPLETE');
    expect(navDone.legTitle).toBe('ALL STOPS COMPLETED');
    expect(navDone.allStopsCompleted).toBe(true);
    expect(navDone.activeStop).toBeNull();
  });

  test('2. Offline Stop Actions: Command Queueing, Storage Persistence, and App Kill/Restart', async () => {
    const queue = new CommandQueue();

    // Driver goes offline at Stop 1 and performs Reach Stop, Submit POD, and Complete Stop
    const reachCmd = await queue.enqueueCommand('REACH_STOP', {
      trip_id: 'trip-p5c-1',
      stop_id: 'stop-delhi',
      stop_sequence: 1,
    });
    expect(reachCmd.state).toBe('PENDING');

    await OfflineQueue.enqueuePOD('trip-p5c-1', {
      stop_id: 'stop-delhi',
      stop_sequence: 1,
      consignee_name: 'Delhi Central Depot',
      consignee_phone: '+919810011001',
      notes: 'Pickup goods verified at dock 1',
      photo_uri: 'file:///data/pod_stop1.jpg',
      latitude: 28.628,
      longitude: 77.112,
    });

    const completeCmd = await queue.enqueueCommand('COMPLETE_STOP', {
      trip_id: 'trip-p5c-1',
      stop_id: 'stop-delhi',
      stop_sequence: 1,
    });
    expect(completeCmd.state).toBe('PENDING');

    // SIMULATE APP KILL & RESTART:
    // Create new instances of CommandQueue and query SQLite
    const restartedQueue = new CommandQueue();
    const pendingCommands = await restartedQueue.getPendingCommands();
    expect(pendingCommands).toHaveLength(2);
    expect(pendingCommands.map((c) => c.type)).toEqual(['REACH_STOP', 'COMPLETE_STOP']);

    const pendingPODs = await OfflineQueue.pendingPODs();
    expect(pendingPODs).toHaveLength(1);
    expect(pendingPODs[0].stop_id).toBe('stop-delhi');
    expect(pendingPODs[0].consignee_name).toBe('Delhi Central Depot');
  });

  test('3. Network Reconnect: Full Multi-Stop Flush & Synchronization with 5x Replay Protection', async () => {
    const queue = new CommandQueue();
    const processor = new CommandProcessor();

    // Enqueue actions for Stop 1, Stop 2, and Stop 3
    await queue.enqueueCommand('REACH_STOP', { trip_id: 'trip-p5c-1', stop_id: 'stop-delhi', stop_sequence: 1 });
    await queue.enqueueCommand('COMPLETE_STOP', { trip_id: 'trip-p5c-1', stop_id: 'stop-delhi', stop_sequence: 1 });
    await queue.enqueueCommand('REACH_STOP', { trip_id: 'trip-p5c-1', stop_id: 'stop-jaipur', stop_sequence: 2 });
    await queue.enqueueCommand('COMPLETE_STOP', { trip_id: 'trip-p5c-1', stop_id: 'stop-jaipur', stop_sequence: 2 });
    await queue.enqueueCommand('REACH_STOP', { trip_id: 'trip-p5c-1', stop_id: 'stop-udaipur', stop_sequence: 3 });
    await queue.enqueueCommand('COMPLETE_STOP', { trip_id: 'trip-p5c-1', stop_id: 'stop-udaipur', stop_sequence: 3 });
    await queue.enqueueCommand('COMPLETE_TRIP', { trip_id: 'trip-p5c-1' });

    // Queue PODs for Stop 1 and Stop 2
    await OfflineQueue.enqueuePOD('trip-p5c-1', {
      stop_id: 'stop-delhi',
      stop_sequence: 1,
      consignee_name: 'Delhi Central Depot',
      photo_uri: 'file:///pod1.jpg',
    });
    await OfflineQueue.enqueuePOD('trip-p5c-1', {
      stop_id: 'stop-jaipur',
      stop_sequence: 2,
      otp: '123456',
      consignee_name: 'Jaipur Retail Hub',
      photo_uri: 'file:///pod2.jpg',
    });

    const callLog: { url: string; method: string; commandId?: string }[] = [];
    global.fetch = jest.fn().mockImplementation(async (url: string, opts: any) => {
      callLog.push({ url, method: opts.method, commandId: opts.headers?.['X-Command-Id'] });
      return {
        ok: true,
        status: 200,
        json: async () => ({ success: true, url }),
      };
    });

    // 1st Flush: All items synchronize successfully
    const res = await processor.flush('mock_driver_token_jwt');
    expect(res.synced).toBe(7);
    expect(res.failed).toBe(0);

    const podRes = await OfflineQueue.flush();
    expect(podRes.podsFlushed).toBe(2);

    // Verify all endpoints received the commands
    expect(callLog.some((c) => c.url.includes('/stops/stop-delhi/reach'))).toBe(true);
    expect(callLog.some((c) => c.url.includes('/stops/stop-delhi/complete'))).toBe(true);
    expect(callLog.some((c) => c.url.includes('/stops/stop-jaipur/reach'))).toBe(true);
    expect(callLog.some((c) => c.url.includes('/stops/stop-jaipur/complete'))).toBe(true);
    expect(callLog.some((c) => c.url.includes('/stops/stop-udaipur/reach'))).toBe(true);
    expect(callLog.some((c) => c.url.includes('/stops/stop-udaipur/complete'))).toBe(true);
    expect(callLog.some((c) => c.url.includes('/api/v1/trips/trip-p5c-1/complete'))).toBe(true);
    expect(callLog.some((c) => c.url.includes('/stops/stop-delhi/pod'))).toBe(true);
    expect(callLog.some((c) => c.url.includes('/stops/stop-jaipur/pod'))).toBe(true);

    // 5x Replay Protection: Subsequent flushes must be zero no-ops
    for (let i = 0; i < 5; i++) {
      const replayCmd = await processor.flush('mock_driver_token_jwt');
      expect(replayCmd.synced).toBe(0);
      expect(replayCmd.failed).toBe(0);

      const replayPOD = await OfflineQueue.flush();
      expect(replayPOD.podsFlushed).toBe(0);
    }
  });

  test('4. Network Failure & Offline Resilience during POD Submission', async () => {
    // Simulate network error during POD upload
    global.fetch = jest.fn().mockRejectedValue(new Error('Network request failed'));

    await OfflineQueue.enqueuePOD('trip-p5c-1', {
      stop_id: 'stop-jaipur',
      stop_sequence: 2,
      consignee_name: 'Jaipur Retail Hub',
      notes: 'Offline submission attempt',
    });

    const result = await OfflineQueue.flush();
    expect(result.podsFlushed).toBe(0);

    // Must remain queued without data loss
    const remaining = await OfflineQueue.pendingPODs();
    expect(remaining).toHaveLength(1);
    expect(remaining[0].stop_id).toBe('stop-jaipur');
    expect(remaining[0].consignee_name).toBe('Jaipur Retail Hub');
  });
});
