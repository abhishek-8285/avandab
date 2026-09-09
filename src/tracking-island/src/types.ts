// Data contracts — must match the Go backend exactly.
// LiveVehicle mirrors internal/telemetry/live.go (JSON tags).
// /api/v1/telemetry/live returns a BARE ARRAY of these (no wrapper object).
export type MarkerState = 'running' | 'stopped' | 'no_signal' | 'maintenance_due';

export interface LiveVehicle {
  vehicle_id: string;
  vehicle_number?: string;
  trip_id?: string;
  lat: number;
  lng: number;
  speed: number;
  heading?: number;
  fuel_level?: number;
  odometer?: number;
  status: MarkerState;
  battery_level?: number;
  gsm_signal?: number;
  provider?: string;
  motion?: boolean;
  valid?: boolean;
  eta_min?: string;
  eta_max?: string;
  eta_method?: string;
  remaining_km?: number;
  route_km?: number;
  driver_name?: string;
  driver_phone?: string;
  ts: string;
}

// GeofenceZone mirrors internal/telemetry/geofences.go.
// Polygon vertices are domain.Point: {lat, lng} — NOT {latitude, longitude}.
export interface GeofencePoint {
  lat: number;
  lng: number;
}

export interface GeofenceZone {
  id: string;
  name: string;
  kind: string;
  shape: 'polygon' | 'circle';
  center_lat: number;
  center_lng: number;
  radius_m: number;
  polygon?: GeofencePoint[];
}

// TrackingMapConfig mirrors the MapConfig map injected by
// internal/handlers/tracking.go Page() (keys are case-sensitive).
export interface TrackingMapConfig {
  Provider: 'auto' | 'osm' | 'google';
  GoogleStyle?: string;
  GL?: string;
  OSMUrl: string;
  PollSec: number;
  LiveEndpoint: string;
  StreamEndpoint: string;
  GeofenceEndpoint: string;
}

export type StatusFilter = 'all' | 'running' | 'stopped' | 'alert';
export type SortKey = 'status' | 'name' | 'speed' | 'fresh';
export type ConnMode = 'live' | 'poll' | 'connecting' | 'offline';
