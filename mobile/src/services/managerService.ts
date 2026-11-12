import { getApiBaseURL } from '../constants/network';
import { useAuthStore } from '../stores/authStore';

export interface ManagerDashboardKPIs {
  todays_trips_count: number;
  active_trips_count: number;
  completed_trips_count: number;
  cancelled_trips_count: number;
  available_vehicles_count: number;
  available_drivers_count: number;
  pending_payments_count: number;
  monthly_revenue: number;
  attention: {
    unassigned_bookings: number;
    maintenance_due: number;
    open_work_orders: number;
    garage_vehicles: number;
    open_alerts: number;
    active_dtcs: number;
    expiring_ewaybills: number;
    pending_kharcha: number;
    low_fastag: number;
    total: number;
  };
}

export interface ManagerTrip {
  trip_id: string;
  trip_number: string;
  status: string;
  origin: string;
  destination: string;
  driver?: {
    id?: string;
    name?: string;
    phone?: string;
  };
  vehicle?: {
    id?: string;
    registration_number?: string;
  };
  telemetry?: {
    latitude?: number;
    longitude?: number;
    speed?: number;
    marker_status?: string;
    remaining_km?: number;
  };
  progression?: {
    total_stops: number;
    completed_stops: number;
    progress_percent: number;
    all_stops_completed: boolean;
  };
  safety?: {
    has_active_sos: boolean;
    active_alerts_count: number;
    latest_alert_type?: string;
    is_deviated: boolean;
  };
}

export interface ManagerVehicle {
  id: string;
  registration_number: string;
  vehicle_number?: string;
  vehicle_type?: string;
  status: string;
  fuel_type?: string;
  current_mileage?: number;
  capacity?: number;
}

export interface PendingExpense {
  id: string;
  trip_id: string;
  driver_id: string;
  driver_name?: string;
  expense_type: string;
  amount: number;
  description?: string;
  receipt_url?: string;
  status: string;
  created_at: string;
}

class ManagerService {
  private getHeaders(): Record<string, string> {
    const token = useAuthStore.getState().token;
    return {
      'Content-Type': 'application/json',
      Accept: 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    };
  }

  async fetchDashboard(): Promise<ManagerDashboardKPIs> {
    const url = `${getApiBaseURL()}/api/v1/manager/dashboard`;
    const res = await fetch(url, { headers: this.getHeaders() });
    if (!res.ok) {
      throw new Error(`Failed to load manager dashboard: HTTP ${res.status}`);
    }
    return res.json();
  }

  async fetchActiveTrips(): Promise<ManagerTrip[]> {
    const url = `${getApiBaseURL()}/api/v1/control-tower/trips`;
    const res = await fetch(url, { headers: this.getHeaders() });
    if (!res.ok) {
      throw new Error(`Failed to load control tower trips: HTTP ${res.status}`);
    }
    const data = await res.json();
    return Array.isArray(data) ? data : [];
  }

  async fetchVehicles(): Promise<ManagerVehicle[]> {
    const url = `${getApiBaseURL()}/api/v1/vehicles`;
    const res = await fetch(url, { headers: this.getHeaders() });
    if (!res.ok) {
      throw new Error(`Failed to load fleet vehicles: HTTP ${res.status}`);
    }
    const data = await res.json();
    if (Array.isArray(data)) return data;
    if (data && Array.isArray(data.vehicles)) return data.vehicles;
    return [];
  }

  async fetchPendingExpenses(): Promise<PendingExpense[]> {
    const url = `${getApiBaseURL()}/api/v1/kharcha/pending`;
    const res = await fetch(url, { headers: this.getHeaders() });
    if (!res.ok) {
      throw new Error(`Failed to load pending expenses: HTTP ${res.status}`);
    }
    const data = await res.json();
    return Array.isArray(data) ? data : [];
  }

  async approveExpense(expenseId: string): Promise<{ status: string; expense_id: string }> {
    const url = `${getApiBaseURL()}/api/v1/kharcha/${expenseId}/approve`;
    const res = await fetch(url, {
      method: 'POST',
      headers: this.getHeaders(),
    });
    if (!res.ok) {
      const err = await res.text().catch(() => '');
      throw new Error(err || `Failed to approve expense: HTTP ${res.status}`);
    }
    return res.json();
  }

  async rejectExpense(expenseId: string, reason: string): Promise<{ status: string; expense_id: string }> {
    const url = `${getApiBaseURL()}/api/v1/kharcha/${expenseId}/reject`;
    const res = await fetch(url, {
      method: 'POST',
      headers: this.getHeaders(),
      body: JSON.stringify({ reason }),
    });
    if (!res.ok) {
      const err = await res.text().catch(() => '');
      throw new Error(err || `Failed to reject expense: HTTP ${res.status}`);
    }
    return res.json();
  }
}

export const managerService = new ManagerService();
