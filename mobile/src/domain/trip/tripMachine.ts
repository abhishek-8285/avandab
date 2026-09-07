import { Trip } from '../../types/api';

export type TripStatus = Trip['status']; // PENDING | IN_TRANSIT | COMPLETED | CANCELLED (mobile collapsed from 9 backend)
export type TripCommand = 'ACCEPT' | 'START' | 'REACH_PICKUP' | 'DELIVER' | 'COMPLETE' | 'CANCEL';

// Backend 9-way: draft/scheduled/assigned → PENDING, started/reached_pickup/in_transit → IN_TRANSIT, delivered/completed → COMPLETED
// Machine collapsed to 4 mobile states; guards cover all backend mapped values
//
// Gap-5 contract — explicit collapsed mapping (backend 9-state → mobile 4-state).
// COLLAPSE IS LOSSY BY DESIGN: mobile never reconstructs the exact backend
// sub-state from a collapsed value (e.g. IN_TRANSIT could be started,
// reached_pickup, or in_transit). Detail screens needing granularity must read
// the raw backend `status` string from the API payload, not this map.
// Outbound is command-based (POST /trips/{id}/start, /cancel, …), never a raw
// status PUT — see MOBILE_TO_BACKEND hint below.
export type BackendTripStatus =
  | 'draft'
  | 'scheduled'
  | 'assigned'
  | 'started'
  | 'reached_pickup'
  | 'in_transit'
  | 'delivered'
  | 'completed'
  | 'cancelled';

export const BACKEND_TO_MOBILE: Record<BackendTripStatus, TripStatus> = {
  draft: 'PENDING',
  scheduled: 'PENDING',
  assigned: 'PENDING',
  started: 'IN_TRANSIT',
  reached_pickup: 'IN_TRANSIT',
  in_transit: 'IN_TRANSIT',
  delivered: 'COMPLETED',
  completed: 'COMPLETED',
  cancelled: 'CANCELLED',
};

// Outbound hint only — mobile sends commands (ACCEPT/START/REACH_PICKUP/
// DELIVER/COMPLETE/CANCEL via tripsApi), not status values. This shows the
// canonical backend status each collapsed mobile state corresponds to when a
// backend echo is needed (e.g. optimistic UI reconciliation). Do NOT PUT it.
export const MOBILE_TO_BACKEND: Record<TripStatus, BackendTripStatus> = {
  PENDING: 'assigned',
  IN_TRANSIT: 'in_transit',
  COMPLETED: 'completed',
  CANCELLED: 'cancelled',
};

// Normalise any raw backend status string (incl. legacy 'pending' alias seen
// in tripMapper RawTrip) to the collapsed mobile state. Unknown → PENDING.
export function toMobileStatus(raw: string): TripStatus {
  return (BACKEND_TO_MOBILE as Record<string, TripStatus>)[raw] ?? 'PENDING';
}
const TRANSITIONS: Record<TripStatus, Partial<Record<TripCommand, TripStatus>>> = {
  PENDING: { ACCEPT: 'IN_TRANSIT', START: 'IN_TRANSIT', CANCEL: 'CANCELLED' },
  IN_TRANSIT: { REACH_PICKUP: 'IN_TRANSIT', DELIVER: 'COMPLETED', COMPLETE: 'COMPLETED', CANCEL: 'CANCELLED' },
  COMPLETED: {},
  CANCELLED: {},
};

export function canTransition(status: TripStatus, command: TripCommand): boolean {
  return Boolean(TRANSITIONS[status]?.[command]);
}

export function nextStatus(status: TripStatus, command: TripCommand): TripStatus | null {
  return TRANSITIONS[status]?.[command] ?? null;
}

export function assertTransition(status: TripStatus, command: TripCommand): void {
  if (!canTransition(status, command)) {
    throw new Error(`Illegal transition ${status} --${command}--> ?`);
  }
}

// Pure helper for UI chips: active vs history
export function isActiveStatus(s: TripStatus): boolean {
  return s === 'PENDING' || s === 'IN_TRANSIT';
}

export function isTerminal(s: TripStatus): boolean {
  return s === 'COMPLETED' || s === 'CANCELLED';
}
