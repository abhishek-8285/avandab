import { useCallback, useEffect, useRef, useState } from 'react';
import type { GeofenceZone, LiveVehicle, TrackingMapConfig } from '../types';
import { hasPos, useTelemetryFeed } from '../hooks';
import MapViewport, { type MapHandle } from './MapViewport';
import FleetSidebar, { SHEET_BP } from './FleetSidebar';
import VehicleDetailDrawer from './VehicleDetailDrawer';
import { LayersIcon, MaximizeIcon, RefreshIcon } from './icons';

async function loadGeofences(url: string): Promise<GeofenceZone[]> {
  const r = await fetch(url, { headers: { Accept: 'application/json' }, credentials: 'same-origin' });
  if (!r.ok) return [];
  const j = await r.json();
  return Array.isArray(j) ? j : [];
}

export default function TrackingApp({ config }: { config: TrackingMapConfig }) {
  const { vehicles, version, conn, sseAttempts, lastSync, sseOn, setSseOn, refreshNow } = useTelemetryFeed({
    live: config.LiveEndpoint, stream: config.StreamEndpoint, pollSec: config.PollSec,
  });
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [follow, setFollow] = useState(false);
  const [geofences, setGeofences] = useState<GeofenceZone[]>([]);
  const [showGeofences, setShowGeofences] = useState(true);
  // Sheet open/close must re-measure the map (center+zoom preserved by
  // invalidateSize). Stable ref so the sidebar effect fires only on change.
  const [handle, setHandle] = useState<MapHandle | null>(null);
  const handleRef = useRef<MapHandle | null>(null);
  const setHandleRef = useCallback((h: MapHandle | null) => {
    handleRef.current = h;
    setHandle(h);
  }, []);
  const invalidateMap = useCallback(() => { handleRef.current?.invalidate(); }, []);

  useEffect(() => {
    let live = true;
    loadGeofences(config.GeofenceEndpoint).then((z) => { if (live) setGeofences(z); }).catch(() => {});
    return () => { live = false; };
  }, [config.GeofenceEndpoint]);

  // j/k navigate, Enter centers, Escape clears selection.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const tag = (e.target as HTMLElement)?.tagName;
      if (tag === 'INPUT' || tag === 'TEXTAREA') return;
      const ids = [...vehicles.keys()];
      const idx = selectedId ? ids.indexOf(selectedId) : -1;
      if (e.key === 'j' || e.key === 'k') {
        e.preventDefault();
        const next = ids[(idx + (e.key === 'j' ? 1 : -1) + ids.length) % Math.max(1, ids.length)];
        if (next) setSelectedId(next);
      } else if (e.key === 'Enter' && selectedId) {
        const v = vehicles.get(selectedId);
        if (v) handle?.focus(v);
      } else if (e.key === 'Escape') {
        setSelectedId(null);
        setFollow(false);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [vehicles, selectedId, handle]);

  const selected: LiveVehicle | null = (selectedId && vehicles.get(selectedId)) || null;
  let activeCount = 0;
  for (const v of vehicles.values()) if (v.status === 'running' || v.status === 'stopped') activeCount += 1;
  const connLabel =
    conn === 'live' ? 'Live Stream' :
    conn === 'poll' ? 'Live (Polling)' :
    conn === 'connecting' ? 'Connecting…' : 'Offline';
  const beaconColor =
    conn === 'live' ? 'var(--color-status-success, #059669)' :
    conn === 'poll' ? 'var(--color-status-info, #2563eb)' :
    conn === 'offline' ? 'var(--color-status-alert, #dc2626)' :
    'var(--color-status-warning, #d97706)';

  // Tap a vehicle → select + center on phones (the sheet covers the map);
  // desktop keeps select-only, inspector-driven.
  const selectVehicle = (id: string | null) => {
    setSelectedId(id);
    if (id) {
      if (typeof window !== 'undefined' && window.innerWidth < SHEET_BP) {
        const v = vehicles.get(id);
        if (v && hasPos(v)) handle?.focus(v);
      }
    } else {
      setFollow(false);
    }
  };

  const streamTitle = sseOn ? 'Live stream on — tap to pause' : 'Live stream paused — tap to resume';
  const streamBtn = (id: string) => (
    <button type="button" id={id} className={'ti-icon-btn ti-live-btn' + (sseOn ? ' on' : '')}
      aria-pressed={sseOn} onClick={() => setSseOn((v) => !v)} title={streamTitle} aria-label={streamTitle}>
      <span className="ti-live-dot" aria-hidden="true" />
      <span className="ti-live-lbl">Live{sseOn && sseAttempts > 0 ? ` ${sseAttempts}` : ''}</span>
    </button>
  );
  const geofenceBtn = (id: string) => (
    <button type="button" id={id} className={'ti-icon-btn' + (showGeofences ? ' on' : '')}
      aria-pressed={showGeofences} onClick={() => setShowGeofences((v) => !v)}
      title="Toggle geofence overlays" aria-label="Toggle geofence overlays">
      <LayersIcon className="ti-btn-svg" />
    </button>
  );
  const refreshBtn = (id: string) => (
    <button type="button" id={id} className="ti-icon-btn" onClick={refreshNow} title="Refresh now" aria-label="Refresh now">
      <RefreshIcon className="ti-btn-svg" />
    </button>
  );
  const fitBtn = (id: string) => (
    <button type="button" id={id} className="ti-icon-btn" onClick={() => handle?.fitAll()}
      title="Fit all vehicles in view" aria-label="Fit all vehicles in view" disabled={vehicles.size === 0}>
      <MaximizeIcon className="ti-btn-svg" />
    </button>
  );

  return (
    <section className="ti-root" aria-label="Live fleet tracking">
      <FleetSidebar vehicles={vehicles} selectedId={selectedId} onSelect={selectVehicle} onSheetChange={invalidateMap} />
      <div id="map-theater" className="ti-main">
        <div className="ti-topbar">
          <span className={'ti-badge ' + conn} aria-live="polite">
            <span id="conn-beacon" className="ti-beacon" style={{ background: beaconColor }} />
            <span id="conn-label">{connLabel}</span>
          </span>
          <span id="live-clock" className="ti-sync ti-mono">
            {lastSync > 0 ? 'Updated ' + new Intl.DateTimeFormat('en-IN', { hour: '2-digit', minute: '2-digit', second: '2-digit' }).format(new Date(lastSync)) : 'SYNCING…'}
          </span>
          <span className="ti-density ti-mono">
            <b id="density-active">{activeCount}</b>/<span id="density-total">{vehicles.size}</span> live
          </span>
          {refreshBtn('refresh-feed-btn')}
          {fitBtn('fit-fleet-btn')}
          <span className="ti-sp" />
          {streamBtn('sse-toggle')}
          {geofenceBtn('geofence-toggle')}
        </div>
        <MapViewport vehicles={vehicles} version={version} selectedId={selectedId} follow={follow}
          geofences={geofences} showGeofences={showGeofences} osmUrl={config.OSMUrl}
          provider={config.Provider} googleStyle={config.GoogleStyle} gl={config.GL}
          onSelect={selectVehicle} handleRef={setHandleRef} />
        {/* Mobile: topbar keeps badge/clock/density; actions float over the map. */}
        <div className="ti-fab-stack" role="group" aria-label="Map actions">
          {refreshBtn('refresh-feed-btn-m')}
          {fitBtn('fit-fleet-btn-m')}
          {streamBtn('sse-toggle-m')}
          {geofenceBtn('geofence-toggle-m')}
        </div>
        {selected && (
          <VehicleDetailDrawer vehicle={selected} following={follow}
            onClose={() => { setSelectedId(null); setFollow(false); }}
            onFollow={() => {
              const next = !follow;
              setFollow(next);
              if (next) handle?.focus(selected);
            }} />
        )}
      </div>
    </section>
  );
}
