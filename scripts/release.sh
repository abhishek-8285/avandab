#!/usr/bin/env bash
# release.sh — Production Release Automation (P5.3)
#
# Generates semver tag, builds version-stamped binary, updates CHANGELOG.md,
# atomically swaps bin/server symlink, retains last N binaries, and creates git tag.
set -euo pipefail

KEEP_RELEASES="${KEEP_RELEASES:-5}"
NO_TAG=0
DRY_RUN=0
TARGET_VERSION=""

usage() {
  cat <<EOF
Usage: $0 [VERSION|patch|minor|major] [options]

Arguments:
  VERSION       Explicit semver tag (e.g. v1.0.0) or bump keyword (patch, minor, major).
                Defaults to 'patch' if omitted.

Options:
  --keep N      Number of release binaries to retain in bin/releases/ (default: 5)
  --no-tag      Skip git tagging
  --dry-run     Inspect planned release without building or tagging
  -h, --help    Show this message
EOF
  exit 1
}

# Parse options
while [[ $# -gt 0 ]]; do
  case "$1" in
    --keep)
      KEEP_RELEASES="$2"
      shift 2
      ;;
    --no-tag)
      NO_TAG=1
      shift
      ;;
    --dry-run)
      DRY_RUN=1
      shift
      ;;
    -h|--help)
      usage
      ;;
    *)
      if [ -z "$TARGET_VERSION" ]; then
        TARGET_VERSION="$1"
        shift
      else
        echo "❌ Unexpected argument: $1" >&2
        usage
      fi
      ;;
  esac
done

TARGET_VERSION="${TARGET_VERSION:-patch}"

# 1. Determine SemVer Tag
get_latest_semver_tag() {
  git tag -l "v[0-9]*.[0-9]*.[0-9]*" | sort -V | tail -n 1
}

LATEST_TAG=$(get_latest_semver_tag)

compute_next_version() {
  local bump_type="$1"
  local base="${LATEST_TAG:-v0.9.9}" # Start at v1.0.0 if no prior semver tag
  local clean_base="${base#v}"
  IFS='.' read -r major minor patch <<< "$clean_base"

  case "$bump_type" in
    major)
      echo "v$((major + 1)).0.0"
      ;;
    minor)
      echo "v${major}.$((minor + 1)).0"
      ;;
    patch)
      echo "v${major}.${minor}.$((patch + 1))"
      ;;
    v[0-9]*.[0-9]*.[0-9]*)
      echo "$bump_type"
      ;;
    [0-9]*.[0-9]*.[0-9]*)
      echo "v$bump_type"
      ;;
    *)
      echo "❌ Invalid version or bump type '$bump_type'. Use 'patch', 'minor', 'major', or 'vX.Y.Z'." >&2
      exit 1
      ;;
  esac
}

VERSION=$(compute_next_version "$TARGET_VERSION")

echo "=================================================="
echo "🚀 Preparing Release: ${VERSION}"
echo "   Previous Tag:     ${LATEST_TAG:-<none>}"
echo "   Retention Policy: Keep last ${KEEP_RELEASES} binaries"
echo "=================================================="

if [ "$DRY_RUN" -eq 1 ]; then
  echo "🔍 [DRY-RUN] Would build bin/releases/server-${VERSION}"
  echo "🔍 [DRY-RUN] Would update CHANGELOG.md"
  echo "🔍 [DRY-RUN] Would create git tag ${VERSION}"
  exit 0
fi

# 2. Generate Changelog Entry
CHANGELOG_FILE="CHANGELOG.md"
DATE_STR=$(date +%Y-%m-%d)
RANGE="${LATEST_TAG:+$LATEST_TAG..HEAD}"
RANGE="${RANGE:-HEAD~20..HEAD}"

TMP_CHANGELOG=$(mktemp)
cat <<EOF > "$TMP_CHANGELOG"
## [${VERSION}] - ${DATE_STR}

EOF

# Categorize commits
FEATS=$(git log "$RANGE" --pretty=format:"- %s (%h)" --grep="^feat" || true)
FIXES=$(git log "$RANGE" --pretty=format:"- %s (%h)" --grep="^fix" || true)
PERF=$(git log "$RANGE" --pretty=format:"- %s (%h)" --grep="^perf" || true)
OPS=$(git log "$RANGE" --pretty=format:"- %s (%h)" --grep="^\(chore\|ops\|ci\|refactor\|docs\)" || true)
OTHER=$(git log "$RANGE" --pretty=format:"- %s (%h)" --invert-grep --grep="^\(feat\|fix\|perf\|chore\|ops\|ci\|refactor\|docs\)" || true)

