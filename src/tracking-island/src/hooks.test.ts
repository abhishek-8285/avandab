import { describe, expect, it, vi } from 'vitest';
import { timeAgo } from './hooks';

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
