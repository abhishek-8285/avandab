#!/usr/bin/env bash
set -euo pipefail
# ponytail: grep ratchet, not a service. Weekly cron fails until placeholders cleared — delete check when fixed.
fail=0
if grep -q "Full registered-office details on request" internal/templates/consumer_compliance.html 2>/dev/null; then
  echo "::warning::Compliance remainder: CIN + registered-office still placeholder (consumer_compliance.html)"
  fail=1
fi
if ! grep -qE "CIN: U[0-9]{5}[A-Z]{2}[0-9]{4}" internal/templates/partials/footer.html 2>/dev/null; then
  echo "::warning::Compliance remainder: footer still missing CIN (partials/footer.html)"
  fail=1
fi
# NCH convergence has no API — manual check, always remind until you remove this line after registering.
echo "::notice::Reminder: register NCH convergence at https://consumerhelpline.gov.in (manual) — remove this notice after done"
if [ "$fail" -ne 0 ]; then
  echo "=> Fix before 1 Jan 2027 enforcement. See docs/COMPLIANCE-ECOMMERCE-AMENDMENT-2026-09.md"
  exit 1
fi
echo "Compliance remainder: placeholders cleared (NCH manual step still verify)"
