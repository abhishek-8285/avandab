import { useEffect, useMemo, useRef, useState } from 'react';
import type { LiveVehicle, SortKey, StatusFilter } from '../types';
import { bucketOf, useFleetFilter } from '../hooks';
import {
  ArrowRightIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  CloseIcon,
  PlusIcon,
  SearchIcon,
  TelemetryEmptyIllustration,
  TruckIcon,
  VehicleTypeIcon,
  ZapIcon,
} from './icons';

interface Props {
  vehicles: Map<string, LiveVehicle>;
  selectedId: string | null;
  onSelect: (id: string) => void;
}

const TABS: { key: StatusFilter; label: string }[] = [
  { key: 'all', label: 'All' }, { key: 'running', label: 'Moving' },
  { key: 'stopped', label: 'Idle' }, { key: 'alert', label: 'Alert' },
];
const SORTS: { key: SortKey; label: string }[] = [
  { key: 'status', label: 'Status' }, { key: 'name', label: 'A-Z' },
  { key: 'speed', label: 'Speed' }, { key: 'fresh', label: 'Fresh' },
];

const DOT: Record<string, string> = {
  running: '#059669', stopped: '#d97706', alert: '#dc2626',
};

const STATUS_LABEL: Record<string, string> = {
  running: 'Moving', stopped: 'Idle', no_signal: 'No signal', maintenance_due: 'Maintenance due',
};

const SPEED_LIMIT_KMH = 80;

