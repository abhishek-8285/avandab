import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { googleTileUrl, GOOGLE_INDIA_TILE_URL, INDIA_PAN_BOUNDS, INDIA_CENTER } from '../constants/indiaBorder';

describe('MapViewport Indian compliance tile configuration', () => {
  it('generates standard Google roadmap tiles with gl=IN region for Survey of India compliance', () => {
    const url = googleTileUrl('m', 'IN');
    expect(url).toBe('https://mt1.google.com/vt/lyrs=m&x={x}&y={y}&z={z}&gl=IN');
    expect(GOOGLE_INDIA_TILE_URL).toBe('https://mt1.google.com/vt/lyrs=m&x={x}&y={y}&z={z}&gl=IN');
  });

  it('centers on India and bounds within official Indian territorial coordinates', () => {
    expect(INDIA_CENTER).toEqual([20.5937, 78.9629]);
    expect(INDIA_PAN_BOUNDS).toEqual([
      [4.0, 65.0],
      [38.5, 100.0],
    ]);
  });

  it('defaults to Google tile provider unless explicitly overridden to osm', () => {
    const src = readFileSync(new URL('./MapViewport.tsx', import.meta.url), 'utf8');
    expect(src).toContain("const useGoogle = propsRef.current.provider !== 'osm';");
  });
});
