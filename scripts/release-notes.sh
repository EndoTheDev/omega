#!/usr/bin/env bash
set -euo pipefail

# release-notes.sh - the release story: a hook, user-facing bullets, counts.
#
# The post is a release NOTE, not a changelog dump (Endo ruling 2026-11-10,
# card t_878b1b7c), and it speaks to TWO audiences (card t_42854d3e): a NEW
# user has to read it and be interested, an EXISTING user has to understand
# what is new or updated. Factory vocabulary - tuner-round phrases, internal
# mechanism names, conventional-commit prefixes - serves neither.
#
#   🚀 **omega v0.17.0** is out!
#
#   <hook: one sentence, plain words, the theme and why it matters>
#
#   - <bullet: user-observable behavior, never the internal mechanism>
#
#   26 features, 2 refactors, 16 fixes, 1 tune, 14 doc updates, 12 merges ...
#
#   Update: `omega update` · full changelog: <release link>
#
# The rewrite rule, applied per bullet: would someone who never worked on this
# codebase understand it without asking? Rendering does what it can (the
# "feat(scope):" prefix is factory vocabulary and never posts; card pointers
# are stripped), but no regex turns a commit subject into behavior text - the
# per-release override file carries the hand-written prose (next paragraph).
#
# Style law: no em-dashes and no en-dashes anywhere in generated text, plain
# hyphens only (AGENTS.md style). Every generated line passes through dash_fix
# before it is written.
#
# Overrides (scripts/release-notes.overrides.md, optional, human-written):
#
#   ## v0.17.3
#   hook: The make-over: the web ui's biggest visual and interaction refresh yet.
#   - the command palette now edits the top bar's clock field right where it lives
#   - the agents tab now works like sessions do
#
# A "## <version>" heading opens a section; "hook: <text>" sets that release's
# hook and "- " / "* " lines are its bullets. Text outside a matching section is
# ignored, so the file accumulates one section per release. The override WINS
# over the tag annotation for the post's hook and bullets; the stats line and
# the GitHub body's changelog are untouched by it. A missing file, or a file
# with no section for this version, is not an error: the annotation carries the
# post, and with no annotation either a plain counts-derived hook is generated.
# NOTES_OVERRIDES points the script at a different file (tests do this).
#
# The GitHub release body keeps the FULL per-class bullet changelog - the
# record lives there, the post is the pitch, one click away. The bullet wall
# was the defect: uncapped headline classes overflowed both the house law's
# ~20-bullet ceiling and Discord's 2000-char cap, and a post that lists every
# commit is not a post anyone reads.
#
# Deterministic, no LLM, same public-text class as the asset list it joins:
# counts and highlights parse the round's commit log (the commit-tone law
# guarantees informative subjects; the range + record-parsing approach is
# shared with scripts/release-suggest.sh).
#
# Hook and bullets: the annotation's body carries the hook paragraph and an
# optional highlight list (lines starting with "- " or "* "). The
# five-highlight law caps that list - extras are named on stderr (never
# silently dropped) and never posted. The annotation's subject is its title and
# is not posted; when the body carries no prose the subject IS the hook (a
# one-line `git tag -a -m ...`). With no annotation at all the hook is
# generated from the class counts and named on stderr - never a blank line,
# never an invented behavior claim ("the tag has no hook" is a curation gap,
# the counts sentence is the honest floor).
#
# The annotation is read off the LOCAL tag OBJECT. A CI checkout can leave
# refs/tags/<v> pointing at the bare commit - actions/checkout@v4's targeted
# fallback fetch (`+<sha>:refs/tags/<v>`) rewrites the ref whenever
# github.sha, the peeled commit, does not equal the tag OBJECT sha. The
# annotation is then unreadable and the post ships bare, which is what
# happened to v0.17.1 (t_530c4635): the workflow restores the tag object
# before this script runs, and the stderr note below names the shape
# whenever an annotated tag arrives without one.
#
# The annotation IS the approval moment: nothing unapproved is generated at
# post time, and the post is public text. The tag is the only human input.
#
# GitHub body (release-notes-body.md): the per-class changelog, unchanged -
# classes run feat -> refactor -> fix -> tune -> docs, newest first inside
# each; feat and refactor are the headline classes and list complete, fix,
# tune and docs take the newest 5 each (t_dd6278c4). Internal tooling never
# highlights: test:/chore: subjects, and journeys/test-scoped fixes,
# refactors or docs (a user does not care that a journey recipe brace was
# dropped). wip/style/embed-lane noise and bare "Merge branch" lines never
# highlight or count. The counts line lists the reader-meaningful classes -
# features, refactors, fixes, tunes, doc updates - then merges and distinct
# scopes; tests and chores stay folded (never a segment). Empty segments
# still print so the line's shape holds between releases; scopes stay
# newest-touched first, capped at 6 so it stays one line a human reads.
#
# Discord backstop: the payload is capped at 2000 chars. The condensed post
# lands around 700, so 2000 chars means a broken annotation - no JSON is
# written and the script exits 1: a release does not announce text nobody
# can read. Fix the tag and re-run. (The old trimmer ladder is dead: measured
# on a fat cycle it saved 133 chars against a 608-char overflow, because the
# weight sits in the feature class.)
#
# Usage:
#   scripts/release-notes.sh [base-ref] [version-tag]
#     base    default: newest stable v* tag that is not the version tag
#     version default: tag pointing at HEAD, else "unreleased"
#
# Writes (dir created; override with NOTES_OUT_DIR):
#   release-notes.md       the post (also printed to stdout) - the Discord text
#   release-notes-body.md  the GitHub release body: prose + counts + the full
#                          per-class changelog
#   discord-release.json   {"content","username"} payload - the webhook step
#                          posts with curl -d @file, never an inline -d string
#
# Exit codes: 2 unusable input (not a git repo / no base found); 1 when the
# post is over the 2000-char Discord cap (the notes and the body are still
# written; no payload, so nothing posts).

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if ! git rev-parse --git-dir >/dev/null 2>&1; then
  echo "release-notes: not a git repo" >&2
  exit 2
