#!/bin/bash
# recipe-lint.sh - the determinism contract for journey recipes.
#
# A recipe may only assert on STATE, never on elapsed time. Four gate reds in
# three nights came from the same root class (t_fdd12843): a press read
# mid-unfold (t_44f55a54), an h+4 clamp offset (113-settings-nav), a
# browser-state leak (95-terminal), a poller count inside a fixed 17s window
# (111-socket-push). Timing makes a recipe a lottery on a loaded machine: the
# numbers are right in isolation and wrong in the suite.
#
# The contract, grep-class:
#
#   1. no fixed sleep. A wait is a poll on an observable (poll_js / wait_js /
#      wait_settled / wait_booted) or a bounded poll loop. A `sleep` outside a
#      loop body is a fixed window - a duration assertion in disguise.
#   2. no fixed window in front of a read. A sleep whose next lines grade an
#      assertion counts behavior inside a window instead of after a settle
#      event (111-socket-push's 17s poller count was exactly this).
#   3. every journey cleans up. A recipe opens a client (open_app) or declares
#      it never touches a browser, so one journey's panes, viewport and
#      localStorage can never become the next journey's baseline (the
#      77-before-70 poisoning, t_81d0901b).
#
# A finding is one line: <file>:<line>: <rule>: <message>
#
# A recipe that genuinely cannot express a wait event-based justifies the ONE
# line in place, and the justification is part of the evidence log:
#
#   sleep 0.15   # recipe-lint: allow fixed-sleep - gesture pacing: the real
#                # pointer pipeline drops a move that arrives in the same task
#
# The marker is rule-scoped on purpose (`allow <rule> - <reason>`): a blanket
# "allow" is a lint suppression by another name, which AGENTS.md forbids. A
# marker with no reason is itself a finding (bare-justification), and so is a
# marker whose sleep has gone (stale-allow) - the two ways an allow rots.
#
#   allow fixed-sleep - <reason>   same line as the sleep, or the line above
#   allow no-browser  - <reason>   file scope: this recipe never opens a page
#
# Usage:
#   scripts/recipe-lint.sh                 # lint scripts/journeys/recipes/*.sh
#   scripts/recipe-lint.sh <file>...       # lint the files given
#   scripts/recipe-lint.sh --advisory ...  # report, never exit 1
#
# Exit: 0 clean, 1 findings, 2 usage (bad arg, no such file).
set -uo pipefail

LINT_DIR="$(cd "$(dirname "$0")" && pwd)"
ADVISORY=0
FILES=()
for arg in "$@"; do
  case "$arg" in
    --advisory) ADVISORY=1 ;;
    -h|--help)
      sed -n '2,50p' "$0" | sed 's/^# \{0,1\}//'
      exit 0 ;;
    -*)
      printf 'recipe-lint: unknown option: %s\n' "$arg" >&2
      printf '  usage: recipe-lint.sh [--advisory] [<file>...]\n' >&2
      exit 2 ;;
    *) FILES+=("$arg") ;;
  esac
done
if [ "${#FILES[@]}" -eq 0 ]; then
  shopt -s nullglob
  FILES=("$LINT_DIR/journeys/recipes/"*.sh)
  shopt -u nullglob
  if [ "${#FILES[@]}" -eq 0 ]; then
    printf 'recipe-lint: no recipes found under %s/journeys/recipes\n' "$LINT_DIR" >&2
    exit 2
  fi
fi
for f in "${FILES[@]}"; do
  [ -f "$f" ] || { printf 'recipe-lint: no such file: %s\n' "$f" >&2; exit 2; }
done

