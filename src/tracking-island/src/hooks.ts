import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { ConnMode, LiveVehicle, SortKey, StatusFilter } from './types';

// Null-safe position check: coordinates of 0 are valid (never truthiness).
export function hasPos(v: LiveVehicle): boolean {
  return v.lat !== null && v.lat !== undefined && v.lng !== null && v.lng !== undefined &&
    Number.isFinite(v.lat) && Number.isFinite(v.lng);
}

export function bucketOf(v: LiveVehicle): 'running' | 'stopped' | 'alert' {
  if (v.status === 'no_signal' || v.status === 'maintenance_due') return 'alert';
  if (v.speed > 0 && v.motion !== false) return 'running';
  return 'stopped';
}

// Bearing in degrees between two coordinates (for marker rotation).
export function bearingDeg(lat1: number, lng1: number, lat2: number, lng2: number): number {
  const toRad = (d: number) => (d * Math.PI) / 180;
  const toDeg = (r: number) => (r * 180) / Math.PI;
  const dLng = toRad(lng2 - lng1);
  const p1 = toRad(lat1);
  const p2 = toRad(lat2);
  const y = Math.sin(dLng) * Math.cos(p2);
  const x = Math.cos(p1) * Math.sin(p2) - Math.sin(p1) * Math.cos(p2) * Math.cos(dLng);
  return (toDeg(Math.atan2(y, x)) + 360) % 360;
}

interface FeedState {
  vehicles: Map<string, LiveVehicle>;
  version: number; // bumped to trigger React render after imperative map updates
  conn: ConnMode;
  sseAttempts: number;
  lastSync: number;
}

