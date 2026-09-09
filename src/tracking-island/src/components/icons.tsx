import type { SVGProps } from 'react';

type IconProps = SVGProps<SVGSVGElement>;

// All icons use standard Lucide/Feather 24x24 viewBox, stroke="currentColor",
// strokeWidth=2, fill="none", strokeLinecap="round", strokeLinejoin="round".
// Strictly aligned with Avandab's internal/handlers/icons.go and enterprise fleet standards.

// --- Vehicle Category SVGs (Industry Standard Commercial Fleet Glyphs) ---

export function HeavyTruckIcon({ className = 'w-4 h-4', ...props }: IconProps) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" className={className} {...props}>
      <path d="M1 17h2" />
      <path d="M7 17h7" />
      <path d="M18 17h3a1 1 0 0 0 1-1v-4a1 1 0 0 0-.25-.66l-2.5-3A1 1 0 0 0 16.5 8H14v9" />
      <path d="M14 9h2.5l2 3H14V9z" />
      <rect x="1" y="5" width="12" height="12" rx="1" />
      <line x1="5" y1="5" x2="5" y2="17" />
      <line x1="9" y1="5" x2="9" y2="17" />
      <circle cx="5" cy="18" r="2" />
      <circle cx="16.5" cy="18" r="2" />
    </svg>
  );
}

export function MiniTruckIcon({ className = 'w-4 h-4', ...props }: IconProps) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" className={className} {...props}>
      <path d="M15 17h5a1 1 0 0 0 1-1v-4.5a1 1 0 0 0-.3-.7L18.2 8.3A1 1 0 0 0 17.5 8H14v9" />
      <path d="M14 9.5h3.2l1.3 2.5H14V9.5z" />
      <path d="M2 11h12v6H2z" />
      <line x1="2" y1="14" x2="14" y2="14" />
      <circle cx="6" cy="18" r="2" />
      <circle cx="16.5" cy="18" r="2" />
    </svg>
  );
}

export function BusIcon({ className = 'w-4 h-4', ...props }: IconProps) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" className={className} {...props}>
      <rect x="2" y="4" width="20" height="13" rx="2" />
      <line x1="2" y1="9" x2="22" y2="9" />
      <line x1="7" y1="4" x2="7" y2="9" />
      <line x1="12" y1="4" x2="12" y2="9" />
      <line x1="17" y1="4" x2="17" y2="9" />
      <line x1="4" y1="13" x2="6" y2="13" />
      <line x1="18" y1="13" x2="20" y2="13" />
      <circle cx="6.5" cy="18" r="2" />
      <circle cx="17.5" cy="18" r="2" />
    </svg>
  );
}

export function VanIcon({ className = 'w-4 h-4', ...props }: IconProps) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" className={className} {...props}>
      <path d="M2 17h3m4 0h6m4 0h2a1 1 0 0 0 1-1v-4.5a1 1 0 0 0-.25-.66l-2.5-3A1 1 0 0 0 17 7H3a1 1 0 0 0-1 1v8a1 1 0 0 0 1 1" />
      <path d="M14 7v10" />
      <path d="M14 8.5h2.8l2.2 3.5H14V8.5z" />
      <line x1="8" y1="10" x2="11" y2="10" />
      <circle cx="7" cy="18" r="2" />
      <circle cx="17" cy="18" r="2" />
    </svg>
  );
}

export function PickupIcon({ className = 'w-4 h-4', ...props }: IconProps) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" className={className} {...props}>
      <path d="M2 17h3m4 0h6m4 0h3a1 1 0 0 0 1-1v-3.5a1 1 0 0 0-.3-.7L20.2 9.3A1 1 0 0 0 19.5 9H13v8" />
      <path d="M13 10.5h6l1.3 2.5H13V10.5z" />
      <path d="M2 12h11v5H2z" />
      <circle cx="7" cy="18" r="2" />
      <circle cx="17" cy="18" r="2" />
    </svg>
  );
}

