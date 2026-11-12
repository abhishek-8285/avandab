# Decision Brief — Web Console UX (UX-1…UX-3)

> **Status: PROPOSED — awaiting product sign-off.** Companion to
> `docs/PLAN-product-details-2026-11-11.md` §3-E and `docs/10` §7.
> Purpose: turn three open UX questions into one-line decisions with evidence,
> so they stop blocking pilot sign-off.
>
> Method: static audit of the working tree on 2026-11-11. UI files were not
> modified (concurrent agent refactors are in `internal/` backend files).
> Evidence commands are in §5 and reproducible.

---

## 1. UX-1 — Responsive target

**Question (`docs/10` §7.1):** is the web console meant to be used on a phone in
the field, or is that the driver app's job?

### Current state (evidence)

| Layer | Evidence |
|---|---|
| Web console size | 158 top-level page templates (`internal/templates/*.html`) |
| Mobile nav exists | sidebar overlay + toggle `internal/templates/layout.html:85,472` |
| Card reflow | `.rtable` responsive tables in **46** files; CSS contract `internal/static/css/app.css:999-1020` |
| Touch targets | ≥44px floor under `<768px` `internal/static/css/app.css:1187-1196` |
| Responsive tests | 5 responsive specs: `test/web.responsive.spec.ts`, `tracking.responsive`, `trip-view.responsive`, `vehicle-view.responsive`, `vehicles.responsive` |
| Playwright projects | desktop 1440×900 (full suite), tablet 820×1180, mobile 390×844 (`playwright.config.ts:65-115`) |
| CI gate | Chromium installed + suite run in CI (`.github/workflows/ci.yml:298,304`) |
| Document layouts excluded | 7 (invoices, statements, EWB detail, home) deliberately not reflowed — `docs/10` §6b |
| Driver app | Expo SDK ~57 / RN 0.86.3, 52 `.tsx` files, offline command queue, trip state machine, en/hi/gu i18n |

Conclusion: the web console **already has a real phone story** for
reading/approving, but not full data entry parity. The driver app owns field
execution. The open question is whether to *guarantee* more than that.

### Options

| Option | What it means | Effort | Risk |
|---|---|---|---|
| **A. Two-surface contract** (recommended) | Web = desktop/tablet management console; phone supported for read/approve on named flows; driver app owns field actions | S — document matrix, add 2–3 mobile specs | Low; avoids over-promising |
| B. Web phone-first | Full responsive parity for all forms, dense modules, invoice editing | L — 158 templates, 169 forms (current count), a11y pass | High; duplicates driver app, heavy regression surface |
| C. Block phones on web | Show "use the driver app" pointer under `md` | S — but discards already-shipped responsive work | Low effort, negative UX for managers on the move |

### Recommendation

**A.** Matches the investment already made (`rtable`, touch targets, responsive
specs are in tree and CI), keeps "field = driver app" clear, and gives sales a
defensible support statement instead of "it's responsive, probably".

Acceptance criteria if A is chosen:
1. A support matrix doc: per module, `phone read`, `phone act`, `desktop only`.
2. Playwright mobile project green on the named `phone act` flows (already
   covered: login, dashboard, bookings, invoices, customers; extend to approvals).
3. Dense/document screens (invoices, statements, EWB detail) show a non-blocking
   "best on desktop" hint below `md`; never break, never silently hide data.
4. No new responsive work on excluded document layouts without a ticket.

---

## 2. UX-2 — Brand divergence (web ops blue vs app WhatsApp green)

**Question (`docs/10` §7.2):** is WhatsApp green for the driver app deliberate,
or legacy drift that should converge on ops blue?

### Current state (evidence)

| Surface | Evidence |
|---|---|
| Web tokens | `src/input.css:6` comment "Brand scale (Fleetbase ops blue)"; `--color-primary: #2563eb` at `src/input.css:19` |
| Mobile tokens | `mobile/src/constants/theme.ts:5-18` — **explicitly documented WhatsApp palette**: `#008069` "WhatsApp Deep Green", `#25d366` "WhatsApp Vibrant Green Accent", `#efeae2` "Classic WhatsApp soft background" |
| Semantics check | Mobile `primary` is green, `success` is a different green (`#00a884`); web `primary` is blue, success is green |
| Design-system debt | palette ratchet currently records **1,689 raw-palette classes across 103 files** (`bash scripts/palette-ratchet.sh --report`); dark variants **352** in templates |

