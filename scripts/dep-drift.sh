#!/usr/bin/env bash
set -uo pipefail

# dep-drift.sh - declared dependencies vs upstream, as a drift table. ADVISORY.
#
# A pinned dependency rots silently. agent-browser floated 0.42.0 -> 0.43.0
# and broke the nightly for three rounds (2026-11-15/16): nothing in the repo
# compared what is declared against what upstream ships, so the first evidence
# of a stale pin was a red gate with no named cause. This turns that into one
# line: `0.43 released Tuesday` (card t_5a2d18f7).
#
# Two sources, one table:
#
#   go    - `go list -m -u all`: every module in the build list whose upstream
#           has something newer. Only rows with an update exist at all; the
#           kind column separates a direct requirement (a project decision)
#           from an indirect one and from a module the graph pulls in without
#           go.mod naming it.
#   webui - `npm outdated --json` for what upstream ships, resolved against
#           webui/package-lock.json for what the build actually installs. A
#           bare `npm outdated` in a checkout without node_modules has no
#           installed version to compare, so it reports every dependency as
#           outdated - the lockfile is the current side. Declared is the
#           package.json range, which is what a human edits.
#
# Delta classes, by core version: MAJOR / MINOR / PATCH. PSEUDO covers a
# pseudo-version bump (identical core, newer revision - what
# `v0.0.0-2026...` updates look like); a core that moves from a prerelease to
# its own release (`v1.2.3-rc.1` -> `v1.2.3`) is PATCH, the least that is true.
# An upstream not ahead of what is resolved is not drift and never gets a row.
#
# Usage:
#     scripts/dep-drift.sh [repo-root]        # default: the script's parent
#
# Environment:
#     DEP_DRIFT_GO          go binary (default: go)
#     DEP_DRIFT_NPM         npm binary (default: npm)
#     DEP_DRIFT_WEBUI       webui directory (default: <root>/webui)
#     DEP_DRIFT_GO_INPUT    file overriding the go query, one row per line:
#                             <module>|<resolved>|<latest>|<kind>
#                           (tests and probes: no network)
#     DEP_DRIFT_NPM_INPUT   file with the JSON `npm outdated --json` prints
#                           (the lockfile is still read for the resolved side)
#     GITHUB_STEP_SUMMARY   when set, a markdown report appends to it
#
# Output: the full drift table on stdout, then one greppable verdict line:
#     DEP DRIFT: <n> outdated (<m> major)     (findings exist)
#     DEP DRIFT OK                            (nothing behind upstream)
#
# Exit codes: 0 always on findings. Advisory by construction - a drift line
# never blocks a tag and never fails the nightly; the signal is the greppable
# line, the same shape as drift-gate's DRIFT_ADVISORY mode and docs-health's
# DOCS WARNING. Exit 2 on tool failure only: no go, no npm, a go query that
# could not reach the module proxy, an npm query that produced no JSON, a
# missing webui/package.json or package-lock.json (the resolved side would be
# unknowable, and a table built on guesses is worse than no table).

die() {
  echo "dep-drift: $1" >&2
  exit 2
}

GO="${DEP_DRIFT_GO:-go}"
NPM="${DEP_DRIFT_NPM:-npm}"

ROOT="${1:-$(cd "$(dirname "$0")" && pwd)/..}"
ROOT="$(cd "$ROOT" 2>/dev/null && pwd)" || die "repo root not found: ${1:-$PWD}"
cd "$ROOT" || die "cannot enter $ROOT"
WEBUI="${DEP_DRIFT_WEBUI:-$ROOT/webui}"

# --- version math -----------------------------------------------------------

