import { describe, expect, it, vi } from 'vitest';
import { timeAgo, upsertVehicle } from './hooks';
import type { LiveVehicle } from './types';

describe('timeAgo', () => {
  it('says "just now" for fresh fixes', () => {
    vi.setSystemTime(new Date('2026-09-19T09:38:41+05:30'));
    expect(timeAgo('2026-09-19T09:38:20+05:30')).toBe('just now');
  });

  it('renders minutes / hours / days compactly', () => {
    vi.setSystemTime(new Date('2026-09-19T09:38:41+05:30'));
    expect(timeAgo('2026-09-19T09:36:41+05:30')).toBe('2m ago');
    expect(timeAgo('2026-09-19T07:38:41+05:30')).toBe('2h ago');
    expect(timeAgo('2026-09-16T09:38:41+05:30')).toBe('3d ago');
  });

  it('never fabricates: empty string for missing/garbage timestamps', () => {
    expect(timeAgo(undefined)).toBe('');
    expect(timeAgo('not-a-date')).toBe('');
  });
});

describe('upsertVehicle', () => {
  const pollRecord = {
    vehicle_id: 'veh-1',
    vehicle_number: 'MH-12-AB-1234',
    status: 'running' as const,
    driver_name: 'Ravi Kumar',
    heading: 210,
    lat: 19.076,
    lng: 72.8777,
    speed: 0,
    ts: '2026-09-19T09:00:00+05:30',
  };

  it('merges a position frame instead of replacing the poll record', () => {
    const m = new Map<string, LiveVehicle>();
    m.set('veh-1', pollRecord);

    expect(upsertVehicle(m, {
      vehicle_id: 'veh-1', lat: 19.11, lng: 72.9, speed: 42,
      timestamp: '2026-09-19T09:38:20+05:30',
    })).toBe(true);

    const v = m.get('veh-1')!;
    // position came from the frame
    expect(v.lat).toBe(19.11);
    expect(v.speed).toBe(42);
    // identity/freshness came from the poll — these were wiped before
    expect(v.vehicle_number).toBe('MH-12-AB-1234');
    expect(v.status).toBe('running');
    expect(v.driver_name).toBe('Ravi Kumar');
    expect(v.heading).toBe(210);
  });

  it('stamps ts from the frame timestamp', () => {
    const m = new Map<string, LiveVehicle>();
    m.set('veh-1', pollRecord);

    upsertVehicle(m, { vehicle_id: 'veh-1', lat: 19.11, lng: 72.9, speed: 42, timestamp: '2026-09-19T09:38:20+05:30' });

    expect(m.get('veh-1')!.ts).toBe('2026-09-19T09:38:20+05:30');
    expect(timeAgo(m.get('veh-1')!.ts)).toBe('just now');
  });

  it('accepts a vehicle first seen through a bare frame', () => {
    const m = new Map<string, LiveVehicle>();
    expect(upsertVehicle(m, { vehicle_id: 'veh-new', lat: 18.5, lng: 73.8, speed: 12, timestamp: '2026-09-19T09:38:20+05:30' })).toBe(true);
    expect(m.get('veh-new')?.lat).toBe(18.5);
  });

  it('rejects payloads that name no vehicle', () => {
    const m = new Map<string, LiveVehicle>();
    expect(upsertVehicle(m, null)).toBe(false);
    expect(upsertVehicle(m, { lat: 1 })).toBe(false);
    expect(m.size).toBe(0);
  });
});