fi

VERSION="${2:-}"
if [ -z "$VERSION" ]; then
  VERSION="$(git describe --tags --exact-match HEAD 2>/dev/null || echo unreleased)"
fi

if [ -n "${1:-}" ]; then
  BASE="$1"
else
  # Same stable-tag pick as release-suggest.sh, minus the version tag
  # itself (at release time the new tag points at HEAD).
  BASE="$(git tag -l 'v*' | grep -v -- '-' | grep -vx -- "$VERSION" | sort -V | tail -1)"
  if [ -z "$BASE" ]; then
    echo "release-notes: no stable v* tag found - pass a base ref" >&2
    exit 2
  fi
fi

# --- the annotation ---------------------------------------------------------
# Read the WHOLE tag message, not just its subject line: the subject is the
# title, the body carries the prose paragraph and (optionally) the highlight
# list. Only an annotated tag has one - `^{tag}` fails to peel a lightweight
# tag. Nothing is invented here; the annotation is Endo's word on the post.
TAG_SUBJ="" TAG_BODY=""
if [ "$VERSION" != "unreleased" ] \
   && git rev-parse -q --verify "refs/tags/$VERSION" >/dev/null 2>&1 \
   && git rev-parse -q --verify "refs/tags/$VERSION^{tag}" >/dev/null 2>&1; then
  TAG_SUBJ="$(git for-each-ref "refs/tags/$VERSION" --format='%(contents:subject)')"
  TAG_BODY="$(git for-each-ref "refs/tags/$VERSION" --format='%(contents:body)')"
elif [ "$VERSION" != "unreleased" ] \
   && git rev-parse -q --verify "refs/tags/$VERSION" >/dev/null 2>&1; then
  # The tag exists but is not a tag OBJECT here: either a genuinely
  # lightweight tag (no prose by design) or an annotated tag whose ref was
  # rewritten to the commit by a CI checkout. Nothing is invented - but the
  # absence is not silent, and the remedy is named (v0.17.1 lost its
  # paragraph to exactly this and nobody saw a word about it).
  printf 'release-notes: refs/tags/%s carries no readable annotation - the post has no prose paragraph (a lightweight tag has none; if the tag IS annotated, restore the tag object: git fetch --force origin refs/tags/%s:refs/tags/%s)\n' \
    "$VERSION" "$VERSION" "$VERSION" >&2
fi

ltrim() { local s="$1"; printf '%s' "${s#"${s%%[![:space:]]*}"}"; }
rtrim() { local s="$1"; printf '%s' "${s%"${s##*[![:space:]]}"}"; }
# one paragraph: every run of whitespace collapses to a single space
oneline() { printf '%s' "$1" | tr -s '[:space:]' ' ' | sed -e 's/^ //' -e 's/ $//'; }

ANN_PROSE=""    # the prose paragraph (empty = no annotation prose)
ANN_ITEMS=()    # the annotation's highlight list (empty = default pick)
if [ -n "$TAG_BODY" ]; then
  while IFS= read -r raw; do
    line="$(rtrim "$(ltrim "$raw")")"
    [ -n "$line" ] || continue
    case "$line" in
      '- '* | '* '*) ANN_ITEMS+=("$(rtrim "${line:2}")") ;;
      *) ANN_PROSE="$ANN_PROSE $line" ;;
    esac
  done <<< "$TAG_BODY"
  ANN_PROSE="$(oneline "$ANN_PROSE")"
