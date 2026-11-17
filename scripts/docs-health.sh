#!/usr/bin/env bash
set -uo pipefail

# docs-health.sh - repo markdown freshness + link/anchor integrity. ADVISORY.
#
# The vault has a decay system; the repo had nothing. docs/pipeline.md sat
# stale through a whole capacity change with no machine noticing. This is the
# repo-internal detector: mechanical, pull-based, and it never blocks a tag.
#
# Two signals, one script (card t_42a86188):
#
#   1. FRESHNESS = change-coupling. Each doc maps to the code it describes.
#      A coupled doc is DUE when its mapped code changed after the doc did.
#      A doc with no narrow code owner falls back to AGE: due when at least
#      one stable release tag landed after the doc's last commit AND the doc
#      has been untouched for AGE_MIN_DAYS. Both rules read git commit times,
#      never filesystem mtimes - a fresh checkout stamps every file with the
#      checkout clock, so mtime would measure the clone, not the writing.
#
#   2. LINK INTEGRITY. Every internal markdown link resolves to a tracked
#      file; every #anchor resolves to a real heading in its target file.
#      Inline links only (`[text](target)`, images included): reference-style
#      definitions (`[text][ref]` + `[ref]: path`) are not resolved. Fenced and
#      inline code is skipped, so an example inside a fence never counts.
#
# Usage:
#     scripts/docs-health.sh [repo-root]        # default: $PWD
#
# Environment:
#     DOCS_HEALTH_MAP    path to a mapping table overriding the one below
#                        (tests and probes: same `doc[,doc]|path|path...` shape;
#                        a line with no `|` is an age-fallback line)
#     DOCS_HEALTH_NOW    epoch "now" for the age rule (tests pin the clock)
#     GITHUB_STEP_SUMMARY  when set, a markdown report appends to it
#
# Output:
#     the full due table on stdout, then a single greppable verdict line:
#         DOCS WARNING: <n> due, <m> broken links      (findings exist)
#         DOCS OK                                       (nothing to report)
#     A tracked .md with no mapping row is its own DOCS WARNING line and
#     suppresses the DOCS OK: a new doc must be given a row, otherwise the
#     detector silently ignores it - which is the failure it exists to stop.
#
# Exit codes: 0 always on findings (advisory - a stale doc never fails the
# nightly, never blocks a tag). 2 on tool failure only: no git, not a
# checkout, a shallow clone (last-touch times and tag counts are unknowable
# there), a mapping row naming an untracked doc or a path with no tracked
# files. Mapping rot is a broken tool, not a finding.
#
# Scope: every tracked .md file. Link targets are checked against
# `git ls-files`, so a link to a file nobody committed is broken by
# definition.

AGE_MIN_DAYS=30

die() {
  echo "docs-health: $1" >&2
  exit 2
}

# --- the mapping table ------------------------------------------------------
# doc[,doc2...]|code-path|code-path...
#
# A code path is a git pathspec: a directory prefix or a file. Every row is
# validated at run time - a row that names a path with no tracked files is a
# tool failure, so a rename upstream forces this table to stay true.
#
# Refinements against the card's initial table (each grounded in the doc's own
# cross-references, not guessed):
#   - docs/configuration.md: the card's `config/` does not exist in this tree;
#     the knobs live in cmd/omega/config.go and are consumed by agent/.
#   - docs/release-gate.md: scripts/release-gate.sh does not exist; the doc
#     names scripts/journeys/, train-pass.sh, review-at-close.sh, fence-test.sh.
#   - docs/api.md: added api/, where the shell ring actually serves routes.
#   - docs/approvals.md: the card's agent/ alone misses harness/approval/,
#     which the doc names twice as the capsule that owns the feature.
#   - bin/skills/learn-skill/SKILL.md: coupled to the validator script it
#     documents, not to its own directory (a doc coupled to itself is
#     structurally never due).
mapping_table() {
  cat <<'MAP'
docs/api.md|agent/|api/|cmd/
docs/cli.md|cmd/
docs/configuration.md|cmd/omega/config.go|agent/
docs/webui.md|webui/
docs/modules.md,docs/migrating-a-module.md,docs/removing-a-module.md|harness/
docs/pipeline.md|scripts/|.github/workflows/
docs/release-gate.md|scripts/journeys/|scripts/train-pass.sh|scripts/review-at-close.sh|scripts/fence-test.sh|.github/workflows/release.yml
docs/approvals.md|harness/approval/|agent/
bin/skills/learn-skill/SKILL.md|bin/skills/learn-skill/scripts/
docs/architecture.md
docs/troubleshooting.md,docs/extraction-recipe.md,docs/project-trust.md
AGENTS.md,.agents/COMMIT.md,.agents/DESIGN.md,.agents/REVIEW.md
README.md,CONTRIBUTING.md,harness/README.md,scripts/journeys/README.md,.github/PULL_REQUEST_TEMPLATE.md
MAP
}

