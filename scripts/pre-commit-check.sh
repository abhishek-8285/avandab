#!/usr/bin/env bash
# ==============================================================================
# Pre-Commit FAST Gate (~2 min): commit often, fail fast.
# Full suite runs on pre-push + CI. See hooks/pre-push.
# ==============================================================================
set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
YELLOW='\033[0;33m'
NC='\033[0m'

echo -e "${BLUE}==============================================================================${NC}"
echo -e "${BLUE}🔍 Running Pre-Commit Verification Suite...${NC}"
echo -e "${BLUE}==============================================================================${NC}"

# 1. Rebuild Tailwind CSS from source
echo -e "\n${CYAN}[1/7] Rebuilding Tailwind CSS bundle...${NC}"
npx @tailwindcss/cli -i src/input.css -o internal/static/css/tailwind.css --minify 2>&1
echo -e "${GREEN}✅ Tailwind CSS rebuilt.${NC}"

# 2. Verify no CDN fallback scripts left in templates
echo -e "\n${CYAN}[2/7] Checking for CDN Tailwind references in templates...${NC}"
CDN_REFS=$(grep -rn 'cdn.tailwindcss.com' internal/templates/ || true)
if [ -n "$CDN_REFS" ]; then
    echo -e "${RED}❌ Error: CDN Tailwind references found in templates:${NC}"
    echo "$CDN_REFS"
    echo -e "${YELLOW}Remove CDN <script> tags — styles must come from /static/css/tailwind.css only.${NC}"
    exit 1
fi
echo -e "${GREEN}✅ No CDN references found.${NC}"

# 3. Format Check (gofmt)
echo -e "\n${CYAN}[3/7] Checking Go code formatting (gofmt)...${NC}"
UNFORMATTED=$(gofmt -l -s . | grep -v '^dist/' || true)
if [ -n "$UNFORMATTED" ]; then
    echo -e "${RED}❌ Error: The following Go files are not formatted:${NC}"
    echo "$UNFORMATTED"
    echo -e "${YELLOW}Run 'gofmt -w -s .' to fix automatically.${NC}"
    exit 1
fi
echo -e "${GREEN}✅ Formatting check passed.${NC}"

# 2. Go Vet Analysis
echo -e "\n${CYAN}[4/7] Running go vet static analysis...${NC}"
go vet ./...
echo -e "${GREEN}✅ Go vet passed.${NC}"

# 3. Security Scanners (golangci-lint, govulncheck, npm audit)
echo -e "\n${CYAN}[5/7] Running security scanner suite...${NC}"
# Ratchet: gate only code newer than HEAD until legacy lint debt is burned down
export LINT_BASE="$(git rev-parse HEAD)"
./scripts/security-check.sh
echo -e "${GREEN}✅ Security checks passed.${NC}"

# 4. SQLC Out-of-Date Check
echo -e "\n${CYAN}[6/7] Checking sqlc generated files integrity...${NC}"
if [ -f "/tmp/go/bin/sqlc" ]; then
    /tmp/go/bin/sqlc generate
else
    go run github.com/sqlc-dev/sqlc/cmd/sqlc@latest generate
fi

if [ -n "$(git diff --name-only db/generated/)" ]; then
    echo -e "${RED}❌ Error: sqlc generated files are out of date.${NC}"
    git diff db/generated/
    echo -e "${YELLOW}Run 'sqlc generate' and commit the updated files.${NC}"
    exit 1
fi
echo -e "${GREEN}✅ sqlc files up to date.${NC}"

# 5. Tests for CHANGED packages only (full suite is pre-push + CI).
echo -e "\n${CYAN}[7/8] Testing changed packages...${NC}"
CHANGED_GO=$(git diff --cached --name-only --diff-filter=ACM | grep '\.go$' || true)
if [ -z "$CHANGED_GO" ] && ! git diff --cached --name-only | grep -qE '^(internal/templates/|db/migrations)'; then
    echo -e "${GREEN}✅ No Go/template/migration changes — skipping tests.${NC}"
else
    PKGS=$(echo "$CHANGED_GO" | grep -v '_test\.go$' | xargs -r -n1 dirname | sort -u | sed 's|^|./|' | tr '\n' ' ')
    # Template changes render through handlers tests; migrations touch everything.
    # (-run subsets apply to handlers only; other packages always run fully.)
    HANDLERS_RUN=""
    if git diff --cached --name-only | grep -q '^internal/templates/'; then
        PKGS="$PKGS ./internal/handlers/"
        HANDLERS_RUN="-run 'Template|Render|Ratchet|Registry|Guard|Tabs_Wired|FeaturesLink'"
    fi
    if git diff --cached --name-only | grep -qE '^db/migrations'; then
        PKGS="$PKGS ./db/ ./internal/handlers/"
        HANDLERS_RUN="-run 'Template|Render|Ratchet|Registry|Guard|Migration|Parity|Tabs_Wired|FeaturesLink'"
    fi
    if [ -z "$PKGS" ]; then PKGS="./internal/handlers/"; fi
    if echo "$PKGS" | grep -q './internal/handlers/'; then
        # shellcheck disable=SC2086
        go test -timeout 10m -count=1 $HANDLERS_RUN ./internal/handlers/
        PKGS=$(echo "$PKGS" | sed 's|./internal/handlers/||')
    fi
    if [ -n "$(echo "$PKGS" | tr -d ' ')" ]; then
        # shellcheck disable=SC2086
        go test -timeout 10m -count=1 $PKGS
    fi
    unset HANDLERS_RUN
    echo -e "${GREEN}✅ Changed-package tests passed.${NC}"
fi

# 6. Whole-repo compile (catches cross-package breaks cheaply).
echo -e "\n${CYAN}[8/8] Compiling all packages...${NC}"
go build ./...
echo -e "${GREEN}✅ Build passed.${NC}"

echo -e "\n${GREEN}==============================================================================${NC}"
echo -e "${GREEN}🎉 All pre-commit checks passed! Your commit is ready to push.${NC}"
echo -e "${BLUE}==============================================================================${NC}\n"