fi
# A one-line `tag -a -m "..."` puts the whole message in the subject: that IS
# the prose. A subject-only title over a prose body stays a title.
if [ -z "$ANN_PROSE" ] && [ -n "$TAG_SUBJ" ]; then
  ANN_PROSE="$(oneline "$TAG_SUBJ")"
fi

# --- one pass over the range (same parsing shape as release-suggest.sh) ----
re_feat='^feat(\(|:| )'
re_fix='^fix(\(|:| )'
re_tune='^tune(\(|:| )'
re_refactor='^refactor(\(|:| )'
re_docs='^docs(\(|:| )'
re_scope='^[a-z]+\(([^)]+)\):'

n_feat=0 n_refactor=0 n_fix=0 n_tune=0 n_docs=0 n_merge=0
fc_disp=() rf_disp=() fx_disp=() tn_disp=() dc_disp=()
scopes=() scope_seen=""

strip_id() { # a trailing " (t_xxxx)" card pointer is noise in a public line
  printf '%s' "$1" | sed -E 's/ ?\(t_[0-9a-f]+\)$//'
}

hl_excluded() { # internal-tooling scopes never highlight (fix(journeys), fix(test), the like); comma parts match exactly
  local sc=";${1//,/;};"
  [[ "$sc" == *';journeys;'* || "$sc" == *';test;'* ]]
}

while IFS= read -r -d $'\x1e' rec; do
  rec="${rec##$'\n'}"   # records after the first start with git's separator newline
  rec="${rec%$'\n'}"
  [ -n "$rec" ] || continue
  rest="${rec#*$'\x1f'}"
  par="${rest%%$'\x1f'*}"
  subj="${rest#*$'\x1f'}"
  low="${subj,,}"
  if [[ "$par" == *\ * ]]; then
    n_merge=$((n_merge + 1))
    continue
  fi
  scope=""
  if [[ "$low" =~ $re_scope ]]; then
    scope="${BASH_REMATCH[1]}"
    scope="${scope,,}"
  fi
  disp="$(strip_id "$subj")"
  keep=0
  if ! hl_excluded "$scope"; then keep=1; fi
  if [[ "$low" =~ $re_feat ]]; then
    n_feat=$((n_feat + 1))
    [ "$keep" -eq 1 ] && fc_disp+=("$disp")
  elif [[ "$low" =~ $re_refactor ]]; then
    n_refactor=$((n_refactor + 1))
    [ "$keep" -eq 1 ] && rf_disp+=("$disp")
  elif [[ "$low" =~ $re_fix ]]; then
    n_fix=$((n_fix + 1))
    [ "$keep" -eq 1 ] && fx_disp+=("$disp")
  elif [[ "$low" =~ $re_tune ]]; then
    n_tune=$((n_tune + 1))
    [ "$keep" -eq 1 ] && tn_disp+=("$disp")
  elif [[ "$low" =~ $re_docs ]]; then
    n_docs=$((n_docs + 1))
    [ "$keep" -eq 1 ] && dc_disp+=("$disp")
  fi
  # tests, chores and the rest stay folded: the changelog is the record
  if [ -n "$scope" ] && [[ ";$scope_seen;" != *";$scope;"* ]]; then
    scopes+=("$scope")
    scope_seen="$scope_seen;$scope"
  fi
done < <(git log --format='%h%x1f%p%x1f%s%x1e' "${BASE}..HEAD")

# --- the counts line --------------------------------------------------------
plural() { if [ "$1" -eq 1 ]; then echo ""; else echo "s"; fi; }
plural_fix() { if [ "$1" -eq 1 ]; then echo ""; else echo "es"; fi; } # fx -> fixes

