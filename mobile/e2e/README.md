# Mobile E2E (Playwright) — driver lifecycle

Scaffold for the product-spec driver lifecycle: launch → injected login →
simulated dispatch → trip card → GPS points → ePOD capture → offline expense →
reconnect flush. The Go backend is mocked entirely with `page.route()`.

## Prerequisites

1. `@playwright/test` must be resolvable from `mobile/`. It is currently
   installed at the **repo root** (`node_modules/@playwright/test`), which Node
   resolves via parent-directory lookup. If it is ever removed from the root,
   install it in `mobile/` (`npm i -D @playwright/test`) plus browsers:
   `npx playwright install chromium`.
2. A static web export must exist:
   `cd mobile && npx expo export --platform web` (produces `./dist`).

## Run

```bash
npm run e2e        # = playwright test --config=e2e/playwright.config.ts
```

The config serves `dist/` on http://localhost:8099 via `npx serve`
(`reuseExistingServer: true`). Auth is injected into localStorage before app
boot, so the authenticated driver navigator renders directly.

## Status

These specs are deliverable scaffolding and are NOT wired into CI. Selectors
against the real web export need a pass of manual verification before they are
treated as stable gates.
