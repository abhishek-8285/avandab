# Frontend & UX Audit — Avandab (Web Console + Driver App)

**Date:** 2026-09-13
**Scope:** `internal/templates/` (136 page templates + 27 partials), `internal/static/css|js`, `src/tracking-island/`, `mobile/`
**Requirement audited against:** responsive design + good interaction experience
**Method:** static audit of source + compiled artifacts. Every claim below is reproducible; commands are in §7. No file was modified.

---

## 0. Verdict

The frontend is **not** a greenfield mess. The design-token layer in `src/input.css` is genuinely good — semantic color tokens, a `.dark` override block, a documented contrast fix, fluid display type, tabular figures. Someone cared.

But four **structural** problems mean the responsive/interaction requirement is currently **unverifiable and actively regressing**:

| # | Problem | Why it matters |
|---|---------|----------------|
| 1 | The compiled CSS bundle is **stale** and nothing detects it | Utilities vanish from production silently |
| 2 | **No browser-level test runs in CI** | "Responsive" has zero regression safety net |
| 3 | Token discipline **breaks down in templates** | Broken classes → invisible UI; dark mode has holes |
| 4 | **Three frontend stacks**, divergent design languages, no shared contract | Web blue vs app green; a11y near-absent on mobile |

Findings: **3 × P0, 6 × P1, 6 × P2.**

---

## 1. P0 — Blocking

### P0-1. The compiled Tailwind bundle is stale. Verified, not inferred.

`internal/static/css/tailwind.css` is built by `make build-css` (Tailwind v4, purging by scanning `internal/templates`). Nothing forces that step to run.

**Proof — a plain, valid utility is missing purely because the CSS was never rebuilt:**

```
$ grep -n 'gap-x-4' internal/templates/home.html
491:  gap-x-4                                  <- used by the template

$ grep -oE '\.gap-x-[0-9]+' internal/static/css/tailwind.css | sort -u
.gap-x-2  .gap-x-3  .gap-x-5  .gap-x-6  .gap-x-8
                                            <- 4 absent
```

Arbitrary-value utilities *do* exist in the bundle (`.min-h-\[44px\]`, `.text-\[10px\]`), so the pipeline works — it is purely drift.

Full audit (`scripts/css-sync-audit.py`) — 1,513 distinct class tokens referenced, **39 defined nowhere**:

- **35 valid utilities/custom classes not in the bundle** — incl. `gap-x-4`, `time-display`, `trip-checkbox`, `board-card`, `step-tab`, `tier-card`, `wizard-step`, `profile-initial`, `flash-toast`, `htmx-indicator`, `js-tenant-toggle`, `multistop-timeline-container`
- **4 genuinely invalid** — `py-0.2`, `text-decoration-none`, `bg-text-text-muted` (see P0-3)

**Root cause:** `Makefile` → `build: build-tracking` — it builds the React island but **not** the CSS. `.github/workflows/ci.yml` (7 jobs) never runs `build-css`.

**Impact:** unbounded and silent. Every new template utility is a coin flip. Worst case is not a crash — it is a control that renders unstyled in production only.

**Fix:**
1. `build: build-css build-tracking`
2. New CI job: run `make build-css` then `git diff --exit-code internal/static/css/tailwind.css` → fails the moment a template drifts from the bundle.

---

### P0-2. No browser-level test gate. The responsive requirement has no verification.

`playwright.config.ts` defines exactly **one project**:

```ts
projects: [{ name: 'Chromium',
  use: { ...devices['Desktop Chrome'], viewport: { width: 1280, height: 720 } } }]
```

Seven spec files exist (`tracking*.spec.ts`, `dashboard_ab`, `dashboard_live_update`, `htmx_datastar`, `router_reactivity`). `npm run test:ui` is wired. But:

```
$ grep -rn "playwright\|vitest\|build-css" .github/workflows/
NONE FOUND
```

**No CI job runs Playwright at all.** The only responsive spec (`test/tracking.responsive.spec.ts`, viewports 390×844 / 820×1180) covers **one page** — the tracking island — and never executes in CI.

**Impact:** of 136 page templates and 118 forms, responsive behaviour is asserted for 1 page and verified for 0 in the pipeline. Any layout regression ships.

**Fix:** add `projects` for mobile (390×844), tablet (820×1180), desktop (1440×900); add a CI job running `npx playwright test`. Note the existing `serial` mode workaround for the SQLite deadlock under parallel registration (`test/tracking.responsive.spec.ts:5`) — fix the fixture, not the parallelism.