if [ -n "$FEATS" ]; then
  echo "### Features" >> "$TMP_CHANGELOG"
  echo "$FEATS" >> "$TMP_CHANGELOG"
  echo "" >> "$TMP_CHANGELOG"
fi

if [ -n "$FIXES" ]; then
  echo "### Bug Fixes" >> "$TMP_CHANGELOG"
  echo "$FIXES" >> "$TMP_CHANGELOG"
  echo "" >> "$TMP_CHANGELOG"
fi

if [ -n "$PERF" ]; then
  echo "### Performance Improvements" >> "$TMP_CHANGELOG"
  echo "$PERF" >> "$TMP_CHANGELOG"
  echo "" >> "$TMP_CHANGELOG"
fi

if [ -n "$OPS" ]; then
  echo "### Maintenance & Operations" >> "$TMP_CHANGELOG"
  echo "$OPS" >> "$TMP_CHANGELOG"
  echo "" >> "$TMP_CHANGELOG"
fi

if [ -n "$OTHER" ]; then
  echo "### Other Changes" >> "$TMP_CHANGELOG"
  echo "$OTHER" >> "$TMP_CHANGELOG"
  echo "" >> "$TMP_CHANGELOG"
fi

# Prepend to existing CHANGELOG.md
if [ -f "$CHANGELOG_FILE" ]; then
  cat "$CHANGELOG_FILE" >> "$TMP_CHANGELOG"
fi
mv "$TMP_CHANGELOG" "$CHANGELOG_FILE"
echo "📝 Updated ${CHANGELOG_FILE}"

# 3. Build Version-Stamped Binary
mkdir -p bin/releases
RELEASE_BIN="bin/releases/server-${VERSION}"

echo "🔨 Compiling version-stamped binary: ${RELEASE_BIN}..."
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.Version=${VERSION}" -o "${RELEASE_BIN}" ./cmd/server
chmod +x "${RELEASE_BIN}"

# Verify binary stamps version
STAMPED_VER=$("${RELEASE_BIN}" --version 2>/dev/null || echo "")
if [[ "$STAMPED_VER" != *"$VERSION"* ]]; then
  echo "⚠️ Warning: Binary self-reported version '${STAMPED_VER}' does not match '${VERSION}'"
fi

# 4. Atomic Symlink Swap
ln -sfn "releases/server-${VERSION}" bin/server
echo "${VERSION}" > bin/releases/current
echo "🔗 Swapped active binary: bin/server -> ${RELEASE_BIN}"

# 5. Prune Old Binaries (Keep last N)
echo "🧹 Pruning old release binaries (keeping last ${KEEP_RELEASES})..."
# List files sorted by creation/version ascending, keep last N
BINARIES=( $(ls -1v bin/releases/server-* 2>/dev/null || true) )
TOTAL_BINS=${#BINARIES[@]}

if [ "$TOTAL_BINS" -gt "$KEEP_RELEASES" ]; then
  DELETE_COUNT=$((TOTAL_BINS - KEEP_RELEASES))
  for ((i=0; i<DELETE_COUNT; i++)); do
    BIN_TO_DEL="${BINARIES[$i]}"
    # Never delete the currently active binary
    if [ "$BIN_TO_DEL" != "$RELEASE_BIN" ]; then
      echo "   Removing stale release binary: ${BIN_TO_DEL}"
      rm -f "${BIN_TO_DEL}"
    fi
  done
fi

# 6. Git Tagging
if [ "$NO_TAG" -eq 0 ]; then
  if git rev-parse "$VERSION" >/dev/null 2>&1; then
    echo "ℹ️ Git tag ${VERSION} already exists, skipping tag creation."
  else
    git tag -a "${VERSION}" -m "Release ${VERSION} (${DATE_STR})"
    echo "🏷️ Created git tag: ${VERSION}"
  fi
fi

echo "=================================================="
echo "✅ Release ${VERSION} complete!"
echo "   Binary:   ${RELEASE_BIN}"
echo "   Symlink:  bin/server -> releases/server-${VERSION}"
echo "   Rollback: ./scripts/rollback.sh"
echo "=================================================="
