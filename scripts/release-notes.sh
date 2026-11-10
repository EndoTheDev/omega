#!/usr/bin/env bash
set -euo pipefail

# release-notes.sh - the release story: counts, top highlights, payloads.
#
# Deterministic, no LLM, same public-text class as the asset list it joins:
# counts and top highlights parse the round's commit log (the commit-tone
# law guarantees informative subjects; the range + record-parsing approach
# is shared with scripts/release-suggest.sh). The one prose summary line
# rides the ANNOTATED tag message, read off the tag at release time - a tag
# without an annotation falls back to counts + highlights only. Nothing
# unapproved is ever generated at post time; the tag IS the approval moment.
#
# Highlight pick (per-class cap, no ranking cleverness - Endo ruling
# 2026-11-09, card t_dd6278c4): the classes run feat -> refactor -> fix ->
# tune -> docs, and within a class the subjects run newest first. feat and
# refactor are the headline classes and list complete; fix, tune and docs
# take the newest 5 each (a busy cycle lands around 19 bullets, inside the
# house law's "under ~20" ceiling). The old total cap of 5 and the per-scope
# dedupe are both gone.
#   Internal tooling never highlights: test:/chore: subjects, and
#   journeys/test-scoped fixes, refactors or docs (a user does not care that
#   a journey recipe brace was dropped). wip/style/embed-lane noise and bare
#   "Merge branch" lines never highlight or count. The full changelog keeps
#   every commit - the bullets are a selection, the changelog is the record.
#   The counts line lists the reader-meaningful classes - features,
#   refactors, fixes, tunes, doc updates - then merges and distinct
#   scopes; tests and chores stay folded (never a segment). Empty
#   segments still print so the line's shape holds between releases;
#   scopes stay newest-touched first, capped at 6 so it stays one
#   line a human reads.
#
# Discord length ladder (Endo ruling 2026-11-10, card t_878b1b7c): the full
# notes above are the record and never shrink - GitHub gets every bullet.
# The Discord payload is capped at 2000 chars, so a fat cycle trims trailing
# bullets from the lowest-signal classes (docs first, then tune), oldest
# first, until the post fits. The counts line always reports the TRUE totals
# however many bullets were trimmed. If the post still does not fit with
# every trimmable bullet gone, no JSON is written: the full notes stand,
# Discord is skipped, and dist/discord-exception.txt carries the reason plus
# the full content for a hand-written post. Never a silent drop.
#
# Usage:
#   scripts/release-notes.sh [base-ref] [version-tag]
#     base    default: newest stable v* tag that is not the version tag
#     version default: tag pointing at HEAD, else "unreleased"
#
# Writes (dir created; override with NOTES_OUT_DIR):
#   release-notes.md       the full post content (also printed to stdout)
#   release-notes-body.md  lead sentence + counts + highlights (release body)
#   discord-release.json   {"content","username"} payload - the webhook step
#                          posts with curl -d @file, never an inline -d string
#   discord-exception.txt  only when the post cannot fit even after trimming:
#                          why the JSON is absent, plus the full content
#
# Exit codes: 2 unusable input (not a git repo / no base found); 1 when the
# Discord payload cannot fit even after trimming (the full notes are still
# written - the release body is complete, only Discord is skipped loudly).

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

# The human lead line: the subject of the annotated tag message. Only an
# annotated tag carries one - `^{tag}` fails to peel a lightweight tag.
ANN=""
if [ "$VERSION" != "unreleased" ] \
   && git rev-parse -q --verify "refs/tags/$VERSION" >/dev/null 2>&1 \
   && git rev-parse -q --verify "refs/tags/$VERSION^{tag}" >/dev/null 2>&1; then
  ANN="$(git for-each-ref "refs/tags/$VERSION" --format='%(contents:subject)' | sed -n '1p')"
  ANN="$(printf '%s' "$ANN" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
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