---

### P0-3. Broken utility classes render real UI elements invisible

These are not cosmetic nits. Each is a class referenced in a template that is **neither a valid Tailwind utility nor defined in any CSS** (`app.css`, `tailwind.css`, or any inline `<style>`):

| Location | Class | Rendered result |
|---|---|---|
| `internal/templates/partials/status_dot.html:2` | `bg-text-text-muted` | **Dot is transparent** — `w-2 h-2 rounded-full` with no background. Used for every `neutral` tone (4 call sites) *and* the partial's default branch |
| `internal/templates/partials/alert_inbox.html:29` | `bg-warning-container` `text-on-warning-container` | **Warning-severity alert row loses its background and text color** — severity is indistinguishable |
| `internal/templates/feature.html:1208` | `bg-border-border-subtle` | **Vertical timeline connector line invisible** (`h-full w-px` + no background) |
| `internal/templates/layout.html:363` | `placeholder-text-text-muted` | Global search placeholder not muted (typo — should be `placeholder:text-text-muted`) |
| `internal/templates/geofence_edit.html:97` | `hover:border-border-default` | Hover border dead — token `border-default` does not exist (only `border-subtle`) |
| `internal/templates/epod_receipt.html:260` | `py-0.2` | Invalid spacing scale → padding absent on ePOD receipt chip |

Two of these are the *typo class* pattern — `bg-` + a `text-`/`border-` token name (`bg-text-text-muted`, `bg-border-border-subtle`). That pattern is a strong signal of hand-written class strings with no validation.

**Fix:** correct the six sites; then add a lint that fails on any class attribute token that resolves to no rule in the built bundle. The script in §7 is a working prototype.

---

## 2. P1 — High

### P1-1. Token system exists, then gets bypassed — dark mode has holes

`src/input.css` defines semantic tokens *and* dark overrides (`:root.dark`, lines 153–198). Templates mostly obey. But:

| Metric | Count |
|---|---|
| Semantic token classes (`bg-surface-container-low`, `text-text-muted`, …) | **8,936** |
| Raw palette classes (`bg-emerald-500`, `text-amber-700`, …) | **1,989** |
| `dark:` variants | **229** |
| Templates using `dark:` at all | **45 / 163** |

Top offenders: `bg-emerald-500` ×192, `bg-amber-500` ×141, `border-emerald-500` ×103, `bg-rose-500` ×95.

Raw palette classes **do not respond to `.dark`**. A `text-emerald-700` on a dark surface is a contrast failure, and 229 `dark:` patches cannot cover 1,989 usages — which is exactly why only 45 templates have any dark handling. The tokens already solve this; the templates just don't use them.

