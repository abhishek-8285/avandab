#!/usr/bin/env bash
# Smoke-crawl a deployed web UI the way the 2026-09 UI failures were found:
# register -> onboard -> GET every core route, fail on 4xx/5xx, redirect
# loops back to /company/onboard, or template-error markers in HTML.
# Usage: ./scripts/smoke.sh [base-url]   (default https://dev.avandab.com)
set -euo pipefail

BASE="${1:-https://dev.avandab.com}"
JAR="$(mktemp -d)/jar.txt"
STAMP="$(date +%s)"
EMAIL="smoke${STAMP}@test.local"
PASS="Smoke-Pass-123!"

post() { # path, field=value... (all form fields)
    local path="$1"; shift
    local args=()
    for f in "$@"; do args+=(--data-urlencode "$f"); done
    curl -s --max-time 15 -b "$JAR" -c "$JAR" -X POST "$BASE$path" \
        -H "Origin: $BASE" -H "Referer: $BASE$path" "${args[@]}" \
        -o /dev/null -w "%{http_code} %{redirect_url}\n" || echo "000 -"
}

echo "== smoke $BASE as $EMAIL"
post /register "name=Smoke" "email=$EMAIL" "password=$PASS" "confirm_password=$PASS" "company_name=SmokeCo" >/dev/null
code=$(post /company/onboard "company_name=SmokeCo" "address=1 Main St, Pune" "phone=9876543210" "email=$EMAIL" | cut -d' ' -f1)
[ "$code" = "303" ] || [ "$code" = "200" ] || { echo "ONBOARD FAILED: $code"; exit 1; }

# route -> expected title fragment (empty = any 200, no error markers).
# Newline-separated: titles may contain spaces.
ROUTES="/dashboard:Dashboard
/bookings:Bookings
/bookings/board:Bookings Board
/trips:Trips
/drivers:Drivers
/vehicles:Vehicles
/customers:Customers
/invoices:Invoices
/payments:Payments
/settings:Settings
/users:Users
/reports:Reports
/alerts:
/audit-logs:Audit
/geofences:Geofences
/ewaybill:Way
/ops/errors:Error
/profile:Profile"
FAIL=0
while IFS= read -r entry; do
    [ -z "$entry" ] && continue
    path="${entry%%:*}"; want="${entry#*:}"
    body="$(mktemp)"
    code=$(curl -s --max-time 15 -b "$JAR" -o "$body" -w "%{http_code}" "$BASE$path")
    final=$(curl -s --max-time 15 -b "$JAR" -o /dev/null -w "%{url_effective}" "$BASE$path")
    bad=""
    [ "$code" -ge 400 ] && bad="HTTP $code"
    [[ "$final" == */company/onboard ]] && bad="bounced to onboard"
    # Go-runtime/template failure signatures only — generic phrases like
    # "stack trace" are legit UI copy (error detail drawer shows them).
    grep -qiE "can't evaluate field|error calling|nil pointer|panic:|runtime error:|template error|value method.*called using.*nil" "$body" && bad="template error"
    if [ -n "$want" ] && ! grep -q "<title>[^<]*$want" "$body"; then bad="title missing '$want'"; fi
    rm -f "$body"
    if [ -n "$bad" ]; then echo "FAIL $path: $bad"; FAIL=1; else echo "ok   $path"; fi
done <<< "$ROUTES"
[ "$FAIL" = "0" ] && echo "SMOKE GREEN" || { echo "SMOKE RED"; exit 1; }