strip_id() { # a trailing "(t_xxxx)" card pointer is noise in a public line
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

# --- the pick ---------------------------------------------------------------
# Ordered classes, each newest-first. feat and refactor are the headline
# classes: the range's story, listed complete. fix, tune and docs are the
# tail: the newest CLASS_CAP subjects each. No per-scope dedupe - a class
# shows what shipped, not one line per topic. hl_cls rides hl so the Discord
# ladder knows which bullets are trimmable.
CLASS_CAP=5
hl=() hl_cls=()

# headline classes: complete
for disp in "${fc_disp[@]}"; do hl+=("$disp"); hl_cls+=("feat"); done
for disp in "${rf_disp[@]}"; do hl+=("$disp"); hl_cls+=("refactor"); done

# tail classes: newest-first, capped
for pair in fx:fix tn:tune dc:docs; do
  declare -n box="${pair%%:*}_disp"
  n=0
  for disp in "${box[@]}"; do
    [ "$n" -lt "$CLASS_CAP" ] || break
    hl+=("$disp")
    hl_cls+=("${pair##*:}")
    n=$((n + 1))
  done
  unset -n box
done

# --- assembly ----------------------------------------------------------------
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

OUT_DIR="${NOTES_OUT_DIR:-dist}"
mkdir -p "$OUT_DIR"
MD="$OUT_DIR/release-notes.md"
BODY="$OUT_DIR/release-notes-body.md"
JSON="$OUT_DIR/discord-release.json"

{
  printf '🚀 **omega %s** is out!\n' "$VERSION"
  if [ -n "$ANN" ]; then printf '\n%s\n' "$ANN"; fi
  printf '\n%s\n' "$counts"
  if [ "${#hl[@]}" -gt 0 ]; then
    printf '\n'
    for line in "${hl[@]}"; do printf -- '- %s\n' "$line"; done
  fi
  if [ "$VERSION" != "unreleased" ]; then
    printf '\nUpdate: `omega update`\n<%s/releases/tag/%s>\n' \
      "${RELEASE_REPO_URL:-https://github.com/EndoTheDev/omega}" "$VERSION"
  fi
} > "$MD"

{
  if [ -n "$ANN" ]; then printf '%s\n\n' "$ANN"; fi
  printf '%s\n' "$counts"
  for line in "${hl[@]}"; do printf -- '- %s\n' "$line"; done
} > "$BODY"

# --- the Discord ladder ------------------------------------------------------
# Build the trim order: walk the pick from the END (the tail classes always
# sit last), keeping only tune/docs bullets - docs is the lowest-signal
# class and lands last, so it trims first, then tune, oldest first inside
# each class. feat/refactor/fix bullets are never trimmable: the headline
# story and the fix list are the post, not filler.
TRIM_LIST="$OUT_DIR/.discord-trim"
: > "$TRIM_LIST"
for ((i = ${#hl[@]} - 1; i >= 0; i--)); do
  case "${hl_cls[$i]}" in
    docs | tune) printf -- '- %s\n' "${hl[$i]}" >> "$TRIM_LIST" ;;
  esac
done
DISCORD_EXCEPTION="$OUT_DIR/discord-exception.txt"
# A previous run's payload must never be posted by mistake: the ladder either
# writes a fresh JSON or an exception report, never both.
rm -f -- "$JSON" "$DISCORD_EXCEPTION"
trap 'rm -f -- "$TRIM_LIST"' EXIT

python3 - "$MD" "$JSON" "$TRIM_LIST" "$DISCORD_EXCEPTION" <<'PY'
import json, sys

md_path, json_path, trim_path, exc_path = sys.argv[1:5]
LIMIT = 2000

with open(md_path, encoding="utf-8") as f:
    full = f.read()
with open(trim_path, encoding="utf-8") as f:
    # tail-first, lowest-signal class first - built by release-notes.sh
    candidates = [line for line in f.read().split("\n") if line]

lines = full.split("\n")
trimmed = []
while len("\n".join(lines)) > LIMIT and candidates:
    cand = candidates.pop(0)
    for i in range(len(lines) - 1, -1, -1):
        if lines[i] == cand:
            del lines[i]
            trimmed.append(cand)
            break
content = "\n".join(lines)

if len(content) > LIMIT:
    trimmed_note = ""
    if trimmed:
        trimmed_note = "Trimmed to fit:\n%s\n\n" % "\n".join(
            "  " + line for line in trimmed)
    with open(exc_path, "w", encoding="utf-8", newline="\n") as f:
        f.write(
            "release-notes: Discord post skipped - %d chars after trimming "
            "%d bullet%s (limit %d).\n"
            "The GitHub release notes are complete at %s.\n"
            "%s"
            "Post the full content below by hand if wanted.\n"
            "\n-----8<-----\n%s"
            % (len(content), len(trimmed), "" if len(trimmed) == 1 else "s",
               LIMIT, md_path, trimmed_note, full)
        )
    sys.stderr.write(
        "release-notes: Discord post skipped - %d chars after trimming all "
        "%d trimmable bullet%s (limit %d); full notes at %s; "
        "exception report at %s\n"
        % (len(content), len(trimmed), "" if len(trimmed) == 1 else "s",
           LIMIT, md_path, exc_path)
    )
    sys.exit(1)

if trimmed:
    sys.stderr.write(
        "release-notes: Discord post trimmed %d bullet%s to fit the "
        "%d-char cap (%d chars) - full notes at %s\n"
        % (len(trimmed), "" if len(trimmed) == 1 else "s", LIMIT,
           len(content), md_path)
    )
    for line in trimmed:
        sys.stderr.write("release-notes: trimmed %s\n" % line)

with open(json_path, "w", encoding="utf-8") as f:
    json.dump({"content": content, "username": "omega releases"},
              f, ensure_ascii=False, indent=2)
    f.write("\n")
PY

cat "$MD"
