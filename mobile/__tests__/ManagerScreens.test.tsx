import React from 'react';
import { render, fireEvent, waitFor } from '@testing-library/react-native';
import { ManagerDashboardScreen } from '../src/screens/manager/ManagerDashboardScreen';
import { ManagerFleetScreen } from '../src/screens/manager/ManagerFleetScreen';
import { ManagerTripsScreen } from '../src/screens/manager/ManagerTripsScreen';
import { ManagerApprovalsScreen } from '../src/screens/manager/ManagerApprovalsScreen';
import { managerService } from '../src/services/managerService';
import { useAuthStore } from '../src/stores/authStore';

describe('Manager Screens', () => {
  beforeEach(() => {
    useAuthStore.setState({
      token: 'test-token',
      user: {
        id: 'u1',
        name: 'Super Manager',
        email: 'manager@avandab.com',
        role: 'manager',
      },
      activeMode: 'manager',
    });
  });

  afterEach(() => {
    jest.clearAllMocks();
  });

  it('renders ManagerDashboardScreen with KPIs and triggers tab navigation', async () => {
    jest.spyOn(managerService, 'fetchDashboard').mockResolvedValue({
      todays_trips_count: 10,
      active_trips_count: 4,
      completed_trips_count: 6,
      cancelled_trips_count: 0,
      available_vehicles_count: 12,
      available_drivers_count: 15,
      pending_payments_count: 2,
      monthly_revenue: 350000,
      attention: {
        unassigned_bookings: 0,
        maintenance_due: 1,
        open_work_orders: 0,
        garage_vehicles: 0,
        open_alerts: 2,
        active_dtcs: 0,
        expiring_ewaybills: 0,
        pending_kharcha: 3,
        low_fastag: 0,
        total: 6,
      },
    });

    const onNavigateTab = jest.fn();
    const { getByText, getByTestId } = render(<ManagerDashboardScreen onNavigateTab={onNavigateTab} />);

    await waitFor(() => {
      expect(getByText('Welcome, Super Manager')).toBeTruthy();
      expect(getByText('MANAGER COCKPIT')).toBeTruthy();
      expect(getByText('Active Trips')).toBeTruthy();
    });

    fireEvent.press(getByTestId('kpi-card-active-trips'));
    expect(onNavigateTab).toHaveBeenCalledWith('trips');

    fireEvent.press(getByTestId('kpi-card-pending-approvals'));
    expect(onNavigateTab).toHaveBeenCalledWith('approvals');
  });

  it('renders ManagerFleetScreen and filters vehicles', async () => {
    jest.spyOn(managerService, 'fetchVehicles').mockResolvedValue([
      { id: 'v1', registration_number: 'MH-12-AB-1234', vehicle_type: 'Truck', status: 'available' },
      { id: 'v2', registration_number: 'DL-01-XY-9999', vehicle_type: 'Trailer', status: 'maintenance' },
    ]);

    const { getByText, queryByText } = render(<ManagerFleetScreen />);

    await waitFor(() => {
      expect(getByText('MH-12-AB-1234')).toBeTruthy();
      expect(getByText('DL-01-XY-9999')).toBeTruthy();
    });

    // Tap "Available" filter pill
    fireEvent.press(getByText('Available'));

    await waitFor(() => {
      expect(getByText('MH-12-AB-1234')).toBeTruthy();
      expect(queryByText('DL-01-XY-9999')).toBeNull();
    });
  });

  it('renders ManagerTripsScreen with route and progress', async () => {
    jest.spyOn(managerService, 'fetchActiveTrips').mockResolvedValue([
      {
        trip_id: 'tr_100',
        trip_number: 'TRIP-100',
        status: 'in_transit',
        origin: 'Delhi Depot',
        destination: 'Jaipur Hub',
        driver: { name: 'Rajesh Kumar' },
        vehicle: { registration_number: 'HR-55-AA-1111' },
        progression: { total_stops: 3, completed_stops: 1, progress_percent: 33, all_stops_completed: false },
        telemetry: { speed: 52, remaining_km: 120 },
      },
    ]);

    const { getByText, getByTestId } = render(<ManagerTripsScreen />);

    await waitFor(() => {
      expect(getByTestId('trip-card-TRIP-100')).toBeTruthy();
      expect(getByText('Delhi Depot')).toBeTruthy();
      expect(getByText('Jaipur Hub')).toBeTruthy();
      expect(getByText('33%')).toBeTruthy();
    });
  });

  it('renders ManagerApprovalsScreen and approves claim', async () => {
    const approveSpy = jest.spyOn(managerService, 'approveExpense').mockResolvedValue({
      status: 'approved',
      expense_id: 'exp_55',
    });

    jest.spyOn(managerService, 'fetchPendingExpenses').mockResolvedValue([
      {
        id: 'exp_55',
        trip_id: 'trip_1',
        driver_id: 'drv_1',
        driver_name: 'Amit Singh',
        expense_type: 'fuel',
        amount: 2500,
        description: 'IOCL Diesel filling',
        status: 'pending',
        created_at: '2026-10-31',
      },
    ]);

    const { getByText, getByTestId } = render(<ManagerApprovalsScreen />);

    await waitFor(() => {
      expect(getByText(/2500/)).toBeTruthy();
      expect(getByText(/IOCL Diesel filling/)).toBeTruthy();
    });

    fireEvent.press(getByTestId('approve-button-exp_55'));

    await waitFor(() => {
      expect(approveSpy).toHaveBeenCalledWith('exp_55');
    });
  });

  it('allows switching between Manager mode and Driver mode in auth store', () => {
    expect(useAuthStore.getState().activeMode).toBe('manager');

    useAuthStore.getState().toggleMode();
    expect(useAuthStore.getState().activeMode).toBe('driver');

    useAuthStore.getState().setMode('manager');
    expect(useAuthStore.getState().activeMode).toBe('manager');
  });
});
