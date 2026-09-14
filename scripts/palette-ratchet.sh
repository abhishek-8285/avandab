#!/usr/bin/env bash
#
# Raw-palette ratchet for templates and Go handlers (frontend audit #11).
#
# Semantic tokens in src/input.css respond to `.dark`; raw Tailwind palette
# classes (bg-emerald-500, text-slate-700, ...) do not. A text-emerald-700 on a
# dark surface is a contrast failure, and hand-patching `dark:` variants cannot
# keep up with ~1,900 usages.
#
# Rewriting them in one PR is not reviewable, so this is a RATCHET, same
# philosophy as the project's LINT_BASE: legacy debt is tolerated, NEW debt is
# not. Per-file counts are frozen in palette-baseline.json; a file may go DOWN
# freely (that is the point) and may never go UP. Re-baselining is a deliberate
# act that shows up in the PR diff, because the baseline is committed.
#
# Everything -- stripping, tokenising, counting AND the baseline comparison --
# runs in ONE awk process over all files at once. The obvious per-file
# pipeline (awk | tr | grep | wc per file) is ~340 process spawns and took
# ~85s; this takes ~1s. A CI gate that slow gets skipped.
#
# Usage:
#   ./scripts/palette-ratchet.sh                 # check (CI gate)
#   ./scripts/palette-ratchet.sh --report        # worst offenders, never fails
#   ./scripts/palette-ratchet.sh --update-baseline
#
# Exit 1 when any file regressed, or when a file with no baseline entry
# introduces raw-palette classes (new code starts at zero).
#
# Not counted: white/black/current/inherit/transparent, semantic tokens
# (bg-surface-container-low, text-text-muted, badge-*, *-soft/*-line/
# *-strong/*-mid), arbitrary values (bg-[#fff], text-[11px]), and anything
# inside <script>/<style> or a comment -- none of those are a dark-mode bug.

set -uo pipefail

cd "$(dirname "$0")/.." || exit 1

BASELINE="scripts/palette-baseline.json"

# ---------------------------------------------------------------------------
# Scanning: one awk process, all files.
#
# Stripping is a line-based state machine with three flags, because no regex
# can reliably match "everything up to </script>" when the JS itself contains
# a '<'. `low` is rebuilt from `line` after every edit so positions stay valid.
#
# Tokenising splits on anything that cannot be part of a class token, then
# requires a whole-token match -- ERE has no lookarounds, so matching against
# raw text would let two adjacent classes share a boundary and undercount.
#
# The trailing `[/]?` handles Tailwind's ARBITRARY opacity syntax,
# `hover:bg-amber-500/[0.1]`: tokenising leaves `bg-amber-500/` with a dangling
# slash, and without it every one of those is skipped (it undercounted
# dashboard.html by 23, which would have let real regressions through).
#
# `[/]` rather than `\/` keeps the regex free of backslashes so it survives
# being an awk string literal. Intervals are spelled out ([0-9][0-9][0-9]?)
# rather than {2,3} for the same portability reason.
# ---------------------------------------------------------------------------
read -r -d '' AWK_SCAN <<'AWK'
BEGIN {
    RE = "^(bg|text|border|ring|divide|from|via|to|fill|stroke|outline|decoration|caret|accent|shadow)" \
         "-(slate|gray|zinc|neutral|stone|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose)" \
         "-[0-9][0-9][0-9]?([/][0-9][0-9]?)?[/]?$"
}
function flush() { if (cur != "" && cnt > 0) printf "%d\t%s\n", cnt, cur }
FNR == 1 { flush(); cur = FILENAME; cnt = 0; ins = 0; inc = 0; ing = 0 }
{
    line = $0
    while (1) {
        low = tolower(line)
        if (inc) { p = index(low, "-->");        if (p) { line = substr(line, p + 3); inc = 0; continue } else { line = ""; break } }
        if (ing) { p = index(low, "*/}}");       if (p) { line = substr(line, p + 4); ing = 0; continue } else { line = ""; break } }
        if (ins) { p = match(low, /<\/(script|style)[^>]*>/); if (p) { line = substr(line, p + RLENGTH); ins = 0; continue } else { line = ""; break } }
        p = 0; kind = ""
        a = index(low, "<!--")
        b = match(low, /<(script|style)[^>]*>/)
        c = index(low, "{{/*")
        if (a && (!p || a < p)) { p = a; kind = "htmlc" }
        if (b && (!p || b < p)) { p = b; kind = "tag"; blen = RLENGTH }
        if (c && (!p || c < p)) { p = c; kind = "goc" }
        if (!p) break
        if (kind == "htmlc") {
            e = index(substr(low, p + 4), "-->")
            if (e) { line = substr(line, 1, p - 1) substr(line, p + 4 + e - 1 + 3); continue }
            line = substr(line, 1, p - 1); inc = 1; break
        }
        if (kind == "goc") {
            e = index(substr(low, p + 4), "*/}}")
            if (e) { line = substr(line, 1, p - 1) substr(line, p + 4 + e - 1 + 4); continue }
            line = substr(line, 1, p - 1); ing = 1; break
        }
        e = match(substr(low, p + blen), /<\/(script|style)[^>]*>/)
        if (e) { line = substr(line, 1, p - 1) substr(line, p + blen + e - 1 + RLENGTH); continue }
        line = substr(line, 1, p - 1); ins = 1; break
    }
    n = split(line, tok, /[^A-Za-z0-9_\/-]+/)
    for (i = 1; i <= n; i++) if (tok[i] ~ RE) cnt++
}
END { flush() }
AWK