**Fix:** introduce `status-success/warning/alert/info` + `-container` variants for the raw-status palette; migrate the top 5 raw classes; add a lint rule capping raw-palette usage per file (ratchet, matching the project's existing `LINT_BASE` philosophy).

### P1-2. Form accessibility: 171 of 235 labels are not associated with their input

```
inputs/selects/textareas : 299
<label> elements         : 235
labels with for="…"      : 64
```

`booking_edit.html:21` is the canonical example:

```html
<label class="block text-eyebrow text-text-muted mb-1.5">Customer *</label>
<select name="customer_id" required class="…">
```

Sibling, not wrapper, no `for`/`id` pair. A screen reader announces the select with no name. Same pattern across the form-heavy templates.

Also thin: 56 `aria-label` total, 4 `aria-live` (all in `layout.html`).

**Fix:** add `id`/`for` pairs (mechanical, scriptable); `aria-describedby` for the `*` required hint; `aria-live` on the async regions that HTMX swaps.

### P1-3. Micro-typography below the readable minimum

| Size | Instances |
|---|---|
| `text-[9px]` | 13 |
| `text-[10px]` | 254 |
| `text-[11px]` | 383 |
| `text-xs` (12px) | 1,172 |

**650 instances of 9–11px text.** On a 390px-wide phone this is below any reasonable legibility floor — and this is an ops tool used in cabs and warehouses, not a desktop-only console. The layout's own `Add-on` chips (`layout.html:84`) use `text-[9px]`.

**Fix:** raise the floor to 11px for metadata, 12px for anything actionable; collapse `9px`/`10px` into one token.

### P1-4. Touch targets

238 `<button>` elements, only **46** carry an explicit ≥44px target (`min-h-[44px]`, `h-11`, …). `booking_edit.html:92-93` does it correctly (`min-h-[44px]` on the action buttons) — the pattern is known and applied inconsistently.

**Fix:** make the ≥44px minimum part of the `btn.html` partial so it cannot be forgotten.

### P1-5. Loading and error feedback is thin where HTMX drives interaction

```
hx-get / hx-post      : 20
hx-target / hx-swap   : 41
hx-indicator          : 11
forms                 : 118
```

`loader.js` is high quality (delayed 200ms overlay, silent-request detection, button spinner API) and covers full-page navigations. But per-request feedback inside a page is largely absent: 11 indicators for 41 swap sites, and only `fastag_index.html` uses `htmx-indicator`. Combined with `hx-boost` on `<body>` (`layout.html:53`), a slow swap looks like nothing happened.

**Fix:** default `hx-indicator` via `htmx.config` or an `hx-boost`-level indicator; standardise a skeleton partial for table/list swaps.

### P1-6. Data tables degrade to horizontal scroll on mobile

58 `<table>` elements, 63 `overflow-x-auto` wrappers — so tables are **contained** (no page-level overflow, that part is handled). But `booking_list_table.html` is the representative case: 7 columns, one hidden below `sm`. On a 390px screen the user gets a horizontally-scrolling table.

There is no card/list fallback for any table. The board view (`bookings_board.html`) exists for bookings only.

**Fix:** pick one responsive table strategy and apply it repo-wide — either a card fallback below `md` (preferred for ops on phones) or a sticky first column plus priority-column hiding. One strategy, not 58 improvisations.

---

## 3. P2 — Medium

| Finding | Evidence |
|---|---|
| 117 inline `onclick`/`onchange`/`oninput` handlers, 216 inline `style=` | `internal/templates/` — CSP-hostile, untestable |
| Duplicate dead asset tree | `static/` (leaflet + 2 JS files) is a stale copy; the server serves `internal/static` (`internal/handlers/app.go:1539`). Unreferenced → drift trap |
| React island has a test script but **zero test files** | `src/tracking-island/package.json:9` `"test": "vitest run"`; no `*.test.*` exists → the script would fail. Not in CI either |
| Mobile: 780 hardcoded hex colors, no token layer | `mobile/src/` — `constants/theme.ts` exists but is bypassed |
| Mobile a11y near-absent | 15 `accessibilityLabel`, 8 `accessibilityRole` across 47 `.tsx` components |
| Mobile has no dark mode | only `theme.ts` + `GetStartedScreen.tsx` mention it; `useColorScheme` unused |
| **Brand divergence across surfaces** | Web: ops blue `#2563eb` (`src/input.css:19`). Driver app: WhatsApp green `#008069` (`mobile/src/constants/theme.ts:4`). Same product, two identities |
| Vestigial code | `layout.html:48` sets `documentElement.style.opacity = "1"` on DOMContentLoaded, but nothing ever sets it to `0` — dead |

**On the green:** for an Indian commercial-driver audience, WhatsApp affordances are a defensible deliberate choice, not an accident. But it is undocumented and unreconciled — the two surfaces should share tokens for status colors, spacing, and radius even if the brand accent differs.

---

## 4. What is already good (keep this)

- **`src/input.css`** — semantic tokens, `.dark` override block, a documented WCAG contrast correction (`#64748b` → `#5b6d85`, line 77–78), fluid `--text-display` clamp, `tabular-nums` for figures, rem-based type.
- **No-FOUC theming** — `partials/theme_head.html` sets `.dark` synchronously in `<head>` from cookie/localStorage/system. Correct.
- **`prefers-reduced-motion`** honoured in 4 places in `app.css`.
- **`loader.js`** — genuinely well-built: 200ms delay before showing, silent-request detection for polls/HTMX/JSON, `WeakMap` button spinners.
- **`session-guard.js`** — `BroadcastChannel` + localStorage multi-tab session conflict detection.
- **HTMX fragment architecture** — `HX-Request` detection + `{{.Content}}` injection (`internal/handlers/app.go:548`) is a clean server-rendered SPA-feel pattern.
- **Partial discipline** — 27 partials incl. `empty_state.html`, `pagination.html`, `btn.html`, `stat_card.html`; `booking_list_table.html` uses empty state + pagination correctly.
- **Interaction guards already exist** — `data-confirm` submit interception, double-submit protection (`layout.html:836`), focus trap + Escape for the mobile sidebar.
- **Mobile app** is substantive: offline command queue with idempotency, encrypted local store, trip state machine with tests, i18n in en/hi/gu.

The foundation is sound. The problem is that nothing **enforces** the standard the codebase already sets.

---

## 5. Remediation plan

**Phase 0 — stop the bleeding**
1. `Makefile`: `build: build-css build-tracking`
2. CI job: rebuild CSS → `git diff --exit-code` (catches P0-1 forever)
3. Fix the 6 broken classes in P0-3
4. Delete the dead `static/` tree

**Phase 1 — make "responsive" verifiable**
5. Playwright `projects` matrix: 390×844, 820×1180, 1440×900
6. CI job running `npx playwright test`
7. Specs for the 6 highest-traffic flows: login, dashboard, booking list, booking edit, tracking, invoice view — assert no horizontal overflow and that primary actions are reachable at 390px

**Phase 2 — accessibility**
8. `id`/`for` association across the 171 unlabelled fields
9. ≥44px touch targets baked into `btn.html`
10. `aria-live` on HTMX-swapped async regions

**Phase 3 — design-system convergence**
11. Ratcheting lint: raw-palette classes per file; unknown-utility classes fail the build
12. Replace the top 5 raw-palette classes with semantic tokens; fix dark-mode holes
13. Raise the 9–11px floor

**Phase 4 — mobile parity**
14. Extract `mobile/src/constants/theme.ts` into tokens; reconcile status/spacing/radius with web
15. `accessibilityLabel` on interactive components; dark mode via `useColorScheme`

---

## 6. Verification commands

```bash
# P0-1 — CSS drift audit (39 classes defined nowhere)
python scripts/css-sync-audit.py

# P0-1 — minimal proof
grep -n 'gap-x-4' internal/templates/home.html
grep -oE '\.gap-x-[0-9]+' internal/static/css/tailwind.css | sort -u

# P0-2 — confirm no browser tests in CI
grep -rn "playwright\|build-css" .github/workflows/   # NONE FOUND

# P1-1 — token bypass vs dark-mode coverage
grep -rhoE '\b(bg|text|border)-(slate|emerald|amber|rose|blue|red|violet)-[0-9]{2,3}' internal/templates/ | wc -l   # 1989
grep -rho 'dark:' internal/templates/ | wc -l                                                                        # 229

# P1-2 — label association
grep -rho '<label' internal/templates/ | wc -l                  # 235
grep -rhoE '<label[^>]*for=' internal/templates/ | wc -l        # 64

# P1-3 / P1-4 — typography and touch targets
grep -rhoE 'text-\[(9|10|11)px\]' internal/templates/ | wc -l    # 650
grep -rhoE 'min-h-\[44px\]|h-11|w-11' internal/templates/ | wc -l # 46
```

---

## 6b. Remediation status — 2026-09-14

Phase 0 was already complete before this pass (CSS sync is a hard CI gate;
the six broken classes in P0-3 are fixed). This pass implemented Phases 1–3 for
the **web console only**; the driver app (Phase 4) is untouched.

| Plan item | Status | Evidence |
|---|---|---|
| #5–6 Viewport matrix | ✅ | `playwright.config.ts` — `desktop` 1440×900 runs the whole suite; `tablet` 820×1180 and `mobile` 390×844 run `*.responsive.spec.ts` |
| #7 Specs for top flows | ✅ | `test/web.responsive.spec.ts` — login, dashboard, booking list (+populated), booking edit, invoices, customers |
| #8 Label/for association | ✅ | 64 → **210 of 235**; the remaining 25 are wrapper labels (valid) or static displays with no control |
| #9 ≥44px touch targets | ✅ | Baked into `partials/btn.html` + a `@media (max-width: 767px)` floor in `app.css`. Scoped to <768px on purpose: 44px is WCAG 2.5.5 (AAA) for thumbs; 2.5.8's 24px (AA) is the desktop bar |
| #10 aria-live / hx-indicator | ✅ | `#htmx-progress` + `#htmx-status` (role=status, aria-live) in `layout.html`; `aria-busy` on swap targets |
| #6 Responsive tables | ✅ | `.rtable` in `app.css` reflows a row into a card below `md` (`data-label` becomes the caption). Applied to **48 of 48** in-scope tables / **243** body cells. 7 document-like layouts deliberately excluded (see below) |
| #11 Ratchet lint | ✅ | `scripts/palette-ratchet.sh` + committed `scripts/palette-baseline.json`: per-file caps on raw-palette classes, wired into CI and `make check-ui`. New files start at 0 |
| #12 Status tokens | ✅ | Chip primitives (`bg-<fam>-soft`, `border-<fam>-line`, `text-<fam>-strong`, `text-<fam>-mid`) added to `src/input.css` and applied across 71 templates. **1,944 → 912** raw-palette classes (−1,032). Light mode provably unchanged |
| #13 Typography floor | ✅ | 267 × `text-[9px]`/`text-[10px]` → `text-[11px]`. **Zero** sub-11px text remains |

**Two new guards worth keeping:**
- `internal/handlers/templates_parse_guard_test.go` — Go parses all templates as
  one set, so a single syntax error 500s *every* page with a misleading
  "auth layout template not found". This fails fast with the real file+line.
- The responsive specs assert *what* overflows (naming the offending selector),
  not just that something does.

### Responsive-table coverage

`.rtable` is now on every table that is a *list* (a repeating set of records a
user scans or acts on). Seven layouts are intentionally **not** converted,
because they are documents rather than lists — reflowing them into cards would
destroy the layout they exist to convey:

| Template | Why excluded |
|---|---|
| `customer_statement.html`, `customer_statement_print.html` | A rendered account statement; print fidelity matters more than phone reflow |
| `invoice_view.html`, `invoice_pay.html`, `invoice_line_items.html` | An invoice is a document with a fixed visual form |
| `ewaybill_detail.html` | Single-record detail view, not a scannable list |
| `home.html` | Marketing/landing layout |

Two details that needed their own handling:

- **Row partials.** Four tables have a `<tbody>` that delegates to another
  template (`{{template "telemetry_device_row.html" .}}`), so the parent has no
  `<td>` to label. Captions live on the row partial instead, matching the
  parent's `<th>` order: `partials/ewaybill_row.html` (9), `scorecard_table.html`
  (7), `telemetry_device_row.html` (6), `telemetry_quarantine_row.html` (6).
- **Total rows.** `<tfoot>` is outside the card treatment, so a total would
  stack and lose its right alignment. `.rtable tfoot` is now a single flex line
  (label left, amount right); empty colspan spacers are dropped so the amount
  pins to the right edge. Affects `report_revenue.html`,
  `report_pending_payments.html`.

### Defect found while verifying this pass (out of scope, fixed anyway)

Running the full suite turned up `TestOpsErrorsAPIGetError` failing with
`UNIQUE constraint failed: error_reports.id`. Not a frontend issue, but it
blocked a green run so it is recorded here.

Root cause: `internal/operations/errors/reporter.go` built ids as
`err_<time.Now().UnixNano()>` / `inc_<...>`. **`time.Now()` does not advance
every nanosecond** — measured on this Windows box it steps in ~0.51 ms chunks
(20/20 back-to-back calls returned an identical value). Two error reports
raised inside one tick therefore collided.

It presented as a *flaky* failure, which is the dangerous part: 10 failures in
40 runs at HEAD on this machine, while Linux CI stays green because its clock
has ns resolution. Confirmed pre-existing by running the same test in a clean
`git worktree` at HEAD.

Fix: a single `newID(prefix)` helper appending a process-wide `atomic` counter
(`err_<unixnano>_<seq>`, `inc_<unixnano>_<seq>`), so uniqueness no longer
depends on clock resolution. `internal/operations/errors/reporter_id_test.go`
ships with it and was proven red before the fix (duplicate id on call 1).
`TestOpsErrorsAPIGetError` then went 40/40 clean, from 40/40 failing.

Related, **not** fixed: 17 other `time.Now().UnixNano()` id/ordering sites
exist across `internal/`. They are only unsafe where two ids can be minted
inside one tick, so they need auditing case by case — see §7.

### Raw-palette ratchet (#11)

`scripts/palette-ratchet.sh` freezes a per-file count of raw-palette classes in
`scripts/palette-baseline.json`. A file may go **down** freely (that is the
point) and may never go **up**; a file with no baseline entry starts at zero, so
new templates cannot open new debt. Same philosophy as the project's
`LINT_BASE`: legacy debt is tolerated, new debt is not — and because the
baseline is committed, re-baselining shows up in the PR diff.

Baseline at adoption: **87 files, 1,944 classes** (204 distinct). Three paths
were each exercised before the gate was trusted: regression (fails), new file
(fails), improvement (passes and reports the drop so it can be locked in).

Not counted: `white`/`black`/`current`/`transparent`, semantic tokens, arbitrary
values (`bg-[#fff]`), and anything inside `<script>`/`<style>`/comments — none
of those are a dark-mode contrast bug.

Implemented as **shell + one awk pass** (matching the other 21 scripts in
`scripts/`), not Python. Two things that are easy to get wrong and were caught
only by diffing against a reference implementation:

- **Arbitrary opacity syntax.** `hover:bg-amber-500/[0.1]` is legal Tailwind.
  After tokenising it leaves `bg-amber-500/` with a dangling slash, so a
  strictly-anchored regex skips it — undercounting `dashboard.html` by 23. The
  regex therefore ends in an optional `/?`. An undercounting ratchet is worse
  than none: it silently lets regressions through.
- **One process, not one per file.** The natural `awk | tr | grep | wc` per
  file is ~340 spawns and took **~85 s**; doing strip+tokenise+count inside a
  single awk over all files takes **~3.7 s**. A gate that slow gets skipped.

The port was verified by requiring byte-identical output to the Python original
(85 files / 968 classes) and by re-running all three paths — regression (fail),
new file (fail), improvement (pass, reports the drop).

### Chip migration (#12)

The top-5 classes turned out to be one repeating **chip**:
`bg-X-500/10` + `border-X-500/15` + `text-X-600|700`. Four token families were
added to `src/input.css` and applied across 71 templates:

| Raw class | Replacement | Light | Dark |
|---|---|---|---|
| `bg-X-500/10` | `bg-X-soft` | `X-500` @10% | `X-400` @15% |
| `border-X-500/15` | `border-X-line` | `X-500` @15% | `X-400` @25% |
| `text-X-700` | `text-X-strong` | `X-700` | `X-300` |
| `text-X-600` | `text-X-mid` | `X-600` | `X-400` |

Families: emerald, amber, rose, blue, violet, cyan, indigo, red, slate.

**Light mode is unchanged, and that is verified rather than asserted.** Tailwind
v4 compiles `/10` to `color-mix(in oklab, var(--color-X-500) 10%, transparent)`
— note `oklab`, not `srgb`; using the other space would have shifted every
chip. The replacement tokens emit the *identical* expression, confirmed by
diffing against the pre-migration bundle:

```
pre-migration:  border-color:color-mix(in oklab, var(--color-amber-500) 15%, transparent)
post-migration: --color-amber-line:color-mix(in oklab, var(--color-amber-500) 15%, transparent)
```

Result: **1,944 → 912** raw-palette classes (−1,032, matching the replacement
count exactly), and files with any raw palette went 87 → 79.

Two things deliberately left alone:
- **28 occurrences inside `<script>`/`<style>`** keep their raw classes. A class
  named only in JS may never be compiled by Tailwind's scanner, so renaming one
  there risks a class that does not exist.
- **Chips emitted from Go** — migrated too (see below).

### Chips emitted from Go

HTML built in Go string literals (HTMX fragments, badge helpers) never appears
in a template, so it was invisible to both the migration and the ratchet. Four
sites migrated:

| Site | Before | After |
|---|---|---|
| `maintenance.go` (Resolved chip) | `bg-emerald-500/10 border-emerald-500/15 text-emerald-700` | `bg-emerald-soft border-emerald-line text-emerald-strong` |
| `compliance.go` (warn/ok icons) | `text-amber-600`, `text-emerald-600` | `text-amber-mid`, `text-emerald-mid` |
| `kharcha.go` (approval banner) | `bg-emerald-50/60 … text-emerald-700` | `bg-emerald-soft … text-emerald-strong` |
| `scorecard.go` (`tierBadgeClass`) | `bg-emerald-100 text-emerald-700` | `badge-success` / `badge-info` / `badge-alert` |

Two disclosures, because not every mapping is a pure no-op:

- `tierBadgeClass` now returns the shared `badge-*` utilities, so a tier chip
  and a status chip are one mechanism. The text step moves from `-700` to the
  token's `-800` — a deliberate normalisation, not a slip.
- The `kharcha.go` wash moves from `emerald-50/60` to `emerald-500/10` (the
  shared chip wash), so light mode reads a touch more saturated there. That is
  the only Go site that is not light-identical.

**Tailwind cannot discover Go-emitted classes** — the same reason
`statusBadgeClass` needed a safelist. The five chip classes used from Go are
now listed in a second `@source inline(...)` in `src/input.css`, annotated with
the call sites; adding one in Go without adding it there is a silent no-op that
renders unstyled.

**The ratchet now scans `internal/**/*.go`** (excluding `*_test.go` and
`mobile/`), so this class of debt can no longer hide: 6 Go files carry **56**
raw-palette classes, now frozen in the baseline like everything else.

Two tests were coupled to the old strings and were rewritten to assert meaning
rather than styling: `TestUserOnboardingPage_DynamicSteps` counts
`>Pending</span>` / `>Completed</span>`; `TestScorecardTierBadge` asserts the
tier→badge mapping instead of a raw palette pair.

**Still open:** Phase 4 (mobile app) entirely; the 17 other clock-derived id
sites; and 968 raw-palette classes (912 in templates, 56 in Go) that are now
ratcheted but not yet migrated.

---

## 7. Open questions for the team

1. **Responsive target** — is the web console intended to be used on a phone in the field, or is that the driver app's job? This changes P1-6 from "card fallback" to "block mobile with a pointer to the app".
2. **Brand divergence** — is WhatsApp green for the driver app a deliberate product decision (adopt it on web for status colors) or legacy drift (converge on ops blue)?
3. **Browser support floor** — `container-queries` is loaded; is a modern-browser-only baseline acceptable, or is an older Android WebView in scope?
4. **Clock-derived ids** — audited; the dangerous one is fixed, the rest are registered below. The repo already has a canonical generator (`ports.IDGenerator` / `internal/shared/id`, used by `create_booking.go` and 12 other files), so the answer is *consolidate onto it*, not add a fourth helper.

**`time.Now()` does not advance every nanosecond.** On Windows it steps in ~0.5 ms chunks — measured: 20/20 back-to-back `UnixNano()` calls returned an identical value. Any two ids minted inside one tick are therefore EQUAL. This is invisible on Linux CI (ns-resolution clocks) and intermittent on Windows, which is what makes it a long-lived flaky-test generator rather than an obvious bug.

| Site | Verdict | Why |
|---|---|---|
| `operations/errors/reporter.go` | **fixed** | `err_`/`inc_` ids hit `UNIQUE constraint failed: error_reports.id`; 10/40 failures at HEAD on Windows |
| `customer/application/customer_service.go` | **fixed** | `booking_number` is `TEXT NOT NULL UNIQUE` and was `BKG-<UnixNano()%1000000>` — collision failed booking creation outright. Proven red (duplicate on call 1: `BKG-770500`); now uses `ports.IDGenerator` like the canonical `create_booking.go` path |
| `operations/notifications/service.go` (`notif_`) | unsafe | opaque PK, plausible under concurrent sends |
| `eta/service.go` (`eta-`) | unsafe | written to `audit_logs.id` |
| `founder/service.go` (`rev_`/`sys_`/`act_`) | unsafe | three PKs minted in one request path |
| `trip/application/assign_driver.go`, `assign_vehicle.go` (`ovr-`, `chk-`) | unsafe | PKs; several minted along one code path |
| `integration/gstn/einvoice.go` (`ACK`, `CNL`) | low | mock/demo only, behind `UseMock` |
| `operations/notifications/adapters.go` | safe | already mixes in random hex (MIME boundary) |
| `agent/rl/store.go` | safe | primary path is `crypto/rand`; nanos is only the error fallback |
| `experiments/experiments.go` | safe-ish | two independent clock components, still not collision-proof |
5. **(answered)** The #12 chip migration uses dark-aware equivalents — light mode stays pixel-identical (verified against the pre-migration bundle, not just asserted).
6. **(answered)** The four Go-emitted chip sites are migrated, and the ratchet now scans `internal/**/*.go` so the hole stays closed. 56 raw-palette classes remain across 6 Go files (`alerts.go` 9, `compliance.go` 19, `invoices.go` 4, `kharcha.go` 20, `maintenance.go` 1, `scorecard.go` 3) — mostly one-off error/banner fragments. They are frozen in the baseline; working them down needs a token + a `@source inline` entry per class.
