import { managerService } from '../src/services/managerService';
import { useAuthStore } from '../src/stores/authStore';

describe('ManagerService', () => {
  const originalFetch = global.fetch;

  beforeEach(() => {
    useAuthStore.setState({
      token: 'test-mgr-token',
      user: {
        id: 'usr_mgr_1',
        name: 'Manager Test',
        email: 'mgr@test.com',
        role: 'manager',
      },
      activeMode: 'manager',
    });
  });

  afterEach(() => {
    global.fetch = originalFetch;
    jest.clearAllMocks();
  });

  it('fetches manager dashboard KPIs successfully', async () => {
    const mockKPIs = {
      todays_trips_count: 14,
      active_trips_count: 5,
      completed_trips_count: 9,
      cancelled_trips_count: 0,
      available_vehicles_count: 8,
      available_drivers_count: 10,
      pending_payments_count: 3,
      monthly_revenue: 450000,
      attention: {
        unassigned_bookings: 1,
        maintenance_due: 2,
        open_work_orders: 0,
        garage_vehicles: 0,
        open_alerts: 1,
        active_dtcs: 0,
        expiring_ewaybills: 0,
        pending_kharcha: 4,
        low_fastag: 1,
        total: 9,
      },
    };

    global.fetch = jest.fn().mockResolvedValue({
      ok: true,
      json: async () => mockKPIs,
    } as any);

    const data = await managerService.fetchDashboard();
    expect(data.active_trips_count).toBe(5);
    expect(data.attention.pending_kharcha).toBe(4);
    expect(global.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/manager/dashboard'),
      expect.objectContaining({
        headers: expect.objectContaining({
          Authorization: 'Bearer test-mgr-token',
        }),
      })
    );
  });

  it('fetches active trips from control tower', async () => {
    const mockTrips = [
      {
        trip_id: 'tr_1',
        trip_number: 'TR-101',
        status: 'in_transit',
        origin: 'Mumbai',
        destination: 'Pune',
        driver: { name: 'Driver A' },
        telemetry: { speed: 65, remaining_km: 80 },
      },
    ];

    global.fetch = jest.fn().mockResolvedValue({
      ok: true,
      json: async () => mockTrips,
    } as any);

    const trips = await managerService.fetchActiveTrips();
    expect(trips.length).toBe(1);
    expect(trips[0].trip_number).toBe('TR-101');
  });

  it('approves a driver kharcha expense claim', async () => {
    global.fetch = jest.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ status: 'approved', expense_id: 'exp_1' }),
    } as any);

    const res = await managerService.approveExpense('exp_1');
    expect(res.status).toBe('approved');
    expect(global.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/kharcha/exp_1/approve'),
      expect.objectContaining({ method: 'POST' })
    );
  });

  it('rejects a driver kharcha expense claim with reason', async () => {
    global.fetch = jest.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ status: 'rejected', expense_id: 'exp_2', reason: 'Receipt blurred' }),
    } as any);

    const res = await managerService.rejectExpense('exp_2', 'Receipt blurred');
    expect(res.status).toBe('rejected');
    expect(global.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/kharcha/exp_2/reject'),
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ reason: 'Receipt blurred' }),
      })
    );
  });
});
