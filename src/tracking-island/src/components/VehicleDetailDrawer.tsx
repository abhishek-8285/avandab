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
  VehicleTypeIcon,
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

  // Move focus into the drawer when it opens for a new vehicle (Escape closes via global handler).
  useEffect(() => {
    if (vehicle) document.getElementById('close-intel-btn')?.focus();
  }, [vehicle?.vehicle_id]);

  if (!vehicle) return null;
  // Migration-00117 parity: battery/motion/valid render from live payload.
  // Absent fields show honest placeholders, never fabricated values.
  const batt = vehicle.battery_level;
  const battText = batt !== undefined ? `${Math.round(batt)}%` : '—';
  const battClass = batt === undefined ? '' : batt <= 20 ? 'text-status-error' : 'text-status-success';
  const motion = vehicle.motion;
  const deviceText =
    motion === true ? 'MOVING' + (vehicle.valid === false ? ' · NO GPS FIX' : '')
    : motion === false ? 'PARKED' + (vehicle.valid === false ? ' · NO GPS FIX' : '')
    : 'OK';
  const timeFmt = new Intl.DateTimeFormat('en-IN', { hour: '2-digit', minute: '2-digit', second: '2-digit' });
  const row = (icon: React.ReactNode, k: string, v: string | undefined, id?: string) =>
    v ? (
      <div className="ti-kv">
        <dt className="ti-kv-left">
          {icon}
          <span>{k}</span>
        </dt>
        <dd id={id}>{v}</dd>
      </div>
    ) : null;

  return (
    <section id="intel-detail-panel" className="ti-drawer" aria-label="Vehicle details">
      <div className="ti-drawer-head">
        <VehicleTypeIcon type={vehicle.vehicle_type} className="ti-title-icon text-primary" />
        <b id="intel-vehicle-id">{vehicle.vehicle_number || vehicle.vehicle_id}</b>
        <span className="ti-pill">{vehicle.status.replace('_', ' ')}</span>
        <span className="ti-sp" />
        <button
          type="button"
          className={'ti-icon-btn' + (following ? ' on' : '')}
          onClick={onFollow}
          aria-pressed={following}
          aria-label={following ? 'Stop following vehicle' : 'Follow vehicle on map'}
          title={following ? 'Stop following vehicle' : 'Follow vehicle on map'}
        >
          <LocateIcon className="ti-btn-svg" />
        </button>
        <button type="button" id="close-intel-btn" className="ti-icon-btn" onClick={onClose} aria-label="Close details">
          <CloseIcon className="ti-btn-svg" />
        </button>
      </div>
      <dl className="ti-drawer-body">
        {row(<VehicleTypeIcon type={vehicle.vehicle_type} className="ti-row-icon" />, 'Class', (vehicle.vehicle_type || 'truck').replace('_', ' ').toUpperCase())}
        {row(<SpeedIcon className="ti-row-icon" />, 'Speed', `${Math.round(vehicle.speed)}\u00A0km/h`, 'intel-speed')}
        {row(<OdometerIcon className="ti-row-icon" />, 'Odometer', vehicle.odometer !== undefined ? `${Math.round(vehicle.odometer)}\u00A0km` : undefined, 'intel-odometer')}
        {row(<FuelIcon className="ti-row-icon" />, 'Fuel', vehicle.fuel_level !== undefined ? `${vehicle.fuel_level}%` : undefined, 'intel-fuel')}
        <div className="ti-kv">
          <dt className="ti-kv-left">
            <ActivityIcon className="ti-row-icon" />
            <span>Battery</span>
          </dt>
          <dd id="intel-battery" className={battClass}>{battText}</dd>
        </div>
        <div className="ti-kv">
          <dt className="ti-kv-left">
            <ShieldIcon className="ti-row-icon" />
            <span>Device</span>
          </dt>
          <dd id="intel-device">{deviceText}</dd>
        </div>
        {row(<UserIcon className="ti-row-icon" />, 'Driver', vehicle.driver_name)}
        {row(<PhoneIcon className="ti-row-icon" />, 'Driver phone', vehicle.driver_phone)}
        {row(
          <ClockIcon className="ti-row-icon" />,
          'ETA window',
          vehicle.eta_min && vehicle.eta_max
            ? `${timeFmt.format(new Date(vehicle.eta_min))} – ${timeFmt.format(new Date(vehicle.eta_max))}`
            : undefined
        )}
        {row(<RouteIcon className="ti-row-icon" />, 'Remaining', vehicle.remaining_km !== undefined ? `${vehicle.remaining_km}\u00A0km` : undefined)}
        {row(<ShieldIcon className="ti-row-icon" />, 'Provider', vehicle.provider)}
        {row(<ClockIcon className="ti-row-icon" />, 'Fix time', timeFmt.format(new Date(vehicle.ts)))}
        {trip && (
          <div aria-live="polite">
            <div className="ti-sep">Active Trip</div>
            {row(<TruckIcon className="ti-row-icon" />, 'Trip', trip.trip_number)}
            {row(<RouteIcon className="ti-row-icon" />, 'Route', trip.origin && trip.destination ? `${trip.origin} → ${trip.destination}` : undefined)}
            {row(<ActivityIcon className="ti-row-icon" />, 'Status', trip.status)}
          </div>
        )}
      </dl>
    </section>
  );
}
