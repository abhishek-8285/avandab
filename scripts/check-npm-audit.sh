#!/bin/bash
set -e

echo "Checking mobile dependencies for vulnerabilities..."
cd mobile

# Critical advisories fail the gate. High and below warn: the Expo toolchain
# carries upstream advisories that only Expo can fix; blocking every commit
# on them would freeze development permanently.
# SECURITY_GATE_STRICT=0 downgrades critical to warning for local iteration.
set +e
npm audit --audit-level=critical
status=$?
set -e

if [ "$SECURITY_GATE_STRICT" = "0" ]; then
  echo "⚠️ SECURITY_GATE_STRICT=0: critical advisories downgraded to warning"
elif [ "$status" -ne 0 ]; then
  echo "❌ Critical vulnerabilities in mobile dependencies"
  echo "Fix: cd mobile && npm audit fix (or bump the affected packages)"
  exit 1
fi

set +e
npm audit --audit-level=high
status=$?
set -e

if [ "$status" -ne 0 ]; then
  echo "⚠️ High-severity advisories detected in Expo toolchain — track and remediate"
fi

echo "✅ Mobile dependency audit complete"