# "count<TAB>path" is the working shape throughout.
scan() {
    local -a files=()
    while IFS= read -r -d '' f; do files+=("$f"); done < <(
        find internal/templates -type f -name '*.html' -print0 2>/dev/null
        find internal -type f -name '*.go' ! -name '*_test.go' \
             -not -path '*/node_modules/*' -not -path '*/mobile/*' -print0 2>/dev/null
    )
    [ ${#files[@]} -eq 0 ] && return 0
    awk "$AWK_SCAN" "${files[@]}"
}

# ---------------------------------------------------------------------------
# Comparison: baseline file first, then current counts on stdin.
# Emits REG/NEW/IMP rows; bash renders them.
# ---------------------------------------------------------------------------
read -r -d '' AWK_DIFF <<'AWK'
NR == FNR {
    if (match($0, /"internal\/[^"]*": *[0-9]+/)) {
        s = substr($0, RSTART, RLENGTH)
        q = index(s, "\":")
        base[substr(s, 2, q - 2)] = substr(s, q + 2) + 0
    }
    next
}
{ split($0, f, "\t"); have[f[2]] = f[1] }
END {
    for (p in have) {
        if (!(p in base))        printf "NEW\t%s\t%d\t0\n", p, have[p]
        else if (have[p] > base[p]) printf "REG\t%s\t%d\t%d\n", p, have[p], base[p]
    }
    for (p in base) {
        c = (p in have) ? have[p] : 0
        if (c < base[p]) printf "IMP\t%s\t%d\t%d\n", p, c, base[p]
    }
}
AWK

MODE="${1:-}"
case "$MODE" in
    ""|--check) MODE=check ;;
    --report)   MODE=report ;;
    --update-baseline) MODE=update ;;
    -h|--help) sed -n '3,33p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "usage: $0 [--report|--update-baseline]" >&2; exit 2 ;;
esac

if [ "$MODE" = report ]; then
    scan | sort -t$'\t' -k1,1nr -k2,2 | head -25 |
        awk -F'\t' '{ printf "  %5s  %s\n", $1, $2; t += $1; n++ }
                    END { printf "\nfiles: %d   total: %d\n", n, t }'
    exit 0
fi

CURRENT=$(scan)
NFILES=$(printf '%s\n' "$CURRENT" | grep -c .)
TOTAL=$(printf '%s\n' "$CURRENT" | awk -F'\t' '{ t += $1 } END { printf "%d", t + 0 }')

if [ "$MODE" = update ]; then
    {
        echo '{'
        echo '  "_comment": "Per-file cap on raw Tailwind palette classes. Frozen by scripts/palette-ratchet.sh - a file may go DOWN freely, never UP. Regenerate with --update-baseline and expect to justify the change in review.",'
        echo '  "version": 1,'
        echo '  "files": {'
        printf '%s\n' "$CURRENT" | sort -t$'\t' -k1,1nr -k2,2 |
            awk -F'\t' '{ printf "    \"%s\": %s\n", $2, $1 }' |
            awk '{ b[NR] = $0 } END { for (i = 1; i <= NR; i++) printf "%s%s\n", b[i], (i < NR ? "," : "") }'
        echo '  }'
        echo '}'
    } > "$BASELINE"
    echo "baseline updated: $NFILES files, $TOTAL raw-palette classes"
    exit 0
fi

# --- check -----------------------------------------------------------------
[ -f "$BASELINE" ] || { echo "no baseline at $BASELINE - run --update-baseline" >&2; exit 1; }

DIFF=$(printf '%s\n' "$CURRENT" | awk "$AWK_DIFF" "$BASELINE" -)
# Row shape is "IMP<TAB>path<TAB>now<TAB>baseline" -- $3 is the CURRENT count
# and $4 the FROZEN one, so the drop is $4-$3 and it reads "baseline -> now".
ROWS=$(printf '%s\n' "$DIFF" | awk -F'\t' '$1 == "IMP" { printf "  -%d  %-56s %s -> %s\n", $4 - $3, $2, $4, $3 }')

if [ -n "$ROWS" ]; then
    echo "improved (baseline will be tightened by --update-baseline):"
    printf '%s\n' "$ROWS"
    echo
fi

BAD=$(printf '%s\n' "$DIFF" | awk -F'\t' '$1 != "IMP"')
if [ -z "$BAD" ]; then
    echo "OK: no raw-palette regression ($NFILES files, $TOTAL classes)"
    [ -n "$ROWS" ] && echo "note: some files improved - run --update-baseline to lock it in."
    exit 0
fi

echo "FAIL: raw-palette classes increased"
echo
printf '%s\n' "$BAD" | awk -F'\t' '{
    why = ($1 == "NEW") ? "new file (no baseline entry)" : "regressed"
    printf "  %s  %s\n      baseline %d -> now %d (+%d)\n", why, $2, $4, $3, $3 - $4
}'
echo
echo "Raw palette classes do not respond to .dark, so every one of these is a"
echo "latent dark-mode contrast bug. Use the semantic tokens in src/input.css."
echo "If a regression is unavoidable, re-baseline deliberately:"
echo "  ./scripts/palette-ratchet.sh --update-baseline"
exit 1
