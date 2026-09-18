import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';

// Red-proving test ONLY for MapViewport selection staleness. No production edits.
// MapViewport.tsx:246-247 toggles .ti-selected inside effect keyed only on
// p.version, so selection change without telemetry leaves stale highlight
// (TrackingApp.tsx:66,97-109 changes selection independently).
// Static source only: no browser, no DB, no network.
describe('MapViewport selection effect deps', () => {
  it('re-runs when selected ID changes without telemetry', () => {
    const src = readFileSync(new URL('./MapViewport.tsx', import.meta.url), 'utf8');
    const selIdx = src.indexOf('ti-selected');
    expect(selIdx, 'premise: ti-selected toggle must exist').not.toBe(-1);
    const depsIdx = src.indexOf('}, [', selIdx);
    expect(depsIdx, 'premise: selection effect deps array must exist after ti-selected').not.toBe(-1);
    const depsEnd = src.indexOf(']);', depsIdx);
    const deps = src.slice(depsIdx, depsEnd);
    expect(deps).toContain('selectedId');
  });
});