counts="$n_feat feature$(plural "$n_feat"), $n_refactor refactor$(plural "$n_refactor"), $n_fix fix$(plural_fix "$n_fix"), $n_tune tune$(plural "$n_tune"), $n_docs doc update$(plural "$n_docs")"
if [ "$n_merge" -gt 0 ]; then counts="$counts, $n_merge merge$(plural "$n_merge")"; fi
if [ "${#scopes[@]}" -gt 0 ]; then
  SCOPE_CAP=6
  [ "${#scopes[@]}" -lt "$SCOPE_CAP" ] && SCOPE_CAP="${#scopes[@]}"
  sjoin="$(printf '%s, ' "${scopes[@]:0:$SCOPE_CAP}")"
  counts="$counts across ${sjoin%, }"
fi

# --- the two-audience template ---------------------------------------------
# Style law: plain hyphens only. A generated line never carries an em- or an
# en-dash, whatever the annotation, the override file or a commit subject
# arrived with - the writer's keyboard is not the reader's problem.
dash_fix() { printf '%s' "$1" | sed -e 's/—/-/g' -e 's/–/-/g'; }

# A post bullet is user-facing text. The conventional-commit prefix
# (feat(webui):, fix:, ...) is factory vocabulary: the record keeps the full
# subject, the post drops the prefix. The body of the subject stays verbatim -
# a regex cannot rewrite a mechanism into a behavior, and pretending it can
# would invent claims.
re_cc_prefix='^(feat|fix|tune|refactor|docs|test|chore)(\([^)]*\))?:[[:space:]]*'
user_line() {
  local s="$1"
  if [[ "${s,,}" =~ $re_cc_prefix ]]; then
    s="${s:${#BASH_REMATCH[0]}}"
  fi
  dash_fix "$s"
}

# --- the override file ------------------------------------------------------
# scripts/release-notes.overrides.md is the human's word on the post's prose:
# one "## <version>" section per release, "hook: <sentence>" plus "- " bullets.
# A missing file, a missing section for this version and a section without a
# hook are all fallbacks, never errors - the post always renders.
OVERRIDES="${NOTES_OVERRIDES:-$SCRIPT_DIR/release-notes.overrides.md}"
OV_HOOK=""
OV_ITEMS=()
if [ -f "$OVERRIDES" ]; then
  ov_in_section=0
  while IFS= read -r raw || [ -n "$raw" ]; do
    line="$(rtrim "$(ltrim "$raw")")"
    if [[ "$line" =~ ^#+[[:space:]]*(.+)$ ]]; then
      # a heading opens a section only when it names THIS version
      head="$(rtrim "${BASH_REMATCH[1]}")"
      if [ "$head" = "$VERSION" ]; then ov_in_section=1; else ov_in_section=0; fi
      continue
    fi
    [ "$ov_in_section" -eq 1 ] || continue
    case "$line" in
      '') ;;
      hook:*) OV_HOOK="$(oneline "${line#hook:}")" ;;
      '- '* | '* '*) OV_ITEMS+=("$(rtrim "${line:2}")") ;;
    esac
  done < "$OVERRIDES"
fi

# --- the hook ---------------------------------------------------------------
# Precedence: the override's hand-written sentence, the annotation's paragraph,
# then a plain counts sentence. The last is a floor, not a pitch - the script
# cannot invent behavior text, so it names what shipped and says so on stderr.
default_hook() {
  local feats="$n_feat new feature$(plural "$n_feat")"
  local fixes="$n_fix bug fix$(plural_fix "$n_fix")"
  if [ "$n_feat" -gt 0 ] && [ "$n_fix" -gt 0 ]; then
    printf 'This update brings %s and %s.' "$feats" "$fixes"
  elif [ "$n_feat" -gt 0 ]; then
    printf 'This update brings %s.' "$feats"
  elif [ "$n_fix" -gt 0 ]; then
    printf 'This update is %s, no new features.' "$fixes"
  else
    printf 'A maintenance round: refactors, tuning and docs, no new features.'
  fi
}

if [ -n "$OV_HOOK" ]; then
  HOOK="$(dash_fix "$OV_HOOK")"
elif [ -n "$ANN_PROSE" ]; then
  HOOK="$(dash_fix "$ANN_PROSE")"
else
  HOOK="$(default_hook)"
  printf 'release-notes: no override section and no tag annotation for %s - the hook is generated from the class counts (write one in %s)\n' \
    "$VERSION" "$OVERRIDES" >&2
fi

# --- the GitHub body's pick -------------------------------------------------
# Ordered classes, each newest-first. feat and refactor are the headline
# classes: the range's story, listed complete. fix, tune and docs are the
# tail: the newest CLASS_CAP subjects each. No per-scope dedupe - a class
# shows what shipped, not one line per topic. This pick feeds the release
# body (the record) only: the post carries five highlights.
CLASS_CAP=5
hl=()

