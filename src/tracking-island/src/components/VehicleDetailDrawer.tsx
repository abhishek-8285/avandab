import { useEffect, useState } from 'react';
import type { LiveVehicle } from '../types';

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
  const row = (k: string, v: string | undefined) =>
    v ? <div className="ti-kv"><span>{k}</span><b>{v}</b></div> : null;

  return (
    <section id="intel-detail-panel" className="ti-drawer" aria-label="Vehicle details">
      <div className="ti-drawer-head">
        <b id="intel-vehicle-id">{vehicle.vehicle_number || vehicle.vehicle_id}</b>
        <span className="ti-pill">{vehicle.status.replace('_', ' ')}</span>
        <span className="ti-sp" />
        <button type="button" className="ti-icon-btn" onClick={onFollow} aria-pressed={following} title="Follow vehicle">
          {following ? '◉' : '◎'}
        </button>
        <button type="button" id="close-intel-btn" className="ti-icon-btn" onClick={onClose} aria-label="Close details">×</button>
      </div>
      <div className="ti-drawer-body">
        <div id="intel-speed" className="ti-kv"><span>Speed</span><b>{Math.round(vehicle.speed)} km/h</b></div>
        {row('Odometer', vehicle.odometer !== undefined ? Math.round(vehicle.odometer) + ' km' : undefined)}
        {vehicle.fuel_level !== undefined
          ? <div id="intel-fuel" className="ti-kv"><span>Fuel</span><b>{vehicle.fuel_level}%</b></div>
          : null}
        {row('Driver', vehicle.driver_name)}
        {row('Driver phone', vehicle.driver_phone)}
        {row('ETA window', vehicle.eta_min && vehicle.eta_max
          ? new Date(vehicle.eta_min).toLocaleString() + ' → ' + new Date(vehicle.eta_max).toLocaleString() : undefined)}
        {row('Remaining', vehicle.remaining_km !== undefined ? vehicle.remaining_km + ' km' : undefined)}
        {row('Provider', vehicle.provider)}
        {row('Fix time', new Date(vehicle.ts).toLocaleString())}
        {trip && (
          <>
            <div className="ti-sep">Trip</div>
            {row('Trip', trip.trip_number)}
            {row('Route', trip.origin && trip.destination ? trip.origin + ' → ' + trip.destination : undefined)}
            {row('Status', trip.status)}
          </>
        )}
      </div>
    </section>
  );
}