export function TempoIcon({ className = 'w-4 h-4', ...props }: IconProps) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" className={className} {...props}>
      <path d="M12 17h3m4 0h2a1 1 0 0 0 1-1v-3l-2.5-4.5A1 1 0 0 0 15.6 8H12v9" />
      <path d="M12 9.5h3.2l1.6 3H12V9.5z" />
      <path d="M3 11h9v6H3z" />
      <circle cx="6.5" cy="18" r="2" />
      <circle cx="18" cy="18" r="2" />
    </svg>
  );
}

export function VehicleTypeIcon({ type, className = 'w-4 h-4' }: { type?: string; className?: string }) {
  switch (type?.toLowerCase()) {
    case 'mini_truck':
      return <MiniTruckIcon className={className} />;
    case 'bus':
      return <BusIcon className={className} />;
    case 'van':
      return <VanIcon className={className} />;
    case 'pickup':
      return <PickupIcon className={className} />;
    case 'tempo':
      return <TempoIcon className={className} />;
    case 'truck':
    default:
      return <HeavyTruckIcon className={className} />;
  }
}

// Canonical generic truck icon matching internal/handlers/icons.go
export const TruckIcon = HeavyTruckIcon;

// --- Industry Standard Telemetry Radar Empty State Illustration ---

export function TelemetryEmptyIllustration({ className = 'w-48 h-32' }: { className?: string }) {
  return (
    <svg viewBox="0 0 220 140" fill="none" xmlns="http://www.w3.org/2000/svg" className={className}>
      <defs>
        <linearGradient id="ti-radar-beam" x1="110" y1="20" x2="110" y2="110" gradientUnits="userSpaceOnUse">
          <stop offset="0%" stopColor="#2563eb" stopOpacity="0.25" />
          <stop offset="100%" stopColor="#2563eb" stopOpacity="0.02" />
        </linearGradient>
        <radialGradient id="ti-radar-glow" cx="110" cy="65" r="55" gradientUnits="userSpaceOnUse">
          <stop offset="0%" stopColor="#2563eb" stopOpacity="0.14" />
          <stop offset="100%" stopColor="#2563eb" stopOpacity="0" />
        </radialGradient>
      </defs>
      {/* Radar range rings */}
      <circle cx="110" cy="65" r="55" stroke="#94a3b8" strokeWidth="1" strokeDasharray="3 3" opacity="0.4" />
      <circle cx="110" cy="65" r="38" stroke="#94a3b8" strokeWidth="1" opacity="0.5" />
      <circle cx="110" cy="65" r="20" stroke="#2563eb" strokeWidth="1.2" opacity="0.6" fill="url(#ti-radar-glow)" />
      {/* Precision crosshairs */}
      <line x1="110" y1="10" x2="110" y2="120" stroke="#94a3b8" strokeWidth="1" strokeDasharray="2 2" opacity="0.35" />
      <line x1="55" y1="65" x2="165" y2="65" stroke="#94a3b8" strokeWidth="1" strokeDasharray="2 2" opacity="0.35" />
      {/* Radar sweep sector */}
      <path d="M110 65 L145 38 A55 55 0 0 0 110 10 Z" fill="url(#ti-radar-beam)" />
      {/* Orbiting Telemetry Satellite */}
      <g transform="translate(155, 14) scale(0.85)" stroke="#2563eb" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" fill="none">
        <circle cx="12" cy="12" r="3" fill="#ffffff" />
        <path d="m4.93 4.93 4.24 4.24" />
        <path d="m14.83 14.83 4.24 4.24" />
        <rect x="1" y="1" width="5" height="5" rx="1" fill="#e2e8f0" />
        <rect x="18" y="18" width="5" height="5" rx="1" fill="#e2e8f0" />
      </g>
      {/* GNSS signal link beam */}
      <line x1="160" y1="22" x2="110" y2="65" stroke="#2563eb" strokeWidth="1.2" strokeDasharray="3 2" opacity="0.6" />
      {/* Target lock pin */}
      <circle cx="110" cy="65" r="3.5" fill="#2563eb" />
      <circle cx="110" cy="65" r="7" stroke="#2563eb" strokeWidth="1.5" />
      {/* Commercial Hauler Silhouette */}
      <g transform="translate(70, 80) scale(1.0)" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" fill="none" opacity="0.75">
        <rect x="2" y="4" width="36" height="17" rx="1.5" fill="#ffffff" />
        <path d="M38 9h12a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2h-12V9z" fill="#ffffff" />
        <path d="M42 12.5h7.5l2.5 4H42v-4z" />
        <line x1="14" y1="4" x2="14" y2="21" strokeDasharray="2 2" opacity="0.5" />
        <line x1="26" y1="4" x2="26" y2="21" strokeDasharray="2 2" opacity="0.5" />
        <circle cx="9" cy="23" r="3" fill="#ffffff" />
        <circle cx="29" cy="23" r="3" fill="#ffffff" />
        <circle cx="47" cy="23" r="3" fill="#ffffff" />
      </g>
      {/* Ground baseline */}
      <line x1="25" y1="108" x2="195" y2="108" stroke="#cbd5e1" strokeWidth="1.5" strokeLinecap="round" opacity="0.6" />
    </svg>
  );
}

