#!/usr/bin/env bash
# ==============================================================================
# Pre-Commit Automated Quality & Integrity Gate
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

# 5. Unit Tests
echo -e "\n${CYAN}[7/7] Executing Go unit tests...${NC}"
# 30m (not the 10m default): the ./test integration package alone needs
# ~12m on weak devices (122-migration setup per test); hangs still fail.
go test -timeout 30m -v ./...
echo -e "${GREEN}✅ All unit tests passed.${NC}"

echo -e "\n${GREEN}==============================================================================${NC}"
echo -e "${GREEN}🎉 All pre-commit checks passed! Your commit is ready to push.${NC}"
echo -e "${BLUE}==============================================================================${NC}\n"
