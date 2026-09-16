# Archived HTML mockups (10 files)

Moved here on 2026-09-16 during the UI consistency pass (Issue #5:
"design alag, app alag"). These files were **not referenced by any code**
— pure design explorations.

## Why archived instead of kept in assets/

They speak a different design language than the shipped app:

| | These mockups | Shipped app (`src/`) |
|---|---|---|
| Style | Material You tokens (`on-tertiary-fixed-variant`, …) | WhatsApp-style theme (`mobile/src/constants/theme.ts`) |
| Primary | Teal `#0d9488` / `#00685f` | WhatsApp green `#008069` |
| Mode | Dark-mode variants | Light only |
| Login | "Logistics Management Portal" + Google/Microsoft buttons | "Driver Ops" email + password |

Keeping them next to the real screen images (`login_screen.png`, … — those
ARE used, do not move them) confused "which design is final".

## To restore one

```bash
git mv mobile/assets/_archive/<name>.html mobile/assets/
```

## To delete permanently

Only after confirming no future redesign will reuse them:

```bash
git rm mobile/assets/_archive/<name>.html
```
