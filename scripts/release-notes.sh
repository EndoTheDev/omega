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
# Highlight pick (mechanical, max 5, no ranking cleverness):
#   1. feat subjects first - newest first, one per scope (topic = scope),
#      max 3,
#   2. informative card-merge subjects ("merge: <story>") next - newest
#      first, max 2, deduped by story substring,
#   3. fixes fill the remaining slots - newest first, one per scope.
#   wip/style/chore/docs/test/embed-lane noise and bare "Merge branch"
#   lines never highlight; they still count in the tallies (other/merges).
#   The counts line lists distinct scopes, newest touched first, capped
#   at 6 so it stays one line a human reads.
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
#
# Exit codes: 2 unusable input (not a git repo / no base found), and the
# JSON step fails loudly if the content would blow Discord's 2000-char cap.

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
re_scope='^[a-z]+\(([^)]+)\):'

n_feat=0 n_fix=0 n_tune=0 n_other=0 n_merge=0
fc_disp=() fc_scope=()
fx_disp=() fx_scope=()
mg_disp=() mg_norm=()
scopes=() scope_seen=""

strip_id() { # a trailing "(t_xxxx)" card pointer is noise in a public line
  printf '%s' "$1" | sed -E 's/ ?\(t_[0-9a-f]+\)$//'
}

merge_story() { # "merge: t_xxx - the story (t_xxx)" -> "the story"
  printf '%s' "$1" | sed -E \
    -e 's/^[Mm]erge:[[:space:]]*//' \
    -e 's/^t_[0-9a-f]+[[:space:]]*(-[[:space:]]*)?//' \
    -e 's/ ?\(t_[0-9a-f]+\)$//'
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
    # informative card merges only; bare "Merge branch 'x' into y" stays a count
    if [[ "$low" == merge:* ]]; then
      story="$(merge_story "$subj")"
      mg_disp+=("$(strip_id "$story")")
      mg_norm+=("$(printf '%s' "$story" | tr '[:upper:]' '[:lower:]')")
    fi
    continue
  fi
  scope=""
  if [[ "$low" =~ $re_scope ]]; then
    scope="${BASH_REMATCH[1]}"
    scope="${scope,,}"
  fi
  disp="$(strip_id "$subj")"
  if [[ "$low" =~ $re_feat ]]; then
    n_feat=$((n_feat + 1))
    fc_disp+=("$disp"); fc_scope+=("$scope")
  elif [[ "$low" =~ $re_fix ]]; then
    n_fix=$((n_fix + 1))
    fx_disp+=("$disp"); fx_scope+=("$scope")
  elif [[ "$low" =~ $re_tune ]]; then
    n_tune=$((n_tune + 1))
  else
    n_other=$((n_other + 1))
  fi
  if [ -n "$scope" ] && [[ ";$scope_seen;" != *";$scope;"* ]]; then
    scopes+=("$scope")
    scope_seen="$scope_seen;$scope"
  fi
done < <(git log --format='%h%x1f%p%x1f%s%x1e' "${BASE}..HEAD")

# --- the pick ---------------------------------------------------------------
FEATS_CAP=3
MERGES_CAP=2
MAX_HL=5
hl=()

# feats: newest-first, one per scope (scopeless topic = the subject itself)
declare -A seen=()
i=0
while [ "$i" -lt "${#fc_disp[@]}" ] && [ "${#hl[@]}" -lt "$FEATS_CAP" ]; do
  key="${fc_scope[$i]}"
  [ -n "$key" ] || key="subj:${fc_disp[$i]}"
  if [ -z "${seen[$key]:-}" ]; then
    seen[$key]=1
    hl+=("${fc_disp[$i]}")
  fi
  i=$((i + 1))
done

# card merges: newest-first, max 2, deduped by story substring
picked_norms=()
n=0
i=0
while [ "$i" -lt "${#mg_disp[@]}" ] && [ "$n" -lt "$MERGES_CAP" ] && [ "${#hl[@]}" -lt "$MAX_HL" ]; do
  norm="${mg_norm[$i]}"
  dup=0
  if [ "${#picked_norms[@]}" -gt 0 ]; then
    for have in "${picked_norms[@]}"; do
      if [[ "$norm" == *"$have"* || "$have" == *"$norm"* ]]; then
        dup=1
        break
      fi
    done
  fi
  if [ "$dup" -eq 0 ]; then
    picked_norms+=("$norm")
    hl+=("${mg_disp[$i]}")
    n=$((n + 1))
  fi
  i=$((i + 1))
done

# fixes fill what is left
declare -A fseen=()
i=0
while [ "$i" -lt "${#fx_disp[@]}" ] && [ "${#hl[@]}" -lt "$MAX_HL" ]; do
  key="${fx_scope[$i]}"
  [ -n "$key" ] || key="subj:${fx_disp[$i]}"
  if [ -z "${fseen[$key]:-}" ]; then
    fseen[$key]=1
    hl+=("${fx_disp[$i]}")
  fi
  i=$((i + 1))
done

# --- assembly ----------------------------------------------------------------
plural() { if [ "$1" -eq 1 ]; then echo ""; else echo "s"; fi; }
plural_fix() { if [ "$1" -eq 1 ]; then echo ""; else echo "es"; fi; } # fx -> fixes

counts="$n_feat feature$(plural "$n_feat"), $n_fix fix$(plural_fix "$n_fix"), $n_tune tune$(plural "$n_tune")"
if [ "$n_other" -gt 0 ]; then counts="$counts, $n_other other"; fi
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

python3 - "$MD" "$JSON" <<'PY'
import json, sys
md_path, json_path = sys.argv[1], sys.argv[2]
with open(md_path, encoding="utf-8") as f:
    content = f.read()
if len(content) > 2000:
    sys.exit("release-notes: Discord content exceeds the 2000-char cap (%d chars)" % len(content))
with open(json_path, "w", encoding="utf-8") as f:
    json.dump({"content": content, "username": "omega releases"},
              f, ensure_ascii=False, indent=2)
    f.write("\n")
PY

cat "$MD"