# headline classes: complete
for disp in "${fc_disp[@]}"; do hl+=("$disp"); done
for disp in "${rf_disp[@]}"; do hl+=("$disp"); done

# tail classes: newest-first, capped
for pair in fx:fix tn:tune dc:docs; do
  declare -n box="${pair%%:*}_disp"
  n=0
  for disp in "${box[@]}"; do
    [ "$n" -lt "$CLASS_CAP" ] || break
    hl+=("$disp")
    n=$((n + 1))
  done
  unset -n box
done

# --- the post's five highlights ---------------------------------------------
# The override's list wins, then the annotation's own list (ruling 3), then the
# newest 5 feat subjects. The post law is five either way - anything beyond is
# named on stderr, never dropped quietly. Rendering into user-facing text is
# user_line's job at print time.
HL_MAX=5
post_hl=()
src_items=()
src_label=""
if [ "${#OV_ITEMS[@]}" -gt 0 ]; then
  src_items=("${OV_ITEMS[@]}")
  src_label="override"
elif [ "${#ANN_ITEMS[@]}" -gt 0 ]; then
  src_items=("${ANN_ITEMS[@]}")
  src_label="annotation"
fi
if [ "${#src_items[@]}" -gt 0 ]; then
  n=0
  for item in "${src_items[@]}"; do
    if [ "$n" -lt "$HL_MAX" ]; then
      post_hl+=("$item")
    else
      printf 'release-notes: %s lists %d highlights - the post keeps the first %d (five-highlight law); not posted: %s\n' \
        "$src_label" "${#src_items[@]}" "$HL_MAX" "$item" >&2
    fi
    n=$((n + 1))
  done
else
  n=0
  for disp in "${fc_disp[@]}"; do
    [ "$n" -lt "$HL_MAX" ] || break
    post_hl+=("$disp")
    n=$((n + 1))
  done
fi

# --- assembly ---------------------------------------------------------------
OUT_DIR="${NOTES_OUT_DIR:-dist}"
mkdir -p "$OUT_DIR"
MD="$OUT_DIR/release-notes.md"
BODY="$OUT_DIR/release-notes-body.md"
JSON="$OUT_DIR/discord-release.json"

# the post: header, hook, user-facing bullets, counts, update + changelog link
{
  printf '🚀 **omega %s** is out!\n' "$VERSION"
  printf '\n%s\n' "$HOOK"
  if [ "${#post_hl[@]}" -gt 0 ]; then
    printf '\n'
    for line in "${post_hl[@]}"; do printf -- '- %s\n' "$(user_line "$line")"; done
  fi
  printf '\n%s\n' "$counts"
  if [ "$VERSION" != "unreleased" ]; then
    printf '\nUpdate: `omega update` · full changelog: <%s/releases/tag/%s>\n' \
      "${RELEASE_REPO_URL:-https://github.com/EndoTheDev/omega}" "$VERSION"
  fi
} > "$MD"

# the GitHub body: the record - the release paragraph, counts, every per-class
# pick bullet. Subjects stay verbatim (the record IS the commit log) apart from
# the style law's dashes. A hand-written hook heads the record too: it is the
# release's own description, not post decoration.
BODY_PROSE="$ANN_PROSE"
if [ -n "$OV_HOOK" ]; then BODY_PROSE="$OV_HOOK"; fi
{
  if [ -n "$BODY_PROSE" ]; then printf '%s\n\n' "$(dash_fix "$BODY_PROSE")"; fi
  printf '%s\n' "$counts"
  for line in "${hl[@]}"; do printf -- '- %s\n' "$(dash_fix "$line")"; done
} > "$BODY"

# --- the Discord payload ----------------------------------------------------
# A previous run's payload must never be posted by mistake: the guard either
# writes a fresh JSON or nothing at all.
rm -f -- "$JSON"
python3 - "$MD" "$JSON" <<'PY'
import json, sys

md_path, json_path = sys.argv[1:3]
LIMIT = 2000

with open(md_path, encoding="utf-8") as f:
    content = f.read()

if len(content) > LIMIT:
    sys.stderr.write(
        "release-notes: Discord post is %d chars, over the %d-char cap - the "
        "tag annotation is too long; no payload written, fix the tag and "
        "re-run (the post is at %s)\n"
        % (len(content), LIMIT, md_path))
    sys.exit(1)

with open(json_path, "w", encoding="utf-8") as f:
    json.dump({"content": content, "username": "omega releases"},
              f, ensure_ascii=False, indent=2)
    f.write("\n")
PY

cat "$MD"