Both palettes are intentional (comments name the inspirations). This is a
deliberate visual language choice, not a missing token: the driver app mimics a
familiar consumer messenger; the web console is an operations tool.

### Options

| Option | What it means | Effort | Risk |
|---|---|---|---|
| **A. Deliberate divergence, documented** (recommended) | Keep two palettes; write ADR + shared brand anchors (logo, wordmark, status semantics, typography) | S — ADR + mapping table | Low; avoids a large, untested re-theme |
| B. Converge mobile on ops blue | Re-theme 52 `.tsx` files, re-run 69 mobile suites, retrain drivers | L | Medium-high; loses the familiar driver aesthetic for no user complaint |
| C. Converge web on WhatsApp green | Re-theme 158 templates + 1,689 ratcheted classes | XL | High; ops console loses status-colour distinction (green = brand *and* success) |

### Recommendation

**A**, with one guardrail: mobile `primary` (green) must never be used for
"success" semantics, and web must not adopt green as brand. Put the mapping in
the ADR:
- brand primary: web blue / app green (intentional, per-surface);
- semantic success/warning/danger: identical meaning and near-identical hue on
  both surfaces (already true: app `success #00a884`, web status-success tokens);
- logo/wordmark/type: shared.

Acceptance criteria: ADR merged; a one-page brand/semantics table in docs; new
screens use semantic tokens on web (ratchet already enforces no new raw
palette); no re-theme work scheduled without a user-facing reason.

---

## 3. UX-3 — Browser support floor

**Question (`docs/10` §7.3):** modern-browser-only, or is an older Android
WebView in scope?

### Current state (evidence)

