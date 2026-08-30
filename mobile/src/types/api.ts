// Type definitions for Avandab API Data Models

export interface Driver {
  id: string;
  name: string;
  phone: string;
  status: 'available' | 'on_trip' | 'off_duty';
  currentLocation?: {
    latitude: number;
    longitude: number;
  };
}

export type StopType = 'pickup' | 'drop' | 'intermediate';
export type StopStatus = 'pending' | 'en_route' | 'arrived' | 'servicing' | 'completed' | 'skipped';

export interface TripStop {
  id: string;
  tripId: string;
  stopSequence: number;
  stopType: StopType;
  locationName: string;
  address?: string;
  latitude: number;
  longitude: number;
  geofenceRadiusM?: number;
  plannedArrival?: string;
  actualArrival?: string;
  departureTime?: string;
  status: StopStatus;
  requiresPOD: boolean;
  requiresOTP: boolean;
  consigneeName?: string;
  consigneePhone?: string;
  otpCode?: string;
  podUrl?: string;
  signatureUrl?: string;
}

export interface Trip {
  id: string;
  tripNumber: string;
  driverName: string;
  vehiclePlate: string;
  origin: string;
  destination: string;
  status: 'PENDING' | 'IN_TRANSIT' | 'COMPLETED' | 'CANCELLED';
  startTime: string;
  stops?: TripStop[];
  currentStopId?: string;
  currentStopSequence?: number;
}

export interface Vehicle {
  id: string;
  plateNumber: string;
  model: string;
  capacityKg: number;
  status: 'active' | 'maintenance';
}
