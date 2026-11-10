#!/usr/bin/env bash
set -euo pipefail

# release-notes.sh - the release story: five highlights, one paragraph, counts.
#
# The post is a release NOTE, not a changelog dump (Endo ruling 2026-11-10,
# card t_878b1b7c):
#
#   🚀 **omega v0.17.0** is out!
#
#   <one prose paragraph>
#
#   - <five highlight bullets>
#
#   26 features, 2 refactors, 16 fixes, 1 tune, 14 doc updates, 12 merges ...
#
#   Update: `omega update` · full changelog: <release link>
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
# Highlights: five, and five only. Default = the newest 5 feat subjects of
# the range. The ANNOTATED tag may override them with a short list (body
# lines starting with "- " or "* "); curation is optional, never mandatory.
# The five-highlight law caps that list too - extras are named on stderr
# (never silently dropped) and never posted.
#
# Prose: the annotation's body, word for word, as ONE paragraph (line wraps
# and paragraph breaks collapse to single spaces; no wording is changed).
# The annotation's subject is its title and is not posted; when the body
# carries no prose the subject IS the prose (a one-line `git tag -a -m ...`).
# A tag with no annotation gets no prose paragraph - the post is highlights +
# counts + link, never a blank paragraph.
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
# Default: the newest 5 feat subjects. The annotation's own list wins
# outright (ruling 3), but the post law is five - anything beyond is named
# on stderr, never dropped quietly.
HL_MAX=5
post_hl=()
if [ "${#ANN_ITEMS[@]}" -gt 0 ]; then
  n=0
  for item in "${ANN_ITEMS[@]}"; do
    if [ "$n" -lt "$HL_MAX" ]; then
      post_hl+=("$item")
    else
      printf 'release-notes: annotation lists %d highlights - the post keeps the first %d (five-highlight law); not posted: %s\n' \
        "${#ANN_ITEMS[@]}" "$HL_MAX" "$item" >&2
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

# the post: header, prose, five highlights, counts, update + changelog link
{
  printf '🚀 **omega %s** is out!\n' "$VERSION"
  if [ -n "$ANN_PROSE" ]; then printf '\n%s\n' "$ANN_PROSE"; fi
  if [ "${#post_hl[@]}" -gt 0 ]; then
    printf '\n'
    for line in "${post_hl[@]}"; do printf -- '- %s\n' "$line"; done
  fi
  printf '\n%s\n' "$counts"
  if [ "$VERSION" != "unreleased" ]; then
    printf '\nUpdate: `omega update` · full changelog: <%s/releases/tag/%s>\n' \
      "${RELEASE_REPO_URL:-https://github.com/EndoTheDev/omega}" "$VERSION"
  fi
} > "$MD"

# the GitHub body: the record - prose, counts, every per-class pick bullet
{
  if [ -n "$ANN_PROSE" ]; then printf '%s\n\n' "$ANN_PROSE"; fi
  printf '%s\n' "$counts"
  for line in "${hl[@]}"; do printf -- '- %s\n' "$line"; done
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