# --- helpers ----------------------------------------------------------------

fmt_date() { # <epoch> -> YYYY-MM-DD, "-" for 0 or empty
  if [ -z "${1:-}" ] || [ "$1" -le 0 ] 2>/dev/null; then
    printf '%s' '-'
    return
  fi
  date -u -d "@$1" +%Y-%m-%d 2>/dev/null || printf '%s' "@$1"
}

# norm <relpath>: lexical normalization, no filesystem, no realpath dependency
norm() {
  local p=$1 out=() seg joined="" sep=""
  local IFS=/
  for seg in $p; do
    case "$seg" in
      '' | .) ;;
      ..) [ ${#out[@]} -gt 0 ] && unset 'out[${#out[@]}-1]' ;;
      *) out+=("$seg") ;;
    esac
  done
  for seg in "${out[@]}"; do
    joined="$joined$sep$seg"
    sep=/
  done
  printf '%s' "$joined"
}

# slug <heading text>: ASCII GitHub-style anchor slug. Docs are ASCII by house
# style (no emoji, no long dashes), so non-ASCII letters are not preserved.
slug() {
  printf '%s' "$1" | tr '[:upper:]' '[:lower:]' \
    | sed -e 's/`//g' \
      -e 's/\*//g' \
      -e 's/\[\([^]]*\)\]([^)]*)/\1/g' \
      -e 's/[^a-z0-9 _-]//g' \
      -e 's/ /-/g'
}

# prose_body <file>: the file minus fenced code blocks, minus inline code - a
# link or a heading shown as an example is not a link or a heading.
prose_body() {
  awk '/^```/{fence=!fence;next} !fence' "$1" 2>/dev/null | sed -e 's/`[^`]*`//g'
}

# anchors_of <file>: every anchor the file can be linked to, one per line -
# heading slugs (GitHub's -1/-2 disambiguation included) plus explicit
# <a id="..">/<a name=".."> targets.
anchors_of() {
  local f=$1 body h s c list=""
  declare -A cnt=()
  body="$(awk '/^```/{fence=!fence;next} !fence && /^#{1,6} /{sub(/^#{1,6} +/,"");print}' "$f" 2>/dev/null)"
  while IFS= read -r h; do
    [ -n "$h" ] || continue
    s="$(slug "$h")"
    c="${cnt[$s]:-0}"
    cnt[$s]=$((c + 1))
    [ "$c" -gt 0 ] && s="$s-$c"
    list="$list$s"$'\n'
  done <<<"$body"
  while IFS= read -r h; do
    [ -n "$h" ] && list="$list$h"$'\n'
  done < <(grep -oE '<a (id|name)="[^"]+"' "$f" 2>/dev/null | sed -e 's/^.*="//' -e 's/"$//')
  printf '%s' "$list"
}

# --- inputs -----------------------------------------------------------------

ROOT="${1:-$PWD}"
ROOT="$(cd "$ROOT" 2>/dev/null && pwd)" || die "repo root not found: ${1:-$PWD}"
cd "$ROOT" || die "cannot enter $ROOT"

command -v git >/dev/null 2>&1 || die "git not found"
git rev-parse --git-dir >/dev/null 2>&1 || die "not a git checkout: $ROOT"
# always measure the whole tree: `git ls-files` from a subdirectory answers
# relative to that subdirectory, which would hide every doc outside it
TOP="$(git rev-parse --show-toplevel 2>/dev/null)" || die "no work tree at $ROOT"
[ -n "$TOP" ] && { cd "$TOP" || die "cannot enter $TOP"; }
[ "$(git rev-parse --is-shallow-repository 2>/dev/null || echo false)" != "true" ] \
  || die "shallow checkout - last-touch times need full history (fetch-depth: 0, fetch-tags: true)"

NOW="${DOCS_HEALTH_NOW:-$(date -u +%s)}"
re='^[0-9]+$'
[[ "$NOW" =~ $re ]] || die "DOCS_HEALTH_NOW must be an epoch integer (got $NOW)"

if [ -n "${DOCS_HEALTH_MAP:-}" ]; then
  [ -f "$DOCS_HEALTH_MAP" ] || die "DOCS_HEALTH_MAP not found: $DOCS_HEALTH_MAP"
  MAPPING="$(cat "$DOCS_HEALTH_MAP")"