# vparts <version>: sets VS_MAJOR/VS_MINOR/VS_PATCH/VS_EXTRA. Build metadata
# (`+incompatible`) is not precedence in semver and is dropped with the leading
# v; the prerelease tail (a `-rc.1`, a pseudo-version's `20261111200114-fac...`)
# lands in VS_EXTRA so an equal core can still be told apart.
vparts() {
  local v=${1#v} extra=""
  v=${v%%+*}
  case "$v" in
    *-*) extra=${v#*-}; v=${v%%-*} ;;
  esac
  local a="" b="" c=""
  IFS='.' read -r a b c <<<"$v"
  case "$a" in '' | *[!0-9]*) a=0 ;; esac
  case "$b" in '' | *[!0-9]*) b=0 ;; esac
  case "$c" in '' | *[!0-9]*) c=0 ;; esac
  VS_MAJOR=$a
  VS_MINOR=$b
  VS_PATCH=$c
  VS_EXTRA=$extra
}

# delta_class <current> <latest>: MAJOR / MINOR / PATCH / PSEUDO / SAME.
#
# SAME covers "not ahead" as well as "identical": an upstream older or equal (a
# pin ahead of the registry, a prerelease of a core we do not run) is not
# drift, and a row claiming otherwise would be a lie the nightly repeats
# forever.
delta_class() {
  local cur=$1 lat=$2
  vparts "$cur"
  local c1=$VS_MAJOR c2=$VS_MINOR c3=$VS_PATCH ce=$VS_EXTRA
  vparts "$lat"
  local l1=$VS_MAJOR l2=$VS_MINOR l3=$VS_PATCH le=$VS_EXTRA
  if [ "$l1" -ne "$c1" ]; then [ "$l1" -gt "$c1" ] && echo MAJOR || echo SAME; return; fi
  if [ "$l2" -ne "$c2" ]; then [ "$l2" -gt "$c2" ] && echo MINOR || echo SAME; return; fi
  if [ "$l3" -ne "$c3" ]; then [ "$l3" -gt "$c3" ] && echo PATCH || echo SAME; return; fi
  [ "$le" = "$ce" ] && { echo SAME; return; }
  # Same core, upstream carries no prerelease tail: we were on a prerelease and
  # upstream shipped its release. Nothing breaks; it is still a move forward.
  [ -z "$le" ] && { echo PATCH; return; }
  # Same core, upstream carries a newer tail: a pseudo-version revision bump.
  echo PSEUDO
}

# --- rows -------------------------------------------------------------------

SRC=()
NAME=()
DECL=()
LAT=()
DELTA=()
KIND=()

add_drift() { # <source> <name> <declared> <resolved> <latest> <kind>
  local class
  class="$(delta_class "$4" "$5")"
  [ "$class" = SAME ] && return 0
  SRC+=("$1")
  NAME+=("$2")
  DECL+=("$3")
  LAT+=("$5")
  DELTA+=("$class")
  KIND+=("$6")
}