// --- Common UI Controls ---

export function RefreshIcon({ className = 'w-4 h-4', ...props }: IconProps) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className} {...props}>
      <path d="M21 12a9 9 0 1 1-9-9c2.52 0 4.85.83 6.72 2.24" />
      <path d="M21 3v6h-6" />
    </svg>
  );
}

export function SearchIcon({ className = 'w-4 h-4', ...props }: IconProps) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className} {...props}>
      <circle cx="11" cy="11" r="8" />
      <path d="m21 21-4.3-4.3" />
    </svg>
  );
}

export function CloseIcon({ className = 'w-4 h-4', ...props }: IconProps) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className} {...props}>
      <path d="M18 6 6 18" />
      <path d="m6 6 12 12" />
    </svg>
  );
}

export function ChevronLeftIcon({ className = 'w-4 h-4', ...props }: IconProps) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className} {...props}>
      <path d="m15 18-6-6 6-6" />
    </svg>
  );
}

export function ChevronRightIcon({ className = 'w-4 h-4', ...props }: IconProps) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className} {...props}>
      <path d="m9 18 6-6-6-6" />
    </svg>
  );
}

export function PlusIcon({ className = 'w-4 h-4', ...props }: IconProps) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className} {...props}>
      <path d="M5 12h14" />
      <path d="M12 5v14" />
    </svg>
  );
}

export function ArrowRightIcon({ className = 'w-4 h-4', ...props }: IconProps) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className} {...props}>
      <path d="M5 12h14" />
      <path d="m12 5 7 7-7 7" />
    </svg>
  );
}

export function LocateIcon({ className = 'w-4 h-4', ...props }: IconProps) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className} {...props}>
      <circle cx="12" cy="12" r="3" />
      <path d="M12 2v4" />
      <path d="M12 18v4" />
      <path d="M2 12h4" />
      <path d="M18 12h4" />
    </svg>
  );
}

export function ZapIcon({ className = 'w-4 h-4', ...props }: IconProps) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className} {...props}>
      <polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2" />
    </svg>
  );
}

export function WarningIcon({ className = 'w-4 h-4', ...props }: IconProps) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className} {...props}>
      <path d="m21.73 18-8-14a2 2 0 0 0-3.46 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3" />
      <line x1="12" x2="12" y1="9" y2="13" />
      <line x1="12" x2="12.01" y1="17" y2="17" />
    </svg>
  );
}