| Fact | Evidence |
|---|---|
| Tailwind | v4.3.3 (`package.json:24-27`), container-queries plugin loaded (`src/input.css:3`) |
| Built CSS uses modern features | `internal/static/css/tailwind.css`: `@property` ×73, `color-mix(` ×598, `oklch(` ×131 |
| Container queries actually used | **0** occurrences of `@container` in the bundle — the plugin is not the constraint |
| Browser matrix in CI | Chromium only (`ci.yml:298` installs `chromium`); no WebKit/Firefox project in `playwright.config.ts` |
| Explicit browser metadata | no `.browserslistrc`, no `browserslist` key in `package.json` |
| Tailwind v4 published baseline | Safari 16.4+, Chrome 111+, Firefox 128+ (per [Tailwind v4 compatibility docs](https://tailwindcss.com/docs/compatibility) — re-verify at sign-off) |

The floor is **already decided by the toolchain**, not by product: Tailwind v4
emits `@property`/`oklch()`/`color-mix()` and does not transcompile them. An
older Android WebView without these renders broken surfaces regardless of what
we document.

### Options

| Option | What it means | Effort | Risk |
|---|---|---|---|
| **A. Codify the modern baseline** (recommended) | Document Chrome/Edge 111+, Safari 16.4+, Firefox 128+, Android WebView 111+ (WebView updates via Play Store on supported Android); no polyfills | S | Low |
| B. Support older WebViews (e.g. WebView 90) | Downgrade Tailwind v4 / hand-written fallback CSS for tokens | XL | High; fights the toolchain, no known customer requirement |
| C. Add WebKit/Firefox Playwright smoke | Extra CI projects for top 6 flows (orthogonal to floor) | M | CI time only |

### Recommendation

**A**, plus **C as optional follow-up** (WebKit is the closest proxy for iOS
Safari; Firefox coverage catches engine-specific regressions). Do not promise
"works on any Android browser"; state the floor in sales/support docs.

Acceptance criteria: baseline written in docs (support page + `docs/`), Playwright
Chromium matrix stays the gate, WebKit/Firefox smoke ticket opened (optional),
no polyfill work.

---

## 4. Adjacent findings (not part of UX-1…3; open tickets)

Found while gathering evidence; none blocks pilot, but they contradict
"audit closed" claims in `docs/10`:

1. **Typography floor regressed — FIXED 2026-11-11.** `docs/10` §6b reported
   "zero sub-11px text"; measured **29** instances (`text-[8px]`/`[9px]`/`[10px]`)
   in 6 templates (`video_dashboard` 14, `control_tower` 8, `cold_chain_dashboard` 3,
   `status` 2, `settlement_view` 1, `feature` 1× 8px). All bumped to `text-[11px]`;
   ratchet added as `internal/handlers/templates_typography_guard_test.go`
   (red-proven on the 29 violations, green after). Stale CSS bundle
   (`status.html`: `hover:opacity-80`, `bg-amber-400/80`, `bg-red-500/80`) rebuilt
   so `css-sync-audit.py` is green again.
2. **Palette debt reduced — 2026-11-11.** Repo ratchet **1,689 -> 1,268**
   raw-palette classes (-25%, 99 files), baseline tightened. Waves: chip-token
   contract on the three newest dashboards; then 19 more templates; then 137
   redundant `dark:` companions removed; then contract extended to the last five
   families used (sky/teal/purple/zinc/orange, 40 tokens) and 73 more conversions.
   Light mode unchanged by construction; dark-mode chips without `dark:` variants
   now flip correctly. Regression caught and fixed: JS-authored badge classes
   (`geofence_draw.js`, `compress-image.js`) are safelisted and
   `css-sync-audit.py` bucket C fails on any future JS-only palette class with no
   compiled rule. Remaining debt is data-viz/marketing brand colour
   (`feature.html` 81, `video_dashboard` 70, `dashboard.html` 60) — needs a chart
   palette decision, not a mechanical swap.
3. **Browser gate is Chromium-only.** No WebKit/Firefox project; Safari-specific
   layout regressions are invisible to CI today.

Each is a small, independent ticket — do not bundle them into pilot scope.

---

## 5. Evidence commands (reproducible)

```bash
# UX-1
ls internal/templates/*.html | wc -l                                  # 158
grep -rl 'rtable' internal/templates | wc -l                         # 46
grep -n 'mobile-overlay\|md:hidden' internal/templates/layout.html   # 85, 472
ls test/*.spec.ts | grep responsive                                  # 5 files
grep -n "name: 'desktop'\|name: 'tablet'\|name: 'mobile'" playwright.config.ts
grep -n 'playwright' .github/workflows/ci.yml                        # install + run

# UX-2
grep -n 'color-primary:' src/input.css                               # #2563eb, ops blue
grep -n 'WhatsApp' mobile/src/constants/theme.ts                     # explicit palette
bash scripts/palette-ratchet.sh --report | head                     # 1,689 classes
grep -rho 'dark:' internal/templates/ | wc -l                        # 352

# UX-3
grep -n 'tailwindcss' package.json                                   # v4.3.3
grep -oE '@property|color-mix\(|oklch\(|@container' internal/static/css/tailwind.css | sort | uniq -c
ls .browserslistrc 2>/dev/null; grep -n browserslist package.json    # none
grep -n 'playwright install' .github/workflows/ci.yml                # chromium only

# Adjacent findings
grep -rnoE 'text-\[(8|9|10)px\]' internal/templates | wc -l   # 29 before fix, 0 after
go test ./internal/handlers -run TestTemplates_NoTextBelow11px -count=1
```

---

## 6. Sign-off

| Question | Proposed answer | Decided by | Date |
|---|---|---|---|
| UX-1 | A — two-surface contract (web read/approve, app field execution) | | |
| UX-2 | A — deliberate divergence, shared brand anchors + semantic mapping | | |
| UX-3 | A — modern baseline codified; WebKit/Firefox smoke optional | | |

After sign-off: update `docs/PLAN-product-details-2026-11-11.md` UX-1…UX-3 rows,
record material choices with `record_decision`, and open the §4 adjacent-finding
tickets separately.
