#!/bin/bash
set -e

echo "Checking mobile dependencies for vulnerabilities..."
cd mobile

# Critical advisories fail the gate. High and below warn: the Expo toolchain
# carries upstream advisories that only Expo can fix; blocking every commit
# on them would freeze development permanently.
# SECURITY_GATE_STRICT=0 downgrades critical to warning for local iteration.

# Documented exceptions. One line per advisory id, with the reason it cannot
# reach a shipped build, and only for advisories that have NO patched release —
# an accepted advisory that gains a fix must be removed here, not left behind.
#
#   GHSA-pqg4-j6r4-53mv  shell-quote  critical, command injection in quote().
#     Path: react-native → react-devtools-core → shell-quote. react-devtools-core
#     is required only inside `if (__DEV__)` in react-native's
#     Libraries/Core/setUpReactDevTools.js, so a release bundle never loads it.
#     No patched shell-quote is published (1.10.0 is the latest) and
#     react-native@0.86.3 is the newest stable, so there is nothing to upgrade
#     to. Remove this line once react-native drops the dependency.
ACCEPTED_ADVISORIES="GHSA-pqg4-j6r4-53mv"

# Every critical advisory NOT on that list, as "<id> (<package>)" lines.
# npm audit exits non-zero when it finds anything, hence set +e.
set +e
critical_ids="$(npm audit --json 2>/dev/null | node -e '
let raw = "";
process.stdin.on("data", (c) => (raw += c));
process.stdin.on("end", () => {
  const accepted = new Set(process.argv[1].split(" ").filter(Boolean));
  const found = [];
  try {
    const data = JSON.parse(raw);
    for (const [name, v] of Object.entries(data.vulnerabilities || {})) {
      if (v.severity !== "critical") continue;
      for (const via of v.via || []) {
        if (via && typeof via === "object" && via.url) {
          const id = (String(via.url).match(/GHSA-[a-z0-9-]+/) || [])[0];
          if (id && !accepted.has(id)) found.push(id + " (" + name + ")");
        }
      }
    }
  } catch (e) {
    console.error("npm audit --json parse failed: " + e.message);
    process.exit(1);
  }
  console.log([...new Set(found)].join("\n"));
});
' "$ACCEPTED_ADVISORIES")"
audit_status=$?
set -e

set +e
npm audit --audit-level=critical
set -e

if [ "$SECURITY_GATE_STRICT" = "0" ]; then
  echo "⚠️ SECURITY_GATE_STRICT=0: critical advisories downgraded to warning"
elif [ -n "$critical_ids" ]; then
  echo "❌ Critical vulnerabilities in mobile dependencies:"
  printf '   %s\n' "$critical_ids"
  echo "Fix: cd mobile && npm audit fix (or bump the affected packages)"
  exit 1
fi

if [ -n "$ACCEPTED_ADVISORIES" ]; then
  echo "⚠️ Accepted critical advisories (documented, still unfixed):"
  for id in $ACCEPTED_ADVISORIES; do echo "   $id — see the reason above this script's allowlist"; done
fi

set +e
npm audit --audit-level=high
status=$?
set -e

if [ "$status" -ne 0 ]; then
  echo "⚠️ High-severity advisories detected in Expo toolchain — track and remediate"
fi

echo "✅ Mobile dependency audit complete"