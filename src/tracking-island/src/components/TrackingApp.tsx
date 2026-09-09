import { useEffect, useState } from 'react';
import type { GeofenceZone, LiveVehicle, TrackingMapConfig } from '../types';
import { useTelemetryFeed } from '../hooks';
import MapViewport, { type MapHandle } from './MapViewport';
import FleetSidebar from './FleetSidebar';
import VehicleDetailDrawer from './VehicleDetailDrawer';

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
  const [handle, setHandle] = useState<MapHandle | null>(null);

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
  const beaconColor = conn === 'live' ? '#22c55e' : conn === 'poll' ? '#0284c7' : conn === 'offline' ? '#dc2626' : '#f59e0b';

  return (
    <div className="ti-root">
      <FleetSidebar vehicles={vehicles} selectedId={selectedId} onSelect={(id) => { setSelectedId(id); }} />
      <div id="map-theater" className="ti-main">
        <div className="ti-topbar">
          <span className={'ti-badge ' + conn} aria-live="polite">
            <span id="conn-beacon" className="ti-beacon" style={{ background: beaconColor }} />
            <span id="conn-label">{connLabel}</span>
          </span>
          <span id="live-clock" className="ti-sync ti-mono">
            {lastSync > 0 ? 'Updated ' + new Date(lastSync).toLocaleTimeString() : 'SYNCING…'}
          </span>
          <span className="ti-density ti-mono">
            <b id="density-active">{activeCount}</b>/<span id="density-total">{vehicles.size}</span> live
          </span>
          <button type="button" id="refresh-feed-btn" className="ti-icon-btn" onClick={refreshNow} title="Refresh now">⟳</button>
          <span className="ti-sp" />
          <label className="ti-toggle">
            <input id="sse-toggle" type="checkbox" checked={sseOn} onChange={(e) => setSseOn(e.target.checked)} />
            Stream{sseAttempts > 0 && !sseOn ? '' : sseAttempts > 0 ? ` (retry ${sseAttempts})` : ''}
          </label>
          <label className="ti-toggle">
            <input type="checkbox" checked={showGeofences} onChange={(e) => setShowGeofences(e.target.checked)} />
            Geofences
          </label>
        </div>
        <MapViewport vehicles={vehicles} version={version} selectedId={selectedId} follow={follow}
          geofences={geofences} showGeofences={showGeofences} osmUrl={config.OSMUrl}
          onSelect={(id) => { setSelectedId(id); if (!id) setFollow(false); }} handleRef={setHandle} />
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
  );
}
