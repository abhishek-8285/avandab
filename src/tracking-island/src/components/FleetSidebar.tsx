import { useEffect, useMemo, useRef, useState } from 'react';
import type { LiveVehicle, SortKey, StatusFilter } from '../types';
import { bucketOf, useFleetFilter } from '../hooks';
import {
  ArrowRightIcon,
  ChevronDownIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  ChevronUpIcon,
  CloseIcon,
  PlusIcon,
  SearchIcon,
  TelemetryEmptyIllustration,
  TruckIcon,
  VehicleTypeIcon,
  ZapIcon,
} from './icons';
import { timeAgo } from '../hooks';

interface Props {
  vehicles: Map<string, LiveVehicle>;
  selectedId: string | null;
  onSelect: (id: string) => void;
  onSheetChange?: () => void;
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
  running: 'var(--color-status-success, #059669)',
  stopped: 'var(--color-status-warning, #d97706)',
  alert: 'var(--color-status-alert, #dc2626)',
};

const STATUS_LABEL: Record<string, string> = {
  running: 'Moving', stopped: 'Idle', no_signal: 'No signal', maintenance_due: 'Maintenance due',
};

export const SHEET_BP = 768;
export type SheetState = 'collapsed' | 'half' | 'full';

const SPEED_LIMIT_KMH = 80;

