import { useState } from 'react';
import { ActiveTrip, TripExecutionStatus } from '../types/trip';
import { commandQueue } from '../../../core/sync/commandQueue';
import { commandProcessor } from '../../../core/sync/commandProcessor';
import { startBackgroundLocationUpdates } from '../../telemetry/native/backgroundLocationTask';

export function useActiveTrip(token?: string, initialTrip?: ActiveTrip | null) {
  const [trip, setTrip] = useState<ActiveTrip | null>(initialTrip || null);
  const [isProcessing, setIsProcessing] = useState<boolean>(false);

  const transitionTrip = async (
    commandType: string,
    nextStatus: TripExecutionStatus,
    payload: Record<string, any> = {}
  ): Promise<void> => {
    if (!token || !trip) return;

    setIsProcessing(true);
    try {
      // 1. Optimistic local state update
      setTrip((prev) => (prev ? { ...prev, status: nextStatus } : null));

      // 2. Adjust telemetry policy according to trip state
      if (nextStatus === 'in_transit' || nextStatus === 'reached_pickup') {
        await startBackgroundLocationUpdates('TRIP_ACTIVE').catch(() => {});
      } else if (nextStatus === 'delivered' || nextStatus === 'cancelled') {
        await startBackgroundLocationUpdates('AVAILABLE').catch(() => {});
      }

      // 3. Enqueue durable offline command
      await commandQueue.enqueueCommand(commandType, {
        trip_id: trip.id,
        ...payload,
      });

      // 4. Trigger sync flush
      await commandProcessor.flush(token);
    } finally {
      setIsProcessing(false);
    }
  };

  const startTrip = () => transitionTrip('START_TRIP', 'reached_pickup');
  const arrivePickup = () => transitionTrip('ARRIVE_PICKUP', 'reached_pickup');
  const startLoading = () => transitionTrip('START_LOADING', 'loading');
  const completeLoading = () => transitionTrip('COMPLETE_LOADING', 'in_transit');
  const arriveDelivery = () => transitionTrip('ARRIVE_DELIVERY', 'reached_delivery');
  const startUnloading = () => transitionTrip('START_UNLOADING', 'unloading');
  const completeTrip = (podData?: Record<string, any>) =>
    transitionTrip('COMPLETE_TRIP', 'delivered', podData || {});

  return {
    trip,
    isProcessing,
    setTrip,
    startTrip,
    arrivePickup,
    startLoading,
    completeLoading,
    arriveDelivery,
    startUnloading,
    completeTrip,
  };
}
