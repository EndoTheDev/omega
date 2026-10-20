#!/usr/bin/env bash
set -euo pipefail

# embed-lane.sh - the ONE lane that owns modules/http_channel/static/.
#
# Webui cards commit SOURCE ONLY (modules/ui/ and modules/<name>/ui/).
# The committed SPA bundle is regenerated and committed here, right
# after a source merge lands, so no branch ever diverges static/ and
# parallel webui cards can never collide on it. go build = complete
# product still holds: static/ stays tracked, the lane keeps it true.
#
# Usage (in the merged tree, right after the source merge):
#
#   scripts/embed-lane.sh [--base <rev>] "<context>"
#
#   --base <rev>  the pre-merge tip, for the change gate. Defaults to
#                 HEAD^1, which is the pre-landing tip for both a real
#                 merge commit and a fast-forward; only a root commit
#                 (no parent) must pass --base.
#
# Guards:
#   - change gate: if <base>..HEAD touches no webui source (modules/ui/
#     or modules/*/ui/), skip regeneration entirely - Go-only merges
#     pay nothing.
#   - hash gate: after the build, an unchanged static/ tree is not
#     committed - the lane never writes no-op commits.
#   - fail-loud: any build failure aborts the merge pass loudly.
#     static/ is NEVER hand-resolved; it is regenerated, or the merge
#     fails. (The root build.sh conflict-marker gate stays as belt
#     and suspenders; this script re-checks before committing.)
#
# Test/offline override:
#   EMBED_LANE_BUILD_CMD - the command that regenerates the bundle.
#   The real pass uses modules/ui/scripts/build.sh.

STATIC="modules/http_channel/static"
LANE_PREFIX="embed-lane: rebuild webui bundle for"

# ui_changed <base> <head>: non-empty output when the diff touches
# webui source (additions, edits, renames AND deletions - removing a
# component changes the bundle too).
ui_changed() {
	git diff --name-only "$1" "$2" -- "modules/ui" ":(glob)modules/*/ui/**"
}

lint_static() {
	if grep -rqE '<<<<<<<|Stashed changes' "$STATIC"; then
		echo "FATAL: embed-lane: conflict markers in the regenerated $STATIC" >&2
		exit 1
	fi
}

fail_no_base() {
	echo "FATAL: embed-lane: HEAD has no parent commit - the change gate needs" >&2
	echo "       the pre-landing tip. Pass: embed-lane.sh --base <pre-landing-tip> \"<context>\"" >&2
	exit 1
}

usage() {
	echo "usage: embed-lane.sh [--base <rev>] \"<context>\"" >&2
	exit 2
}

regen_bundle() {
	echo "==> embed-lane: regenerating the webui bundle (source changed)"
	if ! "${EMBED_LANE_BUILD_CMD:-modules/ui/scripts/build.sh}"; then
		echo "FATAL: embed-lane: webui build FAILED - the merge pass aborts here." >&2
		echo "       static/ is NEVER hand-resolved: fix the ui source or the" >&2
		echo "       build, then rerun embed-lane (regeneration is the only path)." >&2
		exit 1
	fi
	[ -d "$STATIC" ] || {
		echo "FATAL: embed-lane: build produced no $STATIC - the merge pass aborts here." >&2
		exit 1
	}
	lint_static
}

lane_commit() {
	git add -A -- "$STATIC"
	if git diff --cached --quiet HEAD -- "$STATIC"; then
		echo "==> embed-lane: hash gate: bundle unchanged since the last lane commit - skip"
		return 1
	fi
	if ! git commit --only "$STATIC" -m "$LANE_PREFIX $1"; then
		echo "FATAL: embed-lane: failed to commit the regenerated $STATIC - the merge pass aborts here." >&2
		exit 1
	fi
	echo "==> embed-lane: lane commit $(git rev-parse --short HEAD)"
}

main() {
	local base="" context=""
	while [ $# -gt 0 ]; do
		case "$1" in
		--base)
			[ $# -ge 2 ] || usage
			base="$2"
			shift 2
			;;
		-h|--help)
			usage
			;;
		-*)
			usage
			;;
		*)
			if [ -n "$context" ]; then usage; fi
			context="$1"
			shift
			;;
		esac
	done
	[ -n "$context" ] || context="merge"

	cd "$(git rev-parse --show-toplevel)"

	if [ -z "$base" ]; then
		if git rev-parse -q --verify HEAD^1 >/dev/null 2>&1; then
			base="$(git rev-parse HEAD^1)"
		else
			fail_no_base
		fi
	fi

	if [ -z "$(ui_changed "$base" HEAD)" ]; then
		echo "==> embed-lane: change gate: no webui source in the diff - skip (Go-only merge pays nothing)"
		return 0
	fi

	regen_bundle

	lane_commit "$context" || true # hash-gate skip is a clean exit, never a failure
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
	main "$@"
fi