# go_mod_kinds: `<module>|<direct|indirect>` for every go.mod requirement. A
# module the graph pulls in without go.mod naming it is neither, and gets
# `graph` at the call site.
go_mod_kinds() {
  awk '
    /^require[[:space:]]*\(/ { blk = 1; next }
    blk && /^\\)/ { blk = 0; next }
    {
      path = ""; ver = ""
      if (blk) { path = $1; ver = $2 }
      else if ($1 == "require") { path = $2; ver = $3 }
      if (path == "" || ver == "" || path ~ /^\/\// || path == "require") next
      print path "|" ($0 ~ /\/\/ indirect/ ? "indirect" : "direct")
    }
  ' go.mod 2>/dev/null
}

go_rows() {
  local kinds="" out
  if [ -n "${DEP_DRIFT_GO_INPUT:-}" ]; then
    [ -f "$DEP_DRIFT_GO_INPUT" ] || die "DEP_DRIFT_GO_INPUT not found: $DEP_DRIFT_GO_INPUT"
    cat "$DEP_DRIFT_GO_INPUT"
    return 0
  fi
  command -v "$GO" >/dev/null 2>&1 || die "go not found ($GO) - the module graph is unknowable"
  [ -f go.mod ] || die "no go.mod at $ROOT"
  kinds="$(go_mod_kinds)"
  out="$("$GO" list -m -u -f '{{if .Update}}{{.Path}}|{{.Version}}|{{.Update.Version}}{{end}}' all 2>&1)" || {
    die "go list -m -u all failed (no module proxy reachable?): $(printf '%s' "$out" | tail -3 | tr '\n' ' ')"
  }
  local line path kind
  while IFS= read -r line; do
    # `go list` shares its stderr with the capture (the failure message needs
    # it), so only a three-field row is a row: progress and warnings are not.
    [[ "$line" == *"|"*"|"* ]] || continue
    path="${line%%|*}"
    kind="$(printf '%s\n' "$kinds" | awk -F'|' -v p="$path" '$1 == p { print $2; exit }')"
    printf '%s|%s\n' "$line" "${kind:-graph}"
  done <<<"$out"
}

# Reads the query on stdin and prints `<name>|<declared>|<resolved>|<latest>`,
# one row per package whose upstream is ahead of what the lockfile resolves.
# node is a NATIVE binary: it gets the JSON on stdin and RELATIVE file names
# with cwd at the webui dir, so no MSYS -> Windows path translation is in play.
DEP_DRIFT_READER=$(cat <<'JS'
const fs = require('fs')
// node -e puts the positionals at argv[1]; a script FILE puts them at argv[2].
// The last two arguments are the two files either way.
const [pkgFile, lockFile] = process.argv.slice(-2)
const read = (f) => JSON.parse(fs.readFileSync(f, 'utf8'))
let outdated
try {
  outdated = JSON.parse(fs.readFileSync(0, 'utf8'))
} catch (e) {
  console.error('dep-drift: npm outdated produced no parsable JSON: ' + e.message)
  process.exit(2)
}
const pkg = read(pkgFile)
const lock = read(lockFile)
const declared = Object.assign({}, pkg.dependencies, pkg.devDependencies)
const locked = new Map()
for (const [key, entry] of Object.entries(lock.packages || {})) {
  const m = /^node_modules\/((?:@[^/]+\/)?[^/]+)$/.exec(key)
  if (m && entry.version) locked.set(m[1], entry.version)
}
for (const [name, info] of Object.entries(outdated)) {
  const latest = (info && info.latest) || ''
  if (!latest) continue
  const resolved = locked.get(name) || info.wanted || info.current || ''
  if (!resolved) continue
  process.stdout.write([name, declared[name] || '-', resolved, latest].join('|') + '\n')
}
JS
)

# npm_rows: the webui half of the table.
npm_rows() {
  local json rc=0 errf=""
  [ -d "$WEBUI" ] || die "webui directory not found: $WEBUI"
  [ -f "$WEBUI/package.json" ] || die "no package.json at $WEBUI"
  [ -f "$WEBUI/package-lock.json" ] || die "no package-lock.json at $WEBUI (resolved versions unknowable)"
  command -v node >/dev/null 2>&1 || die "node not found - the npm query cannot be read"
  if [ -n "${DEP_DRIFT_NPM_INPUT:-}" ]; then
    [ -f "$DEP_DRIFT_NPM_INPUT" ] || die "DEP_DRIFT_NPM_INPUT not found: $DEP_DRIFT_NPM_INPUT"
    json="$(cat "$DEP_DRIFT_NPM_INPUT")"
  else
    command -v "$NPM" >/dev/null 2>&1 || die "npm not found ($NPM) - the webui half would be unmeasured"
    errf="$(mktemp "${TMPDIR:-/tmp}/dep-drift-npm.XXXXXX")" || die "mktemp failed"
    # npm exits 1 when it has findings - the normal case here, not a failure.
    # Above 1 means the query itself did not run.
    json="$(cd "$WEBUI" && "$NPM" outdated --json 2>"$errf")"
    rc=$?
    if [ "$rc" -gt 1 ]; then
      json="$(tail -3 "$errf" 2>/dev/null | tr '\n' ' ')"
      rm -f "$errf"
      die "npm outdated failed (rc $rc - no registry reachable?): $json"
    fi
    rm -f "$errf"
  fi
  [ -n "$json" ] || die "npm outdated produced no output (empty JSON)"
  (cd "$WEBUI" && printf '%s' "$json" | node -e "$DEP_DRIFT_READER" package.json package-lock.json) ||
    die "the npm query could not be read (no parsable JSON)"
}

# --- measure ----------------------------------------------------------------

go_out="$(go_rows)" || exit 2
npm_out="$(npm_rows)" || exit 2

go_n=0
npm_n=0
while IFS='|' read -r path resolved latest kind; do
  [ -n "${path:-}" ] || continue
  add_drift go "$path" "$resolved" "$resolved" "$latest" "${kind:-graph}"
done <<<"$go_out"
go_n=${#NAME[@]}

while IFS='|' read -r name declared resolved latest; do
  [ -n "${name:-}" ] || continue
  add_drift webui "$name" "$declared" "$resolved" "$latest" direct
done <<<"$npm_out"
npm_n=$(( ${#NAME[@]} - go_n ))

n=${#NAME[@]}
major=0
minor=0
patch=0
pseudo=0
for d in ${DELTA[@]+"${DELTA[@]}"}; do
  case "$d" in
    MAJOR) major=$((major + 1)) ;;
    MINOR) minor=$((minor + 1)) ;;
    PATCH) patch=$((patch + 1)) ;;
    PSEUDO) pseudo=$((pseudo + 1)) ;;
  esac
done

report() {
  echo "## Dependency drift (advisory)"
  echo
  if [ "$n" -eq 0 ]; then
    echo "**Nothing behind upstream.** Every declared dependency resolves at its newest release."
  else
    echo "**$n outdated, $major major** - advisory only: this never blocks a tag and never"
    echo "fails the nightly. A major bump of a direct dependency is a decision; a patch on"
    echo "an indirect one is usually noise upstream already absorbed."
    echo
    echo "| source | module | declared | latest | delta | kind |"
    echo "|--------|--------|----------|--------|-------|------|"
    for i in "${!NAME[@]}"; do
      echo "| ${SRC[$i]} | \`${NAME[$i]}\` | ${DECL[$i]} | ${LAT[$i]} | ${DELTA[$i]} | ${KIND[$i]} |"
    done
    if [ "$major" -gt 0 ]; then
      echo
      echo "### Major bumps (breaking by semver)"
      echo
      for i in "${!NAME[@]}"; do
        [ "${DELTA[$i]}" = MAJOR ] || continue
        echo "- \`${NAME[$i]}\` ${DECL[$i]} -> ${LAT[$i]} (${SRC[$i]}, ${KIND[$i]})"
      done
    fi
  fi
  echo
  echo "Pins are separate work: a bump arrives as a card with its gate run, never as a"
  echo "drive-by in an unrelated change."
}

if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
  report >>"$GITHUB_STEP_SUMMARY"
fi

if [ "$n" -eq 0 ]; then
  echo "DEP DRIFT OK"
  exit 0
fi

echo "dep-drift: ${n} package(s) behind upstream (${go_n} go, ${npm_n} webui) - ${major} major, ${minor} minor, ${patch} patch, ${pseudo} pseudo revision"
echo
dashes() { printf '%*s' "$1" '' | tr ' ' '-'; }
printf '%-6s %-42s %-34s %-34s %-7s %s\n' "source" "module" "declared" "latest" "delta" "kind"
printf '%-6s %-42s %-34s %-34s %-7s %s\n' \
  "$(dashes 6)" "$(dashes 42)" "$(dashes 34)" "$(dashes 34)" "$(dashes 7)" "$(dashes 4)"
for i in "${!NAME[@]}"; do
  printf '%-6s %-42s %-34s %-34s %-7s %s\n' \
    "${SRC[$i]}" "${NAME[$i]}" "${DECL[$i]}" "${LAT[$i]}" "${DELTA[$i]}" "${KIND[$i]}"
done
echo
echo "DEP DRIFT: $n outdated ($major major)"
echo "::warning::DEP DRIFT ADVISORY: $n outdated ($major major) - advisory, no tag blocked"
exit 0
