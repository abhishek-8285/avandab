import { expect, type Page } from '@playwright/test';

/**
 * Shared responsive assertions for the `tablet` / `mobile` Playwright projects.
 *
 * These are deliberately diagnostic: a bare "scrollWidth > clientWidth" tells
 * you *that* the page overflows but not *what* overflows, and with 163 Go
 * templates finding the culprit by hand is slow. So every failure message
 * names the offending selector.
 */

/** Report the elements that stick out past the viewport's right edge. */
async function findOverflowingElements(page: Page): Promise<string[]> {
  return page.evaluate(() => {
    const vw = document.documentElement.clientWidth;
    const out: string[] = [];
    const describe = (el: Element): string => {
      const id = el.id ? `#${el.id}` : '';
      const cls = (el.getAttribute('class') || '')
        .split(/\s+/)
        .filter(Boolean)
        .slice(0, 4)
        .join('.');
      return `${el.tagName.toLowerCase()}${id}${cls ? '.' + cls : ''}`;
    };

    for (const el of Array.from(document.querySelectorAll('body *'))) {
      const style = getComputedStyle(el);
      if (style.display === 'none' || style.visibility === 'hidden') continue;
      // Elements inside a legitimately scrollable container are not page
      // overflow — that container is the intended escape hatch (see P1-6).
      let scrollableAncestor = false;
      for (let p = el.parentElement; p; p = p.parentElement) {
        const ps = getComputedStyle(p);
        if (ps.overflowX === 'auto' || ps.overflowX === 'scroll' || ps.overflowX === 'hidden') {
          scrollableAncestor = true;
          break;
        }
      }
      if (scrollableAncestor) continue;

      const r = el.getBoundingClientRect();
      if (r.width === 0 && r.height === 0) continue;
      if (r.right > vw + 1 || r.left < -1) {
        out.push(`${describe(el)} [left=${Math.round(r.left)} right=${Math.round(r.right)} vw=${vw}]`);
      }
    }
    return out.slice(0, 12);
  });
}

/**
 * Assert the document does not scroll horizontally.
 *
 * +1px tolerance: sub-pixel rounding on `100vw` layouts and fractional device
 * pixel ratios legitimately produce a 0.x px difference.
 */
export async function expectNoHorizontalOverflow(page: Page, label: string): Promise<void> {
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  );
  if (overflow > 1) {
    const culprits = await findOverflowingElements(page);
    throw new Error(
      `${label}: horizontal overflow of ${overflow}px at ${page.viewportSize()?.width}px wide.\n` +
        `Offending elements:\n  ${culprits.join('\n  ') || '(none identified — check a scrollable ancestor or a wide table)'}`,
    );
  }
  expect(overflow, `${label}: no horizontal page overflow`).toBeLessThanOrEqual(1);
}

/**
 * Assert every visible interactive control meets the 44x44 CSS-px minimum.
 *
 * WCAG 2.5.5 (AAA) / 2.5.8 (AA) target size. This is an ops tool used on
 * phones in cabs and warehouses — a 24px tap target in a moving vehicle is a
 * real usability failure, not a nicety.
 *
 * Only checks controls that are actually rendered and visible; hidden
 * off-canvas panels (a stowed mobile drawer at x=-400) are skipped because
 * they are not tappable in that state.
 */
