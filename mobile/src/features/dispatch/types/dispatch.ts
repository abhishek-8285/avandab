export type DispatchOfferStatus = 'offered' | 'accepted' | 'rejected' | 'expired' | 'cancelled';

export interface DispatchOffer {
  id: string;
  booking_id: string;
  driver_id: string;
  vehicle_id: string;
  status: DispatchOfferStatus;
  offered_at: string;
  expires_at: string;
  origin?: string;
  destination?: string;
  cargo_type?: string;
  payout?: number;
}
