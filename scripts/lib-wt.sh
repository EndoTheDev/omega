#!/usr/bin/env bash
# lib-wt.sh - the worktree path fold, shared by train-pass.sh and
# factory-prune.sh. Sourced, never executed.
#
# A card's workspace_path arrives in the DISPATCHER's spelling (winbtw:
# D:\Code\omega\.worktrees\t_x), `git worktree list` prints git's own
# (D:/Code/...), and an MSYS caller may hand /d/Code/... . Windows is
# case-blind and a trailing slash names the same directory, so every
# comparison folds separators, the MSYS drive form, trailing slashes and
# case BEFORE anything is compared.
#
# One copy, deliberately: the train's free step and the pruner both decide
# what .worktrees/<id> IS from the same registrations. Two copies drift,
# and a drift means one of them deletes a directory the other calls a live
# worktree (or the reverse).

# wt_norm <path> -> the comparison spelling of a path.
wt_norm() {
  local p="$1"
  p="${p//\\//}"
  if command -v cygpath >/dev/null 2>&1; then
    p="$(cygpath -m "$p" 2>/dev/null || printf '%s' "$p")"
  fi
  while [ "$p" != "/" ] && [ "${p%/}" != "$p" ]; do p="${p%/}"; done
  case "$p" in
    [A-Za-z]:*) p="$(printf '%s' "$p" | tr 'A-Z' 'a-z')" ;;
  esac
  printf '%s' "$p"
}

# wt_registered_paths: every worktree this checkout registers, normalized,
# one per line. Resolved against the cwd's repository, so call it from
# inside the checkout.
wt_registered_paths() {
  git worktree list --porcelain 2>/dev/null \
    | sed -n 's|^worktree \(.*\)$|\1|p' \
    | while IFS= read -r p; do
        wt_norm "$p"
        printf '\n'
      done
}

# wt_registered <path>: this checkout lists that path as one of its
# worktrees. Any spelling of the same directory answers yes (see wt_norm).
wt_registered() {
  local want have
  want="$(wt_norm "$1")"
  while IFS= read -r have; do
    if [ "$have" = "$want" ]; then return 0; fi
  done < <(wt_registered_paths)
  return 1
}