export async function expectTapTargets(page: Page, label: string): Promise<void> {
  // 44px is WCAG 2.5.5 (AAA) and exists for thumbs — the bar that matters on a
  // 390px phone. Above 768px the layout is a mouse-driven ops console where
  // 2.5.8 (AA) 24px is the applicable bar; a 44px floor there just inflates a
  // deliberately dense UI. See the matching @media block in app.css.
  const min = (page.viewportSize()?.width ?? 1440) < 768 ? 44 : 24;

  const small = await page.evaluate((MIN) => {
    /** A link with no padding, background or border renders as plain text. */
    const isPlainTextLink = (s: CSSStyleDeclaration): boolean => {
      const pad =
        parseFloat(s.paddingTop) + parseFloat(s.paddingBottom) +
        parseFloat(s.paddingLeft) + parseFloat(s.paddingRight);
      const bg = s.backgroundColor;
      const transparent = bg === 'transparent' || bg === 'rgba(0, 0, 0, 0)';
      const border =
        parseFloat(s.borderTopWidth) + parseFloat(s.borderBottomWidth) +
        parseFloat(s.borderLeftWidth) + parseFloat(s.borderRightWidth);
      return pad === 0 && transparent && border === 0;
    };

    const out: string[] = [];
    const describe = (el: Element): string => {
      const id = el.id ? `#${el.id}` : '';
      const cls = (el.getAttribute('class') || '')
        .split(/\s+/)
        .filter(Boolean)
        .slice(0, 3)
        .join('.');
      const text = (el.textContent || '').trim().slice(0, 20);
      return `${el.tagName.toLowerCase()}${id}${cls ? '.' + cls : ''}${text ? ` "${text}"` : ''}`;
    };

    const nodes = Array.from(
      document.querySelectorAll('a[href], button, input[type="checkbox"], input[type="radio"], select, [role="button"]'),
    );
    for (const el of nodes) {
      const style = getComputedStyle(el);
      if (style.display === 'none' || style.visibility === 'hidden' || style.opacity === '0') continue;

      // WCAG 2.5.8 exempts "inline" targets: a link whose size is bound by the
      // surrounding line-height. Detect that by the ABSENCE of box styling
      // rather than by display, because a flex parent blockifies its children
      // (display:inline -> block) even though the link is still a plain text
      // link in the flow. A link with padding / background / border is a
      // control and is checked like any other.
      if (el.tagName === 'A' && isPlainTextLink(style)) continue;

      const r = el.getBoundingClientRect();
      if (r.width === 0 || r.height === 0) continue;
      // Fully off-screen: a stowed drawer, not a tap target.
      if (r.right <= 0 || r.left >= window.innerWidth) continue;
      // A native checkbox/radio is sized by its label's hit area, not its own
      // box — measuring the 20px input would flag a control that is perfectly
      // tappable. Skip when it has (or sits inside) a label.
      if (el instanceof HTMLInputElement && (el.type === 'checkbox' || el.type === 'radio')) {
        if (el.closest('label') || (el.id && document.querySelector(`label[for="${el.id}"]`))) continue;
      }

      if (r.height < MIN - 0.5 || r.width < MIN - 0.5) {
        out.push(`${describe(el)} [${Math.round(r.width)}x${Math.round(r.height)}]`);
      }
    }
    return out;
  }, min);

  expect(
    small,
    `${label}: all visible interactive controls are >=${min}x${min}px (found ${small.length} too small)`,
  ).toEqual([]);
}

/**
 * Assert a `.rtable` has actually reflowed into stacked cards below md.
 *
 * Without this the card CSS could silently stop applying (a specificity clash,
 * a missing `rtable` class) and the overflow assertion would still pass —
 * because a horizontally scrolling table is *contained* by its
 * `overflow-x-auto` wrapper and therefore never overflows the page.
 */
export async function expectTableReflowsToCards(page: Page, label: string): Promise<void> {
  const state = await page.evaluate(() => {
    const table = document.querySelector('table.rtable');
    if (!table) return { found: false as const };
    const thead = table.querySelector('thead');
    const firstTd = table.querySelector('tbody tr td');
    return {
      found: true as const,
      theadDisplay: thead ? getComputedStyle(thead).display : 'no-thead',
      tdDisplay: firstTd ? getComputedStyle(firstTd).display : 'no-td',
      firstLabel: firstTd?.getAttribute('data-label') ?? null,
    };
  });

  expect(state.found, `${label}: a table.rtable is present`).toBe(true);
  if (!state.found) return;

  const compact = (page.viewportSize()?.width ?? 1440) < 768;
  if (compact) {
    expect(state.theadDisplay, `${label}: column headers hidden in card mode`).toBe('none');
    expect(state.tdDisplay, `${label}: cells stack vertically in card mode`).not.toBe('table-cell');
    expect(state.firstLabel, `${label}: first cell carries a data-label caption`).toBeTruthy();
  } else {
    expect(state.theadDisplay, `${label}: column headers shown at md+`).not.toBe('none');
    expect(state.tdDisplay, `${label}: cells stay tabular at md+`).toBe('table-cell');
  }
}

/** Tailwind `lg` breakpoint — above this the layout is full desktop. */
export const LG_BREAKPOINT = 1024;

/** True when the current project viewport is below Tailwind's `lg`. */
export function isBelowLg(page: Page): boolean {
  return (page.viewportSize()?.width ?? 1440) < LG_BREAKPOINT;
}
