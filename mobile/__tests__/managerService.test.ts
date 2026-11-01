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

  it('throws error when fetching manager dashboard fails', async () => {
    global.fetch = jest.fn().mockResolvedValue({
      ok: false,
      status: 500,
    } as any);

    await expect(managerService.fetchDashboard()).rejects.toThrow('Failed to load manager dashboard: HTTP 500');
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

  it('handles non-array active trips response gracefully', async () => {
    global.fetch = jest.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ unexpected: true }),
    } as any);

    const trips = await managerService.fetchActiveTrips();
    expect(trips).toEqual([]);
  });

  it('throws error when active trips endpoint fails', async () => {
    global.fetch = jest.fn().mockResolvedValue({
      ok: false,
      status: 404,
    } as any);

    await expect(managerService.fetchActiveTrips()).rejects.toThrow('Failed to load control tower trips: HTTP 404');
  });

  it('fetches fleet vehicles successfully (both raw array and nested object)', async () => {
    const mockVehicles = [
      {
        id: 'veh_1',
        registration_number: 'MH12AB1234',
        status: 'available',
      },
    ];

    // Case 1: Raw array
    global.fetch = jest.fn().mockResolvedValue({
      ok: true,
      json: async () => mockVehicles,
    } as any);

    let vehicles = await managerService.fetchVehicles();
    expect(vehicles.length).toBe(1);
    expect(vehicles[0].registration_number).toBe('MH12AB1234');

    // Case 2: Wrapped { vehicles: [...] }
    global.fetch = jest.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ vehicles: mockVehicles }),
    } as any);

    vehicles = await managerService.fetchVehicles();
    expect(vehicles.length).toBe(1);

    // Case 3: Empty / unexpected shape
    global.fetch = jest.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ invalid: true }),
    } as any);

    vehicles = await managerService.fetchVehicles();
    expect(vehicles).toEqual([]);
  });

  it('throws error when fetchVehicles fails', async () => {
    global.fetch = jest.fn().mockResolvedValue({
      ok: false,
      status: 503,
    } as any);

    await expect(managerService.fetchVehicles()).rejects.toThrow('Failed to load fleet vehicles: HTTP 503');
  });

  it('fetches pending driver kharcha expenses successfully', async () => {
    const mockExpenses = [
      {
        id: 'exp_1',
        trip_id: 'tr_1',
        driver_id: 'drv_1',
        driver_name: 'Driver 1',
        expense_type: 'fuel',
        amount: 2500,
        status: 'pending',
        created_at: new Date().toISOString(),
      },
    ];

    global.fetch = jest.fn().mockResolvedValue({
      ok: true,
      json: async () => mockExpenses,
    } as any);

    const expenses = await managerService.fetchPendingExpenses();
    expect(expenses.length).toBe(1);
    expect(expenses[0].amount).toBe(2500);

    // Non-array fallback
    global.fetch = jest.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ invalid: true }),
    } as any);
    const fallback = await managerService.fetchPendingExpenses();
    expect(fallback).toEqual([]);
  });

  it('throws error when fetchPendingExpenses fails', async () => {
    global.fetch = jest.fn().mockResolvedValue({
      ok: false,
      status: 401,
    } as any);

    await expect(managerService.fetchPendingExpenses()).rejects.toThrow('Failed to load pending expenses: HTTP 401');
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

  it('throws error when approveExpense fails with text error or fallback', async () => {
    global.fetch = jest.fn().mockResolvedValue({
      ok: false,
      status: 400,
      text: async () => 'Expense already approved',
    } as any);

    await expect(managerService.approveExpense('exp_1')).rejects.toThrow('Expense already approved');

    global.fetch = jest.fn().mockResolvedValue({
      ok: false,
      status: 500,
      text: async () => { throw new Error('parse error'); },
    } as any);

    await expect(managerService.approveExpense('exp_1')).rejects.toThrow('Failed to approve expense: HTTP 500');
  });

  it('rejects a driver kharcha expense claim with reason', async () => {
    global.fetch = jest.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ status: 'rejected', expense_id: 'exp_1', reason: 'Duplicate slip' }),
    } as any);

    const res = await managerService.rejectExpense('exp_1', 'Duplicate slip');
    expect(res.status).toBe('rejected');
    expect(global.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/kharcha/exp_1/reject'),
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ reason: 'Duplicate slip' }),
      })
    );
  });

  it('throws error when rejectExpense fails', async () => {
    global.fetch = jest.fn().mockResolvedValue({
      ok: false,
      status: 400,
      text: async () => 'Invalid reason',
    } as any);

    await expect(managerService.rejectExpense('exp_1', 'Invalid reason')).rejects.toThrow('Invalid reason');

    global.fetch = jest.fn().mockResolvedValue({
      ok: false,
      status: 500,
      text: async () => '',
    } as any);

    await expect(managerService.rejectExpense('exp_1', '')).rejects.toThrow('Failed to reject expense: HTTP 500');
  });

  it('handles getHeaders without token', async () => {
    useAuthStore.setState({ token: null });

    global.fetch = jest.fn().mockResolvedValue({
      ok: true,
      json: async () => [],
    } as any);

    await managerService.fetchPendingExpenses();
    expect(global.fetch).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({
        headers: {
          'Content-Type': 'application/json',
          Accept: 'application/json',
        },
      })
    );
  });
});
