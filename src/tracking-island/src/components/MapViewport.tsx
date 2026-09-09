import { useEffect, useRef } from 'react';
import * as L from 'leaflet';
import 'leaflet/dist/leaflet.css';
import type { GeofenceZone, LiveVehicle } from '../types';
import { bearingDeg, hasPos } from '../hooks';

const STATUS_COLOR: Record<string, string> = {
  running: '#059669', stopped: '#d97706', no_signal: '#dc2626', maintenance_due: '#7c3aed',
};

function getVehicleIconPath(type?: string): string {
  switch (type?.toLowerCase()) {
    case 'mini_truck':
      return `<path d="M15 17h5a1 1 0 0 0 1-1v-4.5a1 1 0 0 0-.3-.7L18.2 8.3A1 1 0 0 0 17.5 8H14v9"/>` +
        `<path d="M14 9.5h3.2l1.3 2.5H14V9.5z"/>` +
        `<path d="M2 11h12v6H2z"/>` +
        `<circle cx="6" cy="18" r="2" fill="#ffffff"/>` +
        `<circle cx="16.5" cy="18" r="2" fill="#ffffff"/>`;
    case 'bus':
      return `<rect x="2" y="5" width="20" height="12" rx="2"/>` +
        `<line x1="2" y1="9" x2="22" y2="9"/>` +
        `<line x1="7" y1="5" x2="7" y2="9"/>` +
        `<line x1="12" y1="5" x2="12" y2="9"/>` +
        `<line x1="17" y1="5" x2="17" y2="9"/>` +
        `<circle cx="6.5" cy="18" r="2" fill="#ffffff"/>` +
        `<circle cx="17.5" cy="18" r="2" fill="#ffffff"/>`;
    case 'van':
      return `<path d="M2 17h3m4 0h6m4 0h2a1 1 0 0 0 1-1v-4.5a1 1 0 0 0-.25-.66l-2.5-3A1 1 0 0 0 17 7H3a1 1 0 0 0-1 1v8a1 1 0 0 0 1 1"/>` +
        `<path d="M14 7v10"/>` +
        `<path d="M14 8.5h2.8l2.2 3.5H14V8.5z"/>` +
        `<circle cx="7" cy="18" r="2" fill="#ffffff"/>` +
        `<circle cx="17" cy="18" r="2" fill="#ffffff"/>`;
    case 'pickup':
      return `<path d="M2 17h3m4 0h6m4 0h3a1 1 0 0 0 1-1v-3.5a1 1 0 0 0-.3-.7L20.2 9.3A1 1 0 0 0 19.5 9H13v8"/>` +
        `<path d="M13 10.5h6l1.3 2.5H13V10.5z"/>` +
        `<path d="M2 12h11v5H2z"/>` +
        `<circle cx="7" cy="18" r="2" fill="#ffffff"/>` +
        `<circle cx="17" cy="18" r="2" fill="#ffffff"/>`;
    case 'tempo':
      return `<path d="M12 17h3m4 0h2a1 1 0 0 0 1-1v-3l-2.5-4.5A1 1 0 0 0 15.6 8H12v9"/>` +
        `<path d="M12 9.5h3.2l1.6 3H12V9.5z"/>` +
        `<path d="M3 11h9v6H3z"/>` +
        `<circle cx="6.5" cy="18" r="2" fill="#ffffff"/>` +
        `<circle cx="18" cy="18" r="2" fill="#ffffff"/>`;
    case 'truck':
    default:
      return `<path d="M1 17h2"/><path d="M7 17h7"/><path d="M18 17h3a1 1 0 0 0 1-1v-4a1 1 0 0 0-.25-.66l-2.5-3A1 1 0 0 0 16.5 8H14v9"/>` +
        `<path d="M14 9h2.5l2 3H14V9z"/>` +
        `<rect x="1" y="6" width="12" height="11" rx="1"/>` +
        `<circle cx="5" cy="18" r="2" fill="#ffffff"/>` +
        `<circle cx="16.5" cy="18" r="2" fill="#ffffff"/>`;
  }
}

function truckIcon(color: string, rotation: number, dim: boolean, vehicleType?: string): L.DivIcon {
  const innerPaths = getVehicleIconPath(vehicleType);
  return L.divIcon({
    className: 'ti-marker',
    html: `<div class="ti-truck" style="--ti-c:${color};--ti-r:${rotation}deg;opacity:${dim ? 0.45 : 1}">` +
      `<svg viewBox="0 0 32 32" width="32" height="32" class="ti-truck-svg">` +
      `<polygon points="16,1 21,7 11,7" fill="${color}" stroke="#ffffff" stroke-width="1.5" stroke-linejoin="round"/>` +
      `<circle cx="16" cy="17" r="12" fill="${color}" stroke="#ffffff" stroke-width="2"/>` +
      `<g transform="translate(8, 9) scale(0.667)" stroke="#ffffff" stroke-width="2.2" fill="none" stroke-linecap="round" stroke-linejoin="round">` +
      innerPaths +
      `</g></svg></div>`,
    iconSize: [32, 32],
    iconAnchor: [16, 17],
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
          icon: truckIcon(STATUS_COLOR[v.status] ?? '#64748b', v.heading ?? 0, stale, v.vehicle_type),
          title: v.vehicle_number || v.vehicle_id,
        }).addTo(map);
        mk.on('click', (e) => { L.DomEvent.stopPropagation(e); propsRef.current.onSelect(v.vehicle_id); });
        markers.set(v.vehicle_id, mk);
      } else {
        const cur = mk.getLatLng();
        if (cur.lat !== v.lat || cur.lng !== v.lng) {
          const rot = bearingDeg(cur.lat, cur.lng, v.lat, v.lng);
          mk.setIcon(truckIcon(STATUS_COLOR[v.status] ?? '#64748b', v.heading ?? rot, stale, v.vehicle_type));
          anims.set(v.vehicle_id, { fromLat: cur.lat, fromLng: cur.lng, toLat: v.lat, toLng: v.lng, start: now, marker: mk, last: v });
        } else {
          mk.setIcon(truckIcon(STATUS_COLOR[v.status] ?? '#64748b', v.heading ?? 0, stale, v.vehicle_type));
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
