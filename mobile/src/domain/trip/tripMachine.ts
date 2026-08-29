import { Trip } from '../../types/api';

export type TripStatus = Trip['status']; // PENDING | IN_TRANSIT | COMPLETED | CANCELLED (mobile collapsed from 9 backend)
export type TripCommand = 'ACCEPT' | 'START' | 'REACH_PICKUP' | 'DELIVER' | 'COMPLETE' | 'CANCEL';

// Backend 9-way: draft/scheduled/assigned → PENDING, started/reached_pickup/in_transit → IN_TRANSIT, delivered/completed → COMPLETED
// Machine collapsed to 4 mobile states; guards cover all backend mapped values
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
