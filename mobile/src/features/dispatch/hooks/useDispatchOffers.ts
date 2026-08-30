import { useState, useEffect, useCallback } from 'react';
import { dispatchApi } from '../api/dispatchApi';
import { DispatchOffer } from '../types/dispatch';
import { commandQueue } from '../../../core/sync/commandQueue';
import { commandProcessor } from '../../../core/sync/commandProcessor';

export function useDispatchOffers(token?: string) {
  const [offers, setOffers] = useState<DispatchOffer[]>([]);
  const [loading, setLoading] = useState<boolean>(false);

  const fetchOffers = useCallback(async () => {
    if (!token) return;
    setLoading(true);
    try {
      const data = await dispatchApi.getPendingOffers(token);
      setOffers(data);
    } catch {
      // Retain existing state if network drops
    } finally {
      setLoading(false);
    }
  }, [token]);

  useEffect(() => {
    fetchOffers();
    const interval = setInterval(fetchOffers, 10000);
    return () => clearInterval(interval);
  }, [fetchOffers]);

  const acceptOffer = async (offerId: string): Promise<void> => {
    if (!token) throw new Error('Authentication required');

    // 1. Optimistic removal from UI list
    setOffers((prev) => prev.filter((o) => o.id !== offerId));

    // 2. Durable offline command enqueue
    await commandQueue.enqueueCommand('ACCEPT_OFFER', { offer_id: offerId });

    // 3. Trigger immediate sync flush
    await commandProcessor.flush(token);
  };

  const rejectOffer = async (offerId: string): Promise<void> => {
    if (!token) throw new Error('Authentication required');

    setOffers((prev) => prev.filter((o) => o.id !== offerId));
    await commandQueue.enqueueCommand('REJECT_OFFER', { offer_id: offerId });
    await commandProcessor.flush(token);
  };

  return {
    offers,
    loading,
    refreshOffers: fetchOffers,
    acceptOffer,
    rejectOffer,
  };
}