// Collapsible fleet registry: fuzzy search (/ hotkey), status tabs,
// sort control, windowed list (renders visible slice + overscan only).
export default function FleetSidebar({ vehicles, selectedId, onSelect, onSheetChange }: Props) {
  const [query, setQuery] = useState('');
  const [status, setStatus] = useState<StatusFilter>('all');
  const [sort, setSort] = useState<SortKey>('status');
  const [collapsed, setCollapsed] = useState(false);
  // Below SHEET_BP the registry is a bottom sheet (collapsed/half/full);
  // 768–1023 keeps the collapsible side panel; ≥1024 is pinned open.
  const [isMobile] = useState(() =>
    typeof window !== 'undefined' ? window.innerWidth < SHEET_BP : false);
  const [sheet, setSheet] = useState<SheetState>('half');
  const sheetTouched = useRef(false);
  // Below lg the registry starts stowed off-canvas; the rail re-opens it.
  const [mobileOpen, setMobileOpen] = useState(() =>
    typeof window !== 'undefined' ? window.innerWidth >= 1024 : true);
  const [scrollTop, setScrollTop] = useState(0);
  const searchRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);

  const pick = (id: string) => {
    onSelect(id);
    if (typeof window === 'undefined') return;
    // Tablet: stow the side registry so the map theater owns focus.
    if (window.innerWidth < 1024 && window.innerWidth >= SHEET_BP) setMobileOpen(false);
    // Mobile: drop the sheet to collapsed so the centered marker is visible.
    if (window.innerWidth < SHEET_BP) { sheetTouched.current = true; setSheet('collapsed'); }
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

  const isEmpty = counts.all === 0;
  const fleetLabel = `Fleet · ${counts.all} vehicle${counts.all === 1 ? '' : 's'}`;

  // First telemetry after an empty start: drop an untouched sheet to
  // collapsed (map-first default) instead of leaving the empty card up.
  const hadFleet = useRef(false);
  useEffect(() => {
    if (counts.all > 0) {
      if (!hadFleet.current && !sheetTouched.current) setSheet('collapsed');
      hadFleet.current = true;
    } else {
      hadFleet.current = false;
    }
  }, [counts.all]);

  const touchSheet = (s: SheetState) => { sheetTouched.current = true; setSheet(s); };

  // Sheet geometry changed → parent re-measures the map. Skips the first
  // render (no geometry change yet) and desktop (no sheet rendered).
  const sheetCb = useRef(onSheetChange);
  sheetCb.current = onSheetChange;
  const firstSheet = useRef(true);
  useEffect(() => {
    if (firstSheet.current) { firstSheet.current = false; return; }
    sheetCb.current?.();
  }, [sheet]);

  const renderEmpty = () => (
    <div className="ti-empty-state">
      <div className="ti-empty-illu-wrap" aria-hidden="true">
        <TelemetryEmptyIllustration className="ti-empty-illu" />
      </div>
      <h2 className="ti-empty-title">
        {query || status !== 'all' ? 'No Matching Fleet Units' : (
          <>
            <span className="ti-empty-title-long">No Vehicles Reporting Telemetry</span>
            <span className="ti-empty-title-short">No vehicles yet</span>
          </>
        )}
      </h2>
      <div className="ti-empty-msg">
        {query || status !== 'all'
          ? 'No vehicles match your search or status filter. Reset filters to view all units.'
          : (
            <>
              <span className="ti-empty-msg-long">No active AIS-140 GPS, OBD-II or driver mobile telemetry feeds detected for this tenant.</span>
              <span className="ti-empty-msg-short">Add a vehicle or pair a GPS tracker to start tracking.</span>
            </>
          )}
      </div>
      {!query && status === 'all' && (
        <div className="ti-empty-btns">
          <a href="/vehicles/new" className="ti-empty-btn-primary">
            <PlusIcon className="ti-btn-svg" />
            Add Vehicle
          </a>
          <a href="/telemetry/devices" className="ti-empty-btn-link">
            Pair GPS Tracker <ArrowRightIcon className="ti-btn-svg-inline" />
          </a>
        </div>
      )}
    </div>
  );

  const renderRows = () => (
    <div style={{ height: list.length * ROW, position: 'relative' }}>
      {visible.map((v, i) => {
        const b = bucketOf(v);
        const ago = timeAgo(v.ts);
        const sub = `${Math.round(v.speed)} km/h · ${STATUS_LABEL[v.status] ?? v.status}${ago ? ` · ${ago}` : ''}`;
        return (
          <button key={v.vehicle_id} type="button" style={{ top: (start + i) * ROW }}
            className={'ti-row fleet-row' + (v.vehicle_id === selectedId ? ' sel' : '')}
            onClick={() => pick(v.vehicle_id)}>
            <span className="ti-dot" style={{ background: DOT[b] }} aria-hidden="true" />
            <span className="ti-row-type-badge" title={v.vehicle_type || 'truck'} aria-hidden="true">
              <VehicleTypeIcon type={v.vehicle_type} className="ti-row-type-icon" />
            </span>
            <span className="ti-row-main">
              <span className="ti-row-name">
                {v.vehicle_number || v.vehicle_id}
                {v.speed > SPEED_LIMIT_KMH && <ZapIcon className="ti-zap-icon" aria-hidden="true" />}
              </span>
              <span className="ti-row-sub ti-mono">{sub}</span>
            </span>
          </button>
        );
      })}
    </div>
  );

  const renderSearch = () => (
    <div className="ti-search-wrap">
      <SearchIcon className="ti-search-icon" />
      <input ref={searchRef} id="vehicle-search" name="vehicle-search" type="search" spellCheck={false} className="ti-search" value={query} placeholder="Search vehicle…  ( / )"
        aria-label="Search Vehicles" autoComplete="off" onChange={(e) => setQuery(e.target.value)} />
      {query && (
        <button type="button" className="ti-clear" onClick={() => setQuery('')} aria-label="Clear search">
          <CloseIcon className="ti-btn-svg-xs" />
        </button>
      )}
    </div>
  );

  const renderTabs = () => (
    <div className="ti-tabs" role="group" aria-label="Filter fleet by status">
      {TABS.map((t) => (
        <button key={t.key} type="button" aria-pressed={status === t.key}
          className={'ti-tab' + (status === t.key ? ' on' : '')} onClick={() => setStatus(t.key)}>
          <span className="ti-tab-count" id={`panel-count-${t.key}`}>{counts[t.key]}</span>
          <span className="ti-tab-lbl">{t.label}</span>
        </button>
      ))}
    </div>
  );

  // Mobile (<SHEET_BP): bottom sheet over a full-width map. Three states —
  // collapsed (handle + summary, ~72px), half (search + filters + list),
  // full (list owns the screen). No sort row, no rail, no side drawer.
  if (isMobile) {
    const toggleHalf = () => touchSheet(sheet === 'collapsed' ? 'half' : 'collapsed');
    return (
      <section id="fleet-sheet" className={`ti-sheet ${sheet}`} aria-label="Fleet panel">
        <button type="button" className="ti-sheet-handle" onClick={toggleHalf}
          aria-label={sheet === 'collapsed' ? 'Expand fleet panel' : 'Collapse fleet panel'}>
          <span className="ti-grabber" aria-hidden="true" />
        </button>
        <div className="ti-sheet-bar">
          <button type="button" className="ti-sheet-summary" onClick={toggleHalf} aria-expanded={sheet !== 'collapsed'}>
            <TruckIcon className="ti-title-icon" />
            <span className="ti-sheet-title">{fleetLabel}</span>
          </button>
          {/* Zero fleet: half and full show the same short card, so the
              expand chevron is dead weight — bar tap still toggles. */}
          {!isEmpty && (sheet !== 'collapsed' ? (
            <button type="button" className="ti-icon-btn" onClick={() => touchSheet(sheet === 'full' ? 'half' : 'full')}
              aria-label={sheet === 'full' ? 'Exit full screen' : 'Expand to full screen'}>
              {sheet === 'full'
                ? <ChevronDownIcon className="ti-btn-svg" />
                : <ChevronUpIcon className="ti-btn-svg" />}
            </button>
          ) : (
            <button type="button" className="ti-icon-btn ti-sheet-open" onClick={toggleHalf} aria-label="Expand fleet panel">
              <ChevronUpIcon className="ti-btn-svg" />
            </button>
          ))}
        </div>
        {sheet !== 'collapsed' && (
          <div className="ti-sheet-body">
            {!isEmpty && renderSearch()}
            {!isEmpty && renderTabs()}
            <div id="fleet-list" className="ti-list" ref={listRef} role="region" aria-label="Fleet list" tabIndex={0} onScroll={(e) => setScrollTop((e.target as HTMLDivElement).scrollTop)}>
              {list.length === 0 ? renderEmpty() : renderRows()}
            </div>
          </div>
        )}
      </section>
    );
  }

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
        <input ref={searchRef} id="vehicle-search" name="vehicle-search" type="search" spellCheck={false} className="ti-search" value={query} placeholder="Search vehicle…  ( / )"
          aria-label="Search Vehicles" autoComplete="off" onChange={(e) => setQuery(e.target.value)} />
        {query && (
          <button type="button" className="ti-clear" onClick={() => setQuery('')} aria-label="Clear search">
            <CloseIcon className="ti-btn-svg-xs" />
          </button>
        )}
      </div>
      {/* Zero fleet: search/sort/filters are dead weight — straight to onboarding. */}
      {!isEmpty && (
      <div className="ti-sort-row" role="group" aria-label="Sort fleet list">
        <span className="ti-sort-label">Sort</span>
        {SORTS.map((s) => (
          <button key={s.key} type="button" aria-pressed={sort === s.key}
            className={'ti-sort-btn' + (sort === s.key ? ' on' : '')} onClick={() => setSort(s.key)}>{s.label}</button>
        ))}
      </div>
      )}
      {!isEmpty && (
      <div className="ti-tabs" role="group" aria-label="Filter fleet by status">
        {TABS.map((t) => (
          <button key={t.key} type="button" aria-pressed={status === t.key}
            className={'ti-tab' + (status === t.key ? ' on' : '')} onClick={() => setStatus(t.key)}>
            <span className="ti-tab-count" id={`panel-count-${t.key}`}>{counts[t.key]}</span>
            <span className="ti-tab-lbl">{t.label}</span>
          </button>
        ))}
      </div>
      )}
      <div id="fleet-list" className="ti-list" ref={listRef} role="region" aria-label="Fleet list" tabIndex={0} onScroll={(e) => setScrollTop((e.target as HTMLDivElement).scrollTop)}>
        {list.length === 0 ? renderEmpty() : renderRows()}
      </div>
    </aside>
    </>
  );
}