# The parser. awk, not grep, because three pieces of bash shape decide the
# verdict:
#   - here-doc bodies (a python block carries `for`, `if` and `done`-looking
#     words; none of them are bash)
#   - quoted strings (a `jsq '...for (const x of y)...'` probe is a JS loop,
#     and a `while` inside a probe string must not open a bash block)
#   - comments (prose about polling is not a call)
#
# LOOP NESTING separates the two sleep classes: a sleep inside a bounded
# for/while body is a POLL INTERVAL (the pattern the contract asks for), a
# sleep at statement level is a FIXED WINDOW. `done` only ever closes a loop,
# so for|while|until|select against done needs no other block bookkeeping. A
# `done` that closes nothing is reported as `unbalanced`, so a file the parser
# misreads is loud instead of silently clean.
AWK_PROG=$(cat <<'AWK'
function trim(s) { sub(/^[ \t]+/, "", s); sub(/[ \t]+$/, "", s); return s }

# unquote: drop '...' and "..." segments - a keyword inside a probe string is
# not a bash block. Bash escapers matter in both directions: inside single
# quotes a backslash is LITERAL (`'...\"...[data-x=\"y\"]...'` is the corpus
# shape - the probe's backslash must not close anything), and inside double
# quotes a backslash escapes the next character. A scanner that treats `\"`
# as a close leaves the rest of a JS probe reading as bash, and the next
# `for (` in it opens a phantom loop that swallows every later finding.
function unquote(s,   out, i, c, q) {
  out = ""; q = ""
  for (i = 1; i <= length(s); i++) {
    c = substr(s, i, 1)
    if (q == "\"") {
      if (c == "\\") { i++; continue }
      if (c == "\"") { q = ""; continue }
      continue
    }
    if (q == "'") {
      if (c == "'") q = ""
      continue
    }
    if (c == "'" || c == "\"") { q = c; continue }
    out = out c
  }
  return out
}

function emit(n, rule, msg) { printf "%s:%d: %s: %s\n", FILENAME, n, rule, msg }

{ raw[FNR] = $0 }

END {
  # --- pass 1: classify every line (comment-stripped, unquoted) ----------
  heredoc = ""
  for (n = 1; n <= FNR; n++) {
    line = raw[n]
    if (heredoc != "") {
      text[n] = ""                      # opaque: never code
      if (trim(line) == heredoc) heredoc = ""
      continue
    }
    if (match(line, /<<-?[ \t]*["\047]?[A-Za-z_][A-Za-z0-9_]*["\047]?/)) {
      d = substr(line, RSTART, RLENGTH)
      sub(/^<<-?[ \t]*/, "", d)
      gsub(/["\047]/, "", d)
      heredoc = d
      text[n] = ""
      continue
    }
    c = line
    sub(/#.*/, "", c)
    text[n] = unquote(c)
  }

  # --- pass 2: markers, loop depth, sleeps -------------------------------
  depth = 0
  nobrowser = ""
  opened = ""
  for (n = 1; n <= FNR; n++) {
    c = text[n]
    rawline = raw[n]

    # marker: `recipe-lint: allow[ -]<rule> - <reason>`
    if (match(rawline, /recipe-lint: allow[ -][A-Za-z-]+/)) {
      m = substr(rawline, RSTART, RLENGTH)
      rule = m
      sub(/^recipe-lint: allow[ -]/, "", rule)
      reason = substr(rawline, RSTART + RLENGTH)
      sub(/^[ \t]*-*[ \t]*/, "", reason)
      reason = trim(reason)
      if (length(reason) < 3)
        emit(n, "bare-justification", "`recipe-lint: allow " rule "` carries no reason - a suppression with no checked reason is what AGENTS.md forbids")
      if (rule == "no-browser") {
        if (length(reason) >= 3) nobrowser = reason
      } else if (rule == "fixed-sleep" || rule == "count-window") {
        # line-scoped: the sleep on this line, or the one below it
        tgt = (c ~ /(^|[ \t;&|(])sleep[ \t]+[0-9.]/) ? n : n + 1
        marker_rule[tgt] = rule
        marker_at[tgt] = n
        nmark++
        mark_tgt[nmark] = tgt
      }
    }

    # loop depth: openers minus closers on this line.
    # A `for ... in a b done; do` header carries a list item NAMED done, so
    # everything before the line's own `do` keyword is header (never a
    # closer) and only what follows it counts as closes. Without this split
    # one recipe's `for st in triage ... done; do` reads as a net-zero line
    # followed by a phantom closer, and every later finding in the file is
    # swallowed by a negative depth.
    header = c; rest = c
    if (match(c, /(^|[ \t;])do([ \t;&|]|$)/)) {
      header = substr(c, 1, RSTART - 1)
      rest = substr(c, RSTART, length(c))
    }
    tmp = header; nopen = 0
    while (match(tmp, /(^|[ \t;&|(])(while|until|for|select)[ \t(]/)) {
      nopen++
      tmp = substr(tmp, RSTART + RLENGTH)
    }
    tmp = rest; nclose = 0
    while (match(tmp, /(^|[ \t;&|)])(done)([ \t;&|)]|$)/)) {
      nclose++
      tmp = substr(tmp, RSTART + RLENGTH)
    }
    depth += nopen
    if (depth - nclose < 0)
      emit(n, "unbalanced", "a `done` closes no open loop - the parser cannot read this file, so its sleeps were never judged")
    inloop[n] = (depth - nclose) > 0 || (nopen > 0 && nclose > 0)
    depth -= nclose

    # the cleanup contract: a client opener, or a declared browserless recipe
    if (opened == "" && c ~ /(^|[ \t;&|(])(open_app|journey_clean_room)([ \t;&|)]|$)/) opened = n

    # a sleep at loop level 0 is a fixed window
    if (c ~ /(^|[ \t;&|(])sleep[ \t]+[0-9.]/ && !inloop[n]) {
      if (n in marker_rule) { used[n] = 1; continue }
      grade = ""
      k = n; seen = 0
      while (k < FNR && seen < 8) {
        k++
        if (text[k] == "") continue
        seen++
        if (text[k] ~ /assert_(eq|ne)|(^|[ \t;&|(])die[ \t]|grep -c|wc -l|\.length|_count/) { grade = "read"; break }
      }
      if (grade == "read")
        emit(n, "count-window", "a fixed window grades a read - count after a settle event (poll_js / wait_js / wait_settled), never after a sleep")
      else
        emit(n, "fixed-sleep", "a fixed sleep - wait on an observable (poll_js / wait_js / wait_settled) or justify it in place")
    }
  }

  if (opened == "" && nobrowser == "")
    emit(1, "no-cleanup", "no open_app / journey_clean_room call and no `recipe-lint: allow no-browser - <reason>` - a journey that cleans nothing leaves its state to the next one")

  # a marker whose sleep has gone: the justification outlived its line
  for (i = 1; i <= nmark; i++) {
    tgt = mark_tgt[i]
    if (!(tgt in used) && tgt <= FNR)
      emit(tgt, "stale-allow", "`recipe-lint: allow " marker_rule[tgt] "` justifies a line that carries no fixed sleep (marker at line " marker_at[tgt] ")")
  }
}
AWK
)

findings=0
for f in "${FILES[@]}"; do
  out="$(awk "$AWK_PROG" "$f")"
  if [ -n "$out" ]; then
    printf '%s\n' "$out"
    findings=$((findings + $(printf '%s\n' "$out" | grep -c .)))
  fi
done

if [ "$findings" -gt 0 ]; then
  if [ "$ADVISORY" = "1" ]; then
    printf 'recipe-lint: %s finding(s) (advisory - not failing)\n' "$findings" >&2
    exit 0
  fi
  printf 'recipe-lint: %s finding(s)\n' "$findings" >&2
  exit 1
fi
exit 0
