export interface LatLng {
  latitude: number;
  longitude: number;
}

// Known hubs — extend as needed. Keys normalized to UPPER without spaces.
const HUBS: Record<string, LatLng> = {
  BURARI: { latitude: 28.761, longitude: 77.193 },
  // Chandni Chowk variants (seed uses CHANDANI)
  'CHANDANICHOWK': { latitude: 28.6506, longitude: 77.2306 },
  'CHANDNICHOWK': { latitude: 28.6506, longitude: 77.2306 },
  'CHANDNICHOWKDELHI': { latitude: 28.6506, longitude: 77.2306 },
  DELHI: { latitude: 28.6139, longitude: 77.209 },
  MUMBAI: { latitude: 19.076, longitude: 72.8777 },
  PUNE: { latitude: 18.5204, longitude: 73.8567 },
  GURUGRAM: { latitude: 28.4595, longitude: 77.0266 },
  GURGAON: { latitude: 28.4595, longitude: 77.0266 },
  NOIDA: { latitude: 28.5355, longitude: 77.391 },
  FARIDABAD: { latitude: 28.4089, longitude: 77.3178 },
  GHAZIABAD: { latitude: 28.6692, longitude: 77.4538 },
};

function normalize(label: string): string {
  return label.toUpperCase().replace(/[^A-Z]/g, '');
}

export function resolvePlaceCoords(label?: string): LatLng | null {
  if (!label) return null;
  const n = normalize(label);
  if (!n) return null;
  if (HUBS[n]) return HUBS[n];
  // substring match for composite like "BURARI, DELHI" or "Chandani Chowk, Delhi"
  for (const [k, v] of Object.entries(HUBS)) {
    if (n.includes(k) || k.includes(n)) return v;
  }
  return null;
}
