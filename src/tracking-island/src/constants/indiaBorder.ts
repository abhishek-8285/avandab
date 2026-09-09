import * as L from 'leaflet';

/**
 * Official Indian Territorial Coordinates (including J&K, Ladakh, Arunachal Pradesh, Kutch & Kanyakumari).
 * Configured according to Survey of India standards (matching FlyFleet).
 */
export const INDIA_BOUNDS: L.LatLngBoundsLiteral = [
  [6.5, 68.0],   // Southwest India
  [37.5, 97.5],  // Northeast India
];

/**
 * Camera panning bounds: clamps the user's viewport so they cannot pan away to other
 * countries/continents, keeping focus exclusively on India and its territorial waters.
 */
export const INDIA_PAN_BOUNDS: L.LatLngBoundsLiteral = [
  [4.0, 65.0],   // Southwest boundary (covers Lakshadweep, Arabian Sea, Kutch)
  [38.5, 100.0], // Northeast boundary (covers Ladakh, Arunachal Pradesh, Andaman & Nicobar)
];

export const INDIA_CENTER: [number, number] = [20.5937, 78.9629]; // Geographic center of India
export const INDIA_DEFAULT_ZOOM = 5;
export const INDIA_MIN_ZOOM = 4;
export const INDIA_MAX_ZOOM = 22;

/**
 * Google Maps Raster Tile Endpoint with gl=IN (Region: India).
 * Exactly as used in FlyFleet (apps/dashboard-web/src/components/LiveMap.tsx).
 * Serves official Government of India / Survey of India boundaries at zero cost.
 */
export const GOOGLE_INDIA_TILE_URL = 'https://mt1.google.com/vt/lyrs=m&x={x}&y={y}&z={z}&gl=IN';
