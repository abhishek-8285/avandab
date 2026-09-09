import { useEffect, useRef } from 'react';
import * as L from 'leaflet';
import 'leaflet/dist/leaflet.css';
import type { GeofenceZone, LiveVehicle } from '../types';
import { bearingDeg, hasPos } from '../hooks';

const STATUS_COLOR: Record<string, string> = {
  running: '#059669', stopped: '#d97706', no_signal: '#dc2626', maintenance_due: '#7c3aed',
};

function truckIcon(color: string, rotation: number, dim: boolean): L.DivIcon {
  return L.divIcon({
    className: 'ti-marker',
    html: `<div class="ti-truck" style="--ti-c:${color};--ti-r:${rotation}deg;opacity:${dim ? 0.45 : 1}">` +
      `<svg viewBox="0 0 24 24" width="30" height="30" fill="var(--ti-c)" stroke="#fff" stroke-width="1">` +
      `<path d="M1 8h13v8H1zM14 11h4l3 3v2h-7z"/><circle cx="6" cy="18" r="2"/><circle cx="17" cy="18" r="2"/></svg></div>`,
    iconSize: [30, 30],
    iconAnchor: [15, 15],
  });
}

interface Anim { fromLat: number; fromLng: number; toLat: number; toLng: number; start: number; marker: L.Marker; last: LiveVehicle }

export interface MapHandle {
  focus: (v: LiveVehicle) => void;
  fitAll: () => void;
}

interface Props {
  vehicles: Map<string, LiveVehicle>;
  version: number;
  selectedId: string | null;
  follow: boolean;
  geofences: GeofenceZone[];
  showGeofences: boolean;
  osmUrl: string;
  onSelect: (id: string | null) => void;
  handleRef: (h: MapHandle | null) => void;
}

const INTERP_MS = 800;

// Leaflet viewport managed imperatively: React renders never touch markers,
// so SSE bursts can't reset popups or pan/zoom state. Positions interpolate
// over 800ms via rAF toward the latest fix (bearing rotates the truck SVG).
export default function MapViewport(p: Props) {
  const divRef = useRef<HTMLDivElement>(null);
  const mapRef = useRef<L.Map | null>(null);
  const markersRef = useRef(new Map<string, L.Marker>());
  const animsRef = useRef(new Map<string, Anim>());
  const geoLayerRef = useRef<L.LayerGroup | null>(null);
  const propsRef = useRef(p);
  propsRef.current = p;

  useEffect(() => {
    const map = L.map(divRef.current!, { zoomControl: true, attributionControl: true })
      .setView([22.5, 78.9], 5);
    L.tileLayer(propsRef.current.osmUrl, {
      maxZoom: 19,
      attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors',
    }).addTo(map);
    geoLayerRef.current = L.layerGroup().addTo(map);
    mapRef.current = map;

    let raf = 0;
    const tick = (now: number) => {
      const anims = animsRef.current;
      for (const [id, a] of anims) {
        const t = Math.min(1, (now - a.start) / INTERP_MS);
        const e = t * t * (3 - 2 * t); // smoothstep
        a.marker.setLatLng([a.fromLat + (a.toLat - a.fromLat) * e, a.fromLng + (a.toLng - a.fromLng) * e]);
        if (t >= 1) anims.delete(id);
      }
      if (anims.size > 0) raf = requestAnimationFrame(tick);
      else raf = 0;
    };
    const kick = (now: number) => { if (!raf) raf = requestAnimationFrame(tick); else tick(now); };
    (map as unknown as { __kick?: (n: number) => void }).__kick = kick;

    propsRef.current.handleRef({
      focus: (v) => map.setView([v.lat, v.lng], Math.max(map.getZoom(), 14), { animate: true }),
      fitAll: () => {
        const pts = [...markersRef.current.values()].map((m) => m.getLatLng());
        if (pts.length > 0) map.fitBounds(L.latLngBounds(pts).pad(0.15));
      },
    });
    map.on('click', () => propsRef.current.onSelect(null));
    return () => {
      propsRef.current.handleRef(null);
      map.remove();
      mapRef.current = null;
      if (raf) cancelAnimationFrame(raf);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Sync markers on each fleet version bump.
  useEffect(() => {
    const map = mapRef.current;
    if (!map) return;
    const markers = markersRef.current;
    const anims = animsRef.current;
    const seen = new Set<string>();
    const now = performance.now();
    const { selectedId, follow, onSelect } = propsRef.current;

    for (const v of p.vehicles.values()) {
      if (!hasPos(v)) continue;
      seen.add(v.vehicle_id);
      let mk = markers.get(v.vehicle_id);
      const stale = v.status === 'no_signal';
      if (!mk) {
        mk = L.marker([v.lat, v.lng], {
          icon: truckIcon(STATUS_COLOR[v.status] ?? '#64748b', v.heading ?? 0, stale),
          title: v.vehicle_number || v.vehicle_id,
        }).addTo(map);
        mk.on('click', (e) => { L.DomEvent.stopPropagation(e); propsRef.current.onSelect(v.vehicle_id); });
        markers.set(v.vehicle_id, mk);
      } else {
        const cur = mk.getLatLng();
        if (cur.lat !== v.lat || cur.lng !== v.lng) {
          const rot = bearingDeg(cur.lat, cur.lng, v.lat, v.lng);
          mk.setIcon(truckIcon(STATUS_COLOR[v.status] ?? '#64748b', v.heading ?? rot, stale));
          anims.set(v.vehicle_id, { fromLat: cur.lat, fromLng: cur.lng, toLat: v.lat, toLng: v.lng, start: now, marker: mk, last: v });
        } else {
          mk.setIcon(truckIcon(STATUS_COLOR[v.status] ?? '#64748b', v.heading ?? 0, stale));
        }
      }
      const el = mk.getElement();
      if (el) el.classList.toggle('ti-selected', v.vehicle_id === selectedId);
    }
    for (const [id, mk] of markers) {
      if (!seen.has(id)) { map.removeLayer(mk); markers.delete(id); anims.delete(id); }
    }
    (map as unknown as { __kick?: (n: number) => void }).__kick?.(now);

    if (follow && selectedId) {
      const sel = p.vehicles.get(selectedId);
      if (sel && hasPos(sel)) map.panTo([sel.lat, sel.lng], { animate: true });
    }
    void onSelect;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [p.version]);

  // Geofence overlay.
  useEffect(() => {
    const layer = geoLayerRef.current;
    const map = mapRef.current;
    if (!layer || !map) return;
    layer.clearLayers();
    if (!p.showGeofences) return;
    for (const z of p.geofences) {
      const style = { color: '#2563eb', weight: 2, fillOpacity: 0.08 };
      if (z.shape === 'circle') {
        L.circle([z.center_lat, z.center_lng], { radius: z.radius_m, ...style }).bindTooltip(z.name).addTo(layer);
      } else if (z.polygon && z.polygon.length >= 3) {
        L.polygon(z.polygon.map((pt) => [pt.lat, pt.lng] as [number, number]), style).bindTooltip(z.name).addTo(layer);
      }
    }
  }, [p.geofences, p.showGeofences]);

  return <div ref={divRef} id="live-map" className="ti-map" role="application" aria-label="Live fleet map" />;
}
