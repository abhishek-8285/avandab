export type TripExecutionStatus =
  | 'assigned'
  | 'reached_pickup'
  | 'loading'
  | 'in_transit'
  | 'reached_delivery'
  | 'unloading'
  | 'delivered'
  | 'cancelled';

export interface ActiveTrip {
  id: string;
  booking_id: string;
  driver_id: string;
  vehicle_id: string;
  status: TripExecutionStatus;
  started_at?: string;
  origin?: string;
  destination?: string;
  pod_url?: string;
}
