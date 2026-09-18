import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';

// Red-proving test ONLY for tracking-island theme-contract defect. No production edits.
// internal/static/js/theme.js:33-39 drives dark mode via the `.dark` class on
// documentElement, but src/tracking-island/src/styles.css:236-259 drives island
// colors via OS `prefers-color-scheme` + `:root[data-theme]` selectors, so the
// island never follows the app theme. Concrete failure: `.ti-sidebar`
// (styles.css:11-12) is hardcoded `background: #fff` while `.ti-empty-title`
// uses `var(--color-on-surface)` which flips to near-white #eef2f8 in dark
// (src/input.css :root.dark), i.e. white-on-white in dark mode.
// Static source only: no browser, no DB, no network.
describe('tracking island theme contract', () => {
  const css = readFileSync(new URL('./styles.css', import.meta.url), 'utf8');
  const themeJs = readFileSync(
    new URL('../../../internal/static/js/theme.js', import.meta.url),
    'utf8',
  );
  const tokens = readFileSync(new URL('../../input.css', import.meta.url), 'utf8');

  it('does not drive island colors via [data-theme] or OS media query', () => {
    expect(
      css.includes('[data-theme'),
      'island must not use [data-theme] selectors (app theme.js never sets data-theme on documentElement)',
    ).toBe(false);
    expect(
      css.includes('prefers-color-scheme'),
      'island must not use prefers-color-scheme (app theme.js resolves system -> .dark class)',
    ).toBe(false);
  });

  it('follows the app .dark-class contract from theme.js', () => {
    expect(
      themeJs.includes("classList.add('dark')") || themeJs.includes('classList.add("dark")'),
      'premise: theme.js must drive dark mode via .dark class',
    ).toBe(true);
    expect(css.includes('.dark'), 'island styles must respond to the .dark class').toBe(true);
  });

  it('sidebar surface and empty-state title stay legible in both themes', () => {
    // Premise: the title token flips near-white in dark ...
    const darkBlock = tokens.slice(tokens.indexOf(':root.dark'));
    expect(darkBlock.includes('--color-on-surface'), 'premise: :root.dark must define --color-on-surface').toBe(true);
    const titleIdx = css.indexOf('.ti-empty-title');
    expect(titleIdx, 'premise: .ti-empty-title rule must exist').not.toBe(-1);
    expect(
      css.slice(titleIdx, titleIdx + 400).includes('var(--color-on-surface'),
      'premise: empty-state title must use var(--color-on-surface)',
    ).toBe(true);
    // ... so the sidebar surface must be a dark-aware token, not hardcoded white.
    const sideIdx = css.indexOf('.ti-sidebar');
    expect(sideIdx, 'premise: .ti-sidebar rule must exist').not.toBe(-1);
    const sideBlock = css.slice(sideIdx, css.indexOf('}', sideIdx));
    expect(
      sideBlock.includes('var(--'),
      'defect: .ti-sidebar background is hardcoded #fff while title flips near-white in dark — use a semantic token',
    ).toBe(true);
  });
});