else
  MAPPING="$(mapping_table)"
fi

TRACKED="$(git ls-files)"
[ -n "$TRACKED" ] || die "no tracked files (empty checkout?)"

# stable release tags with their commit date, read once
TAGS="$(git for-each-ref --format='%(refname:short) %(creatordate:unix)' refs/tags 2>/dev/null)"

is_tracked() { # <path>
  printf '%s\n' "$TRACKED" | grep -qxF -- "$1"
}

# --- freshness --------------------------------------------------------------

DOCS=()
MODES=()
DOCD=()
REFD=()
STAT=()
add_row() {
  DOCS+=("$1")
  MODES+=("$2")
  DOCD+=("$3")
  REFD+=("$4")
  STAT+=("$5")
}

MAPPED=""
while IFS= read -r line; do
  [ -n "$line" ] || continue
  case "$line" in
    \#*) continue ;;
  esac
  docfield="${line%%|*}"
  if [ "$docfield" = "$line" ]; then
    globs_raw=""
  else
    globs_raw="${line#*|}"
  fi
  IFS=',' read -r -a doclist <<<"$docfield"
  for doc in "${doclist[@]}"; do
    doc="${doc#"${doc%%[![:space:]]*}"}"
    doc="${doc%"${doc##*[![:space:]]}"}"
    [ -n "$doc" ] || continue
    is_tracked "$doc" || die "mapping names an untracked doc: '$doc' (row: $line)"
    MAPPED="$MAPPED$doc"$'\n'
    doc_ct="$(git log -1 --format=%ct -- "$doc" 2>/dev/null)"
    doc_ct="${doc_ct:-0}"

    if [ -n "$globs_raw" ]; then
      IFS='|' read -r -a globs <<<"$globs_raw"
      paths=()
      for g in "${globs[@]}"; do
        [ -n "$g" ] || continue
        if [ -z "$(git ls-files -- "$g" | head -1)" ]; then
          die "mapping names a path with no tracked files: '$g' (doc: $doc)"
        fi
        paths+=("$g")
      done
      [ ${#paths[@]} -gt 0 ] || die "mapping row has no code paths: $line"
      # one log call over every path: the newest commit touching any of them
      code_ct="$(git log -1 --format=%ct -- "${paths[@]}" 2>/dev/null)"
      code_ct="${code_ct:-0}"
      if [ "$code_ct" -gt "$doc_ct" ]; then
        add_row "$doc" coupled "$(fmt_date "$doc_ct")" "code $(fmt_date "$code_ct")" DUE
      else
        add_row "$doc" coupled "$(fmt_date "$doc_ct")" "code $(fmt_date "$code_ct")" ok
      fi
    else
      age_days=$(((NOW - doc_ct) / 86400))
      [ "$age_days" -ge 0 ] || age_days=0
      releases="$(printf '%s\n' "$TAGS" | awk -v ct="$doc_ct" \
        '$1 ~ /^v[0-9]/ && index($1, "-") == 0 && $2 > ct {n++} END {print n + 0}')"
      releases="${releases:-0}"
      if [ "$releases" -gt 0 ] && [ "$age_days" -ge "$AGE_MIN_DAYS" ]; then
        add_row "$doc" age "$(fmt_date "$doc_ct")" "${age_days}d, ${releases} rel" DUE
      else
        add_row "$doc" age "$(fmt_date "$doc_ct")" "${age_days}d, ${releases} rel" ok
      fi
    fi
  done
done <<<"$MAPPING"

# every tracked .md must have a row: an unmapped doc is silently unmeasured
UNMAPPED=()
TRACKED_MD="$(printf '%s\n' "$TRACKED" | grep -E '\.md$' || true)"
while IFS= read -r doc; do
  [ -n "$doc" ] || continue
  printf '%s\n' "$MAPPED" | grep -qxF -- "$doc" && continue
  UNMAPPED+=("$doc")
  add_row "$doc" - "$(fmt_date "$(git log -1 --format=%ct -- "$doc" 2>/dev/null || echo 0)")" "-" UNMAPPED
done <<<"$TRACKED_MD"

# --- link + anchor integrity ------------------------------------------------

BROKEN=()
LINKS=0
while IFS= read -r doc; do
  [ -n "$doc" ] || continue
  targets="$(prose_body "$doc" | grep -oE '!?\[[^]]*\]\([^)[:space:]]+' | sed -e 's/^.*](//' | sort -u || true)"
  [ -n "$targets" ] || continue
  while IFS= read -r t; do
    [ -n "$t" ] || continue
    case "$t" in
      http://* | https://* | mailto:* | tel:*) continue ;;
    esac
    LINKS=$((LINKS + 1))
    tpath="$t"
    anchor=""
    case "$t" in
      *'#'*)
        tpath="${t%%#*}"
        anchor="${t#*#}"
        ;;
    esac
    if [ -z "$tpath" ]; then
      target="$doc"
    else
      target="$(norm "$(dirname "$doc")/$tpath")"
    fi
    if ! is_tracked "$target"; then
      BROKEN+=("$doc -> $t (target not tracked: $target)")
      continue
    fi
    if [ ! -f "$target" ]; then
      BROKEN+=("$doc -> $t (tracked but missing on disk: $target)")
      continue
    fi
    if [ -n "$anchor" ] && ! anchors_of "$target" | grep -qxF -- "$anchor"; then
      BROKEN+=("$doc -> $t (no heading anchor #$anchor in $target)")
    fi
  done <<<"$targets"
done <<<"$TRACKED_MD"

# --- report -----------------------------------------------------------------

due=0
coupled=0
fallback=0
for i in "${!DOCS[@]}"; do
  [ "${MODES[$i]}" = coupled ] && coupled=$((coupled + 1))
  [ "${MODES[$i]}" = age ] && fallback=$((fallback + 1))
  [ "${STAT[$i]}" = DUE ] && due=$((due + 1))
done
broken="${#BROKEN[@]}"
unmapped="${#UNMAPPED[@]}"

echo "docs-health: ${#DOCS[@]} doc(s) - $coupled coupled, $fallback age fallback, $LINKS internal link(s) checked"
echo
printf '%-38s %-8s %-12s %-17s %s\n' "doc" "mode" "doc touched" "code / age" "status"
printf '%s\n' "$(printf '%-38s %-8s %-12s %-17s' "----" "----" "-----------" "----------" | tr ' ' '-')"
for i in "${!DOCS[@]}"; do
  printf '%-38s %-8s %-12s %-17s %s\n' \
    "${DOCS[$i]}" "${MODES[$i]}" "${DOCD[$i]}" "${REFD[$i]}" "${STAT[$i]}"
done

if [ "$broken" -gt 0 ]; then
  echo
  echo "broken links:"
  for b in "${BROKEN[@]}"; do
    echo "  $b"
  done
fi

report() {
  echo "## Repo docs health (advisory)"
  echo
  echo "**$due due, $broken broken links** - advisory only: this never blocks a tag"
  echo "and never fails the nightly. A coupled doc is due when its mapped code"
  echo "changed after it; an age-fallback doc is due when a stable release landed"
  echo "after it and it has been untouched for $AGE_MIN_DAYS+ days."
  echo
  echo "| doc | mode | doc touched | code / age | status |"
  echo "|-----|------|-------------|------------|--------|"
  for i in "${!DOCS[@]}"; do
    echo "| \`${DOCS[$i]}\` | ${MODES[$i]} | ${DOCD[$i]} | ${REFD[$i]} | ${STAT[$i]} |"
  done
  if [ "$broken" -gt 0 ]; then
    echo
    echo "### Broken links"
    echo
    for b in "${BROKEN[@]}"; do
      echo "- $b"
    done
  fi
  if [ "$unmapped" -gt 0 ]; then
    echo
    echo "### Unmapped tracked docs (give them a row in \`scripts/docs-health.sh\`)"
    echo
    for u in "${UNMAPPED[@]}"; do
      echo "- \`$u\`"
    done
  fi
  echo
  echo "Fixing is separate work: docs fixes arrive as cards, \`AGENTS.md\` and"
  echo "\`.agents/*.md\` are write-guard (main session, live approval)."
}

if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
  report >>"$GITHUB_STEP_SUMMARY"
fi

# exactly one verdict line per run: the unmapped line replaces the DOCS OK
# rather than stacking above it, and the due/broken contract line is the last
# `DOCS ...` line (the ::warning:: annotation follows it, exactly as
# drift-gate's advisory mode orders its two lines)
if [ "$unmapped" -gt 0 ]; then
  echo "DOCS WARNING: $unmapped unmapped tracked doc(s): ${UNMAPPED[*]}"
fi
if [ "$due" -gt 0 ] || [ "$broken" -gt 0 ]; then
  echo "DOCS WARNING: $due due, $broken broken links"
  echo "::warning::DOCS ADVISORY: $due doc(s) due, $broken broken internal link(s) - advisory, no tag blocked"
elif [ "$unmapped" -eq 0 ]; then
  echo "DOCS OK"
fi
exit 0