// Live telemetry feed: SSE (telemetry events on the stream endpoint) is
// authoritative; REST polling of the live endpoint is the backup while the
// stream is down — same contract as the legacy tracking.html (ingestTelemetry:
// a bare array replaces the fleet, a single object upserts one vehicle).
export function useTelemetryFeed(cfg: { live: string; stream: string; pollSec: number }) {
  const [{ vehicles, version, conn, sseAttempts, lastSync }, setState] = useState<FeedState>({
    vehicles: new Map(), version: 0, conn: 'connecting', sseAttempts: 0, lastSync: 0,
  });
  const mapRef = useRef(vehicles);
  mapRef.current = vehicles;
  const renderQueued = useRef(false);
  const esRef = useRef<EventSource | null>(null);
  const [sseOn, setSseOn] = useState(true);

  // Coalesce high-frequency bursts into one repaint per frame.
  const scheduleRerender = useCallback((mode: ConnMode) => {
    if (renderQueued.current) return;
    renderQueued.current = true;
    requestAnimationFrame(() => {
      renderQueued.current = false;
      setState((s) => ({ ...s, vehicles: new Map(mapRef.current), version: s.version + 1, conn: mode, lastSync: Date.now() }));
    });
  }, []);

  const ingest = useCallback((payload: unknown, mode: ConnMode) => {
    const m = mapRef.current;
    if (Array.isArray(payload)) {
      const seen = new Set<string>();
      for (const v of payload as LiveVehicle[]) {
        if (!v || typeof v.vehicle_id !== 'string') continue;
        seen.add(v.vehicle_id);
        m.set(v.vehicle_id, v);
      }
      for (const id of [...m.keys()]) if (!seen.has(id)) m.delete(id);
    } else if (payload && typeof (payload as LiveVehicle).vehicle_id === 'string') {
      m.set((payload as LiveVehicle).vehicle_id, payload as LiveVehicle);
    } else {
      return;
    }
    scheduleRerender(mode);
  }, [scheduleRerender]);

  // REST poll backup.
  useEffect(() => {
    let timer: ReturnType<typeof setInterval> | null = null;
    let stopped = false;
    const refresh = () => {
      fetch(cfg.live, { headers: { Accept: 'application/json' }, credentials: 'same-origin' })
        .then((r) => { if (!r.ok) throw new Error('HTTP ' + r.status); return r.json(); })
        .then((list) => { if (!stopped) ingest(list, esRef.current ? 'live' : 'poll'); })
        .catch(() => { if (!stopped) setState((s) => ({ ...s, conn: 'offline' })); });
    };
    const startPolling = () => { if (!timer) { refresh(); timer = setInterval(refresh, Math.max(2, cfg.pollSec) * 1000); } };
    const stopPolling = () => { if (timer) { clearInterval(timer); timer = null; } };
    (window as unknown as { __trackingPoll?: { start: () => void; stop: () => void } }).__trackingPoll = {
      start: startPolling, stop: stopPolling,
    };
    startPolling();

    const onVis = () => {
      if (document.hidden) stopPolling();
      else if (!esRef.current) startPolling();
    };
    document.addEventListener('visibilitychange', onVis);
    return () => { stopped = true; stopPolling(); document.removeEventListener('visibilitychange', onVis); };
  }, [cfg.live, cfg.pollSec, ingest]);

  // SSE primary. Exponential backoff: min(1000 * 2^attempt, 30000)ms.
  // Disabled via the stream toggle → polling becomes the transport.
  useEffect(() => {
    if (!sseOn || !window.EventSource) {
      if (esRef.current) { esRef.current.close(); esRef.current = null; }
      (window as unknown as { __trackingPoll?: { start: () => void } }).__trackingPoll?.start();
      return;
    }
    let attempt = 0;
    let retryTimer: ReturnType<typeof setTimeout> | null = null;
    let closed = false;
    const ctl = (window as unknown as { __trackingPoll?: { start: () => void; stop: () => void } }).__trackingPoll;

    const connect = () => {
      if (closed) return;
      setState((s) => ({ ...s, conn: 'connecting', sseAttempts: attempt }));
      const es = new EventSource(cfg.stream);
      esRef.current = es;
      es.onopen = () => { attempt = 0; ctl?.stop(); scheduleRerender('live'); };
      es.addEventListener('telemetry', (e) => {
        try { ingest(JSON.parse((e as MessageEvent).data), 'live'); }
        catch { /* malformed frame — poll backup still covers us */ }
      });
      es.onerror = () => {
        es.close();
        if (esRef.current === es) esRef.current = null;
        ctl?.start();
        attempt += 1;
        setState((s) => ({ ...s, conn: 'poll', sseAttempts: attempt }));
        retryTimer = setTimeout(connect, Math.min(1000 * 2 ** attempt, 30000));
      };
    };
    connect();
    return () => { closed = true; if (retryTimer) clearTimeout(retryTimer); esRef.current?.close(); esRef.current = null; };
  }, [cfg.stream, sseOn, ingest, scheduleRerender]);

  const refreshNow = useCallback(() => {
    fetch(cfg.live, { headers: { Accept: 'application/json' }, credentials: 'same-origin' })
      .then((r) => { if (!r.ok) throw new Error('HTTP ' + r.status); return r.json(); })
      .then((list) => ingest(list, esRef.current ? 'live' : 'poll'))
      .catch(() => setState((s) => ({ ...s, conn: 'offline' })));
  }, [cfg.live, ingest]);

  return { vehicles, version, conn, sseAttempts, lastSync, sseOn, setSseOn, refreshNow };
}

const STATUS_RANK: Record<string, number> = { alert: 0, running: 1, stopped: 2 };

export function useFleetFilter(
  vehicles: Map<string, LiveVehicle>,
  query: string, status: StatusFilter, sort: SortKey,
) {
  return useMemo(() => {
    const q = query.trim().toLowerCase();
    let list = [...vehicles.values()].filter(hasPos);
    if (status !== 'all') list = list.filter((v) => bucketOf(v) === status);
    if (q) {
      list = list.filter((v) =>
        (v.vehicle_number ?? '').toLowerCase().includes(q) ||
        v.vehicle_id.toLowerCase().includes(q) ||
        (v.driver_name ?? '').toLowerCase().includes(q) ||
        (v.trip_id ?? '').toLowerCase().includes(q));
    }
    const byName = (a: LiveVehicle, b: LiveVehicle) =>
      (a.vehicle_number || a.vehicle_id).localeCompare(b.vehicle_number || b.vehicle_id);
    switch (sort) {
      case 'name': list.sort(byName); break;
      case 'speed': list.sort((a, b) => b.speed - a.speed || byName(a, b)); break;
      case 'fresh': list.sort((a, b) => +new Date(b.ts) - +new Date(a.ts)); break;
      default:
        list.sort((a, b) => (STATUS_RANK[bucketOf(a)] ?? 3) - (STATUS_RANK[bucketOf(b)] ?? 3) || byName(a, b));
    }
    return list;
  }, [vehicles, query, status, sort]);
}