export function RadioIcon({ className = 'w-4 h-4', ...props }: IconProps) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className} {...props}>
      <circle cx="12" cy="12" r="2" />
      <path d="M16.24 7.76a6 6 0 0 1 0 8.49m-8.48-.01a6 6 0 0 1 0-8.49m11.31-2.82a10 10 0 0 1 0 14.14m-14.14 0a10 10 0 0 1 0-14.14" />
    </svg>
  );
}

export function LayersIcon({ className = 'w-4 h-4', ...props }: IconProps) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className} {...props}>
      <polygon points="12 2 2 7 12 12 22 7 12 2" />
      <polyline points="2 17 12 22 22 17" />
      <polyline points="2 12 12 17 22 12" />
    </svg>
  );
}

export function MaximizeIcon({ className = 'w-4 h-4', ...props }: IconProps) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className} {...props}>
      <path d="M8 3H5a2 2 0 0 0-2 2v3m18 0V5a2 2 0 0 0-2-2h-3m0 18h3a2 2 0 0 0 2-2v-3M3 16v3a2 2 0 0 0 2 2h3" />
    </svg>
  );
}

export function SpeedIcon({ className = 'w-4 h-4', ...props }: IconProps) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className} {...props}>
      <path d="m12 14 4-4" />
      <path d="M3.34 19a10 10 0 1 1 17.32 0" />
    </svg>
  );
}

export function OdometerIcon({ className = 'w-4 h-4', ...props }: IconProps) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className} {...props}>
      <polygon points="3 11 22 2 13 21 11 13 3 11" />
    </svg>
  );
}

export function FuelIcon({ className = 'w-4 h-4', ...props }: IconProps) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className} {...props}>
      <line x1="3" x2="3" y1="22" y2="13.5" />
      <path d="M3 13.5a2.5 2.5 0 0 1 5 0V22" />
      <line x1="4" x2="7" y1="9" y2="9" />
      <path d="M14 22V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v18" />
      <line x1="14" x2="22" y1="13" y2="13" />
    </svg>
  );
}

export function UserIcon({ className = 'w-4 h-4', ...props }: IconProps) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className} {...props}>
      <path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2" />
      <circle cx="9" cy="7" r="4" />
    </svg>
  );
}

export function PhoneIcon({ className = 'w-4 h-4', ...props }: IconProps) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className} {...props}>
      <path d="M22 16.92v3a2 2 0 0 1-2.18 2 19.79 19.79 0 0 1-8.63-3.07 19.5 19.5 0 0 1-6-6 19.79 19.79 0 0 1-3.07-8.67A2 2 0 0 1 4.11 2h3a2 2 0 0 1 2 1.72 12.84 12.84 0 0 0 .7 2.81 2 2 0 0 1-.45 2.11L8.09 9.91a16 16 0 0 0 6 6l1.27-1.27a2 2 0 0 1 2.11-.45 12.84 12.84 0 0 0 2.81.7A2 2 0 0 1 22 16.92z" />
    </svg>
  );
}

export function ClockIcon({ className = 'w-4 h-4', ...props }: IconProps) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className} {...props}>
      <circle cx="12" cy="12" r="10" />
      <polyline points="12 6 12 12 16 14" />
    </svg>
  );
}

export function RouteIcon({ className = 'w-4 h-4', ...props }: IconProps) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className} {...props}>
      <circle cx="6" cy="19" r="3" />
      <path d="M9 19h8.5a3.5 3.5 0 0 0 0-7h-11a3.5 3.5 0 0 1 0-7H15" />
      <circle cx="18" cy="5" r="3" />
    </svg>
  );
}

export function ShieldIcon({ className = 'w-4 h-4', ...props }: IconProps) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className} {...props}>
      <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
    </svg>
  );
}

export function ActivityIcon({ className = 'w-4 h-4', ...props }: IconProps) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className} {...props}>
      <polyline points="22 12 18 12 15 21 9 3 6 12 2 12" />
    </svg>
  );
}
