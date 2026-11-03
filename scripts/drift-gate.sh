#!/usr/bin/env bash
set -euo pipefail

# drift-gate.sh - the hard-fail drift gate between dev and master.
#
# Version of the card-1 stub (which reported only). CI on every dev push
# fails RED when dev is more than DRIFT_MAX_COMMITS ahead of master or
# more than DRIFT_MAX_DAYS past the last master sync. The gate is the
# tripwire that keeps a release round cheap: when it goes red, master
# syncs (soak ff or a release round) and dev stays walkable.
#
# Usage:
#     scripts/drift-gate.sh                       # CI wiring
#
# Environment:
#     DRIFT_BASE           base ref (default: origin/master)
#     DRIFT_MAX_COMMITS    commit budget (default: 30)
#     DRIFT_MAX_DAYS       day budget since last sync (default: 7)
#     DRIFT_ADVISORY       when set, over-budget becomes a greppable
#                          DRIFT WARNING line and exit 0 - the signal
#                          survives, a per-push badge never lies. Default
#                          (unset) keeps hard-fail: the local + release
#                          gate tripwire is unchanged.
#     GITHUB_STEP_SUMMARY  when set, a markdown report appends to it
#
# Exit codes: 0 within budget (or advisory: over budget, warning logged),
# 1 OVER BUDGET (the hard-fail red, default), 2 unmeasurable (base missing
# - also red in advisory mode: an absent master means drift is unknowable).

BASE="${DRIFT_BASE:-origin/master}"
MAX_COMMITS="${DRIFT_MAX_COMMITS:-30}"
MAX_DAYS="${DRIFT_MAX_DAYS:-7}"

# Both budgets must be integers.
re='^[0-9]+$'
if ! [[ "$MAX_COMMITS" =~ $re && "$MAX_DAYS" =~ $re ]]; then
  echo "drift gate: DRIFT_MAX_COMMITS/DRIFT_MAX_DAYS must be integers (got $MAX_COMMITS/$MAX_DAYS)" >&2
  exit 2
fi

# An unmeasurable base is not a green: fail loudly.
git rev-parse --verify "$BASE" >/dev/null 2>&1 || {
  echo "drift gate: base ref $BASE not found (needs a full checkout - fetch-depth 0)" >&2
  exit 2
}

commits=$(git rev-list --count "$BASE..HEAD")
mb=$(git merge-base "$BASE" HEAD)
mb_ct=$(git log -1 --format=%ct "$mb")
head_ct=$(git log -1 --format=%ct HEAD)
days=$(( (head_ct - mb_ct) / 86400 ))

reasons=()
verdict="GREEN"
[ "$commits" -le "$MAX_COMMITS" ] || reasons+=("dev is $commits commits ahead of master (limit $MAX_COMMITS)")
[ "$days" -le "$MAX_DAYS" ] || reasons+=("dev is $days days past master's last sync (limit $MAX_DAYS)")
ADVISORY=""
[ -z "${DRIFT_ADVISORY:-}" ] || ADVISORY=1
[ "${#reasons[@]}" -eq 0 ] || verdict=ADVISORY
[ "$ADVISORY" ] || [ "${#reasons[@]}" -eq 0 ] || verdict="RED"

report() {
  {
    echo "## Drift gate: dev vs master"
    echo ""
    echo "| metric | value | budget |"
    echo "|--------|-------|--------|"
    echo "| commits ahead of master | $commits | $MAX_COMMITS |"
    echo "| days since last sync from master | $days | $MAX_DAYS |"
    echo "| merge-base | \`$mb\` | - |"
    echo ""
    for r in "${reasons[@]}"; do
      echo "- :rotating_light: $r"
    done
    echo ""
    echo "**Verdict: $verdict.** Red = dev has outgrown master: sync it (release round, or the nightly soak ff) before more dev pushes. Advisory = warning logged, never a failed push."
  }
}

if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
  report >>"$GITHUB_STEP_SUMMARY"
else
  report >&2
fi

if [ "${#reasons[@]}" -gt 0 ]; then
  if [ "$ADVISORY" ]; then
    # The signal survives as a greppable line; the per-push job stays green.
    echo "DRIFT WARNING: $commits commits / $days days ahead of master - reconcile at the next release round"
    echo "::warning::DRIFT ADVISORY: ${reasons[*]}"
    exit 0
  fi
  for r in "${reasons[@]}"; do
    echo "::error::DRIFT GATE: $r"
  done
  exit 1
fi