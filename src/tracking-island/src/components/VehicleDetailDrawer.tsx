import { useEffect, useState } from 'react';
import type { LiveVehicle } from '../types';
import {
  ActivityIcon,
  ClockIcon,
  CloseIcon,
  FuelIcon,
  LocateIcon,
  OdometerIcon,
  PhoneIcon,
  RouteIcon,
  ShieldIcon,
  SpeedIcon,
  TruckIcon,
  UserIcon,
} from './icons';

interface TripSummary {
  trip_number?: string;
  origin?: string;
  destination?: string;
  status?: string;
}

// Vehicle inspector drawer: ETA, driver, fuel, odometer from the live fix;
// trip summary lazily fetched only when the vehicle has an active trip.
export default function VehicleDetailDrawer({ vehicle, onClose, onFollow, following }: {
  vehicle: LiveVehicle | null;
  onClose: () => void;
  onFollow: () => void;
  following: boolean;
}) {
  const [trip, setTrip] = useState<TripSummary | null>(null);

  useEffect(() => {
    setTrip(null);
    if (!vehicle?.trip_id) return;
    let live = true;
    fetch('/api/v1/trips/' + encodeURIComponent(vehicle.trip_id) + '/summary',
      { headers: { Accept: 'application/json' }, credentials: 'same-origin' })
      .then((r) => (r.ok ? r.json() : null))
      .then((j) => { if (live && j) setTrip(j); })
      .catch(() => { /* drawer still shows the live fix */ });
    return () => { live = false; };
  }, [vehicle?.trip_id]);

  if (!vehicle) return null;
  const row = (icon: React.ReactNode, k: string, v: string | undefined) =>
    v ? (
      <div className="ti-kv">
        <span className="ti-kv-left">
          {icon}
          <span>{k}</span>
        </span>
        <b>{v}</b>
      </div>
    ) : null;

  return (
    <section id="intel-detail-panel" className="ti-drawer" aria-label="Vehicle details">
      <div className="ti-drawer-head">
        <TruckIcon className="ti-title-icon text-primary" />
        <b id="intel-vehicle-id">{vehicle.vehicle_number || vehicle.vehicle_id}</b>
        <span className="ti-pill">{vehicle.status.replace('_', ' ')}</span>
        <span className="ti-sp" />
        <button
          type="button"
          className={'ti-icon-btn' + (following ? ' on' : '')}
          onClick={onFollow}
          aria-pressed={following}
          title={following ? 'Stop following vehicle' : 'Follow vehicle on map'}
        >
          <LocateIcon className="ti-btn-svg" />
        </button>
        <button type="button" id="close-intel-btn" className="ti-icon-btn" onClick={onClose} aria-label="Close details">
          <CloseIcon className="ti-btn-svg" />
        </button>
      </div>
      <div className="ti-drawer-body">
        {row(<SpeedIcon className="ti-row-icon" />, 'Speed', `${Math.round(vehicle.speed)} km/h`)}
        {row(<OdometerIcon className="ti-row-icon" />, 'Odometer', vehicle.odometer !== undefined ? `${Math.round(vehicle.odometer)} km` : undefined)}
        {row(<FuelIcon className="ti-row-icon" />, 'Fuel', vehicle.fuel_level !== undefined ? `${vehicle.fuel_level}%` : undefined)}
        {row(<UserIcon className="ti-row-icon" />, 'Driver', vehicle.driver_name)}
        {row(<PhoneIcon className="ti-row-icon" />, 'Driver phone', vehicle.driver_phone)}
        {row(
          <ClockIcon className="ti-row-icon" />,
          'ETA window',
          vehicle.eta_min && vehicle.eta_max
            ? `${new Date(vehicle.eta_min).toLocaleTimeString()} – ${new Date(vehicle.eta_max).toLocaleTimeString()}`
            : undefined
        )}
        {row(<RouteIcon className="ti-row-icon" />, 'Remaining', vehicle.remaining_km !== undefined ? `${vehicle.remaining_km} km` : undefined)}
        {row(<ShieldIcon className="ti-row-icon" />, 'Provider', vehicle.provider)}
        {row(<ClockIcon className="ti-row-icon" />, 'Fix time', new Date(vehicle.ts).toLocaleTimeString())}
        {trip && (
          <>
            <div className="ti-sep">Active Trip</div>
            {row(<TruckIcon className="ti-row-icon" />, 'Trip', trip.trip_number)}
            {row(<RouteIcon className="ti-row-icon" />, 'Route', trip.origin && trip.destination ? `${trip.origin} → ${trip.destination}` : undefined)}
            {row(<ActivityIcon className="ti-row-icon" />, 'Status', trip.status)}
          </>
        )}
      </div>
    </section>
  );
}