// Collapsible fleet registry: fuzzy search (/ hotkey), status tabs,
// sort control, windowed list (renders visible slice + overscan only).
export default function FleetSidebar({ vehicles, selectedId, onSelect }: Props) {
  const [query, setQuery] = useState('');
  const [status, setStatus] = useState<StatusFilter>('all');
  const [sort, setSort] = useState<SortKey>('status');
  const [collapsed, setCollapsed] = useState(false);
  // Below lg the registry starts stowed off-canvas; the rail re-opens it.
  const [mobileOpen, setMobileOpen] = useState(() =>
    typeof window !== 'undefined' ? window.innerWidth >= 1024 : true);
  const [scrollTop, setScrollTop] = useState(0);
  const searchRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);

  const pick = (id: string) => {
    onSelect(id);
    // Stow the registry after a pick on small screens (map theater owns focus).
    if (typeof window !== 'undefined' && window.innerWidth < 1024) setMobileOpen(false);
  };

  const list = useFleetFilter(vehicles, query, status, sort);
  const counts = useMemo(() => {
    const c = { all: 0, running: 0, stopped: 0, alert: 0 };
    for (const v of vehicles.values()) { c.all += 1; c[bucketOf(v)] += 1; }
    return c;
  }, [vehicles]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const tag = (e.target as HTMLElement)?.tagName;
      if (e.key === '/' && tag !== 'INPUT' && tag !== 'TEXTAREA') { e.preventDefault(); searchRef.current?.focus(); }
      if (e.key === 'Escape') { setQuery(''); }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  // Windowed render: 56px rows, 12 overscan.
  const ROW = 56;
  const start = Math.max(0, Math.floor(scrollTop / ROW) - 12);
  const visible = list.slice(start, start + Math.ceil(600 / ROW) + 24);

  if (collapsed) {
    return (
      <button type="button" id="drawer-expand-rail" className="ti-rail" onClick={() => { setCollapsed(false); setMobileOpen(true); }} aria-label="Show fleet panel">
        <ChevronRightIcon className="ti-rail-icon" />
        <span className="ti-rail-count">{counts.all}</span>
      </button>
    );
  }

  return (
    <>
    {!mobileOpen && (
      <button type="button" id="drawer-expand-rail" className="ti-rail ti-rail-mobile" onClick={() => setMobileOpen(true)} aria-label="Show fleet panel">
        <ChevronRightIcon className="ti-rail-icon" />
        <span className="ti-rail-count">{counts.all}</span>
      </button>
    )}
    <aside id="fleet-drawer" className={'ti-sidebar' + (mobileOpen ? ' open' : '')}>
      <div className="ti-side-head">
        <span className="ti-side-title">
          <TruckIcon className="ti-title-icon" />
          Fleet <span className="ti-mono">(<span id="count-all">{counts.all}</span>)</span>
        </span>
        <button type="button" className="ti-icon-btn" onClick={() => setCollapsed(true)} aria-label="Hide panel">
          <ChevronLeftIcon className="ti-btn-svg" />
        </button>
      </div>
      <div className="ti-search-wrap">
        <SearchIcon className="ti-search-icon" />
        <input ref={searchRef} id="vehicle-search" className="ti-search" value={query} placeholder="Search vehicle…  ( / )"
          aria-label="Search Vehicles" autoComplete="off" onChange={(e) => setQuery(e.target.value)} />
        {query && (
          <button type="button" className="ti-clear" onClick={() => setQuery('')} aria-label="Clear search">
            <CloseIcon className="ti-btn-svg-xs" />
          </button>
        )}
      </div>
      <div className="ti-sort-row" role="tablist" aria-label="Sort fleet list">
        <span className="ti-sort-label">Sort</span>
        {SORTS.map((s) => (
          <button key={s.key} type="button" role="tab" aria-selected={sort === s.key}
            className={'ti-sort-btn' + (sort === s.key ? ' on' : '')} onClick={() => setSort(s.key)}>{s.label}</button>
        ))}
      </div>
      <div className="ti-tabs" role="tablist" aria-label="Filter fleet by status">
        {TABS.map((t) => (
          <button key={t.key} type="button" role="tab" aria-selected={status === t.key}
            className={'ti-tab' + (status === t.key ? ' on' : '')} onClick={() => setStatus(t.key)}>
            <span className="ti-tab-count" id={`panel-count-${t.key}`}>{counts[t.key]}</span>
            <span className="ti-tab-lbl">{t.label}</span>
          </button>
        ))}
      </div>
      <div id="fleet-list" className="ti-list" ref={listRef} onScroll={(e) => setScrollTop((e.target as HTMLDivElement).scrollTop)}>
        {list.length === 0 ? (
          <div className="ti-empty-state">
            <div className="ti-empty-illu-wrap">
              <TelemetryEmptyIllustration className="ti-empty-illu" />
            </div>
            <div className="ti-empty-title">
              {query || status !== 'all' ? 'No Matching Fleet Units' : 'No Vehicles Reporting Telemetry'}
            </div>
            <div className="ti-empty-msg">
              {query || status !== 'all' 
                ? 'No vehicles match your search or status filter. Reset filters to view all units.' 
                : 'No active AIS-140 GPS, OBD-II or driver mobile telemetry feeds detected for this tenant.'}
            </div>
            {!query && status === 'all' && (
              <div className="ti-empty-btns">
                <a href="/vehicles/new" className="ti-empty-btn-primary">
                  <PlusIcon className="ti-btn-svg" />
                  New Vehicle
                </a>
                <a href="/telemetry/devices" className="ti-empty-btn-link">
                  Pair GPS Tracker <ArrowRightIcon className="ti-btn-svg-inline" />
                </a>
              </div>
            )}
          </div>
        ) : (
          <div style={{ height: list.length * ROW, position: 'relative' }}>
            {visible.map((v, i) => {
              const b = bucketOf(v);
              return (
                <button key={v.vehicle_id} type="button" style={{ top: (start + i) * ROW }}
                  className={'ti-row fleet-row' + (v.vehicle_id === selectedId ? ' sel' : '')}
                  onClick={() => pick(v.vehicle_id)}>
                  <span className="ti-dot" style={{ background: DOT[b] }} />
                  <span className="ti-row-type-badge" title={v.vehicle_type || 'truck'}>
                    <VehicleTypeIcon type={v.vehicle_type} className="ti-row-type-icon" />
                  </span>
                  <span className="ti-row-main">
                    <span className="ti-row-name">
                      {v.vehicle_number || v.vehicle_id}
                      {v.speed > SPEED_LIMIT_KMH && <ZapIcon className="ti-zap-icon" title="Overspeed warning" />}
                    </span>
                    <span className="ti-row-sub ti-mono">{Math.round(v.speed)} km/h · {STATUS_LABEL[v.status] ?? v.status}</span>
                  </span>
                </button>
              );
            })}
          </div>
        )}
      </div>
    </aside>
    </>
  );
}
