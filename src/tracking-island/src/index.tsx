import React from 'react';
import { createRoot } from 'react-dom/client';
import TrackingApp from './components/TrackingApp';
import type { TrackingMapConfig } from './types';
import './styles.css';

function readConfig(): TrackingMapConfig {
  const root = document.getElementById('tracking-root');
  const raw = root?.getAttribute('data-config') ?? '{}';
  let parsed: Partial<TrackingMapConfig> = {};
  try { parsed = JSON.parse(raw); } catch { parsed = {}; }
  return {
    Provider: 'auto',
    OSMUrl: 'https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png',
    PollSec: 10,
    LiveEndpoint: '/api/v1/telemetry/live',
    StreamEndpoint: '/api/v1/telemetry/stream',
    GeofenceEndpoint: '/api/v1/telemetry/geofences',
    ...parsed,
  };
}

const rootEl = document.getElementById('tracking-root');
if (rootEl) {
  createRoot(rootEl).render(
    <React.StrictMode>
      <TrackingApp config={readConfig()} />
    </React.StrictMode>,
  );
}
