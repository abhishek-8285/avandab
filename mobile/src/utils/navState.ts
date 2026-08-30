import { Trip, TripStop } from '../types/api';

export interface NavState {
  /** True when a real trip backs this screen. */
  hasTrip: boolean;
  /** Current leg label for the instruction card. */
  legTitle: string;
  /** Where the driver is heading right now (real origin/destination name). */
  nextStopAddress: string;
  /** e.g. "STOP 01/02" derived from trip status — never fabricated counts. */
  stepLabel: string;
  /** Real trip reference, e.g. "REF #TRP-8492" (null without trip). */
  refLabel: string | null;
  /** Real GPS speed in km/h rounded, or null when unknown. */
  speedKmh: number | null;
  /** Status line under the brand block. */
  statusLine: string;
  /** Currently active stop if trip has multi-stop sequence. */
  activeStop?: TripStop | null;
  /** Index of active stop. */
  activeStopIndex?: number;
  /** Total number of stops. */
  totalStops?: number;
  /** True when all multi-stops have been serviced/completed. */
  allStopsCompleted?: boolean;
}

const mpsToKmh = (mps: number | null | undefined): number | null =>
  typeof mps === 'number' && !isNaN(mps) && mps >= 0 ? Math.round(mps * 3.6) : null;

/**
 * Derives the navigation HUD state from real trip + GPS data only.
 * Every field traces back to backend trip fields or device GPS —
 * the helper never invents distances, ETAs, turn instructions or stop counts.
 */
export function deriveNavState(trip: Trip | null | undefined, speedMps?: number | null): NavState {
  const speedKmh = mpsToKmh(speedMps);

  if (!trip) {
    return {
      hasTrip: false,
      legTitle: 'NO ACTIVE TRIP',
      nextStopAddress: 'Select a trip from the dispatch list',
      stepLabel: '—',
      refLabel: null,
      speedKmh,
      statusLine: 'NO TRIP SELECTED',
    };
  }

  const finished = trip.status === 'COMPLETED' || trip.status === 'CANCELLED';

  // Multi-stop journey resolution
  if (trip.stops && trip.stops.length > 0) {
    const sortedStops = [...trip.stops].sort((a, b) => a.stopSequence - b.stopSequence);
    const activeIdx = sortedStops.findIndex((s) => s.status !== 'completed' && s.status !== 'skipped');
    const allCompleted = activeIdx === -1;
    const activeStop = allCompleted ? sortedStops[sortedStops.length - 1] : sortedStops[activeIdx];
    const totalStops = sortedStops.length;
    const seq = activeStop ? activeStop.stopSequence : 1;

    const pad = (n: number) => String(n).padStart(2, '0');
    const stopTypeLabel = activeStop?.stopType ? activeStop.stopType.toUpperCase() : 'STOP';

    let legTitle = 'HEAD TO STOP';
    if (allCompleted || finished) {
      legTitle = trip.status === 'CANCELLED' ? 'TRIP CANCELLED' : 'ALL STOPS COMPLETED';
    } else if (activeStop.status === 'arrived' || activeStop.status === 'servicing') {
      legTitle = activeStop.stopType === 'pickup' ? 'AT PICKUP' : 'AT DROP LOCATION';
    } else if (activeStop.stopType === 'pickup') {
      legTitle = 'HEAD TO PICKUP';
    } else if (activeStop.stopType === 'drop') {
      legTitle = 'DELIVER TO';
    }

    return {
      hasTrip: true,
      legTitle,
      nextStopAddress: allCompleted
        ? trip.destination || activeStop?.locationName || 'Trip Complete'
        : activeStop?.locationName || activeStop?.address || 'Stop location',
      stepLabel: allCompleted ? 'COMPLETE' : `STOP ${pad(seq)}/${pad(totalStops)} · ${stopTypeLabel}`,
      refLabel: trip.tripNumber ? `REF #${trip.tripNumber}` : null,
      speedKmh,
      statusLine: trip.tripNumber ? `TRIP #${trip.tripNumber} · ${trip.status}` : `STATUS ${trip.status}`,
      activeStop: allCompleted ? null : activeStop,
      activeStopIndex: allCompleted ? totalStops : activeIdx,
      totalStops,
      allStopsCompleted: allCompleted,
    };
  }

  // Two-leg journey: leg 1 = head to pickup (origin), leg 2 = deliver (destination).
  const onLeg1 = trip.status === 'PENDING';

  const legTitle = finished
    ? trip.status === 'CANCELLED'
      ? 'TRIP CANCELLED'
      : 'TRIP DELIVERED'
    : onLeg1
      ? 'HEAD TO PICKUP'
      : 'DELIVER TO';

  return {
    hasTrip: true,
    legTitle,
    nextStopAddress: finished ? trip.destination : onLeg1 ? trip.origin || 'Pickup point' : trip.destination || 'Destination',
    stepLabel: finished ? 'COMPLETE' : onLeg1 ? 'STOP 01/02 · PICKUP' : 'STOP 02/02 · DROP',
    refLabel: trip.tripNumber ? `REF #${trip.tripNumber}` : null,
    speedKmh,
    statusLine: trip.tripNumber ? `TRIP #${trip.tripNumber} · ${trip.status}` : `STATUS ${trip.status}`,
  };
}
