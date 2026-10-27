#!/usr/bin/env bash
set -euo pipefail

# embed-lane.sh - the ONE lane that owns http_channel/static/.
#
# Webui cards commit SOURCE ONLY (webui/ and modules/<name>/ui/).
# The committed SPA bundle is regenerated and committed here, right
# after a source merge lands, so no branch ever diverges static/ and
# parallel webui cards can never collide on it. go build = complete
# product still holds: static/ stays tracked, the lane keeps it true.
#
# Usage (in the merged tree, right after the source merge):
#
#   scripts/embed-lane.sh [--base <rev>] "<context>"
#
#   --base <rev>  the pre-merge tip, for the change gate. Without it the
#                 base is selected by select_base (below): HEAD^1 for a
#                 real merge commit, origin/dev for a fast-forward
#                 result, and an UNPROVEN fallback that bypasses the
#                 gate rather than risk skipping a needed regen.
#
# Guards:
#   - change gate: if <base>..HEAD touches no webui source (webui/
#     or modules/*/ui/), skip regeneration entirely - Go-only merges
#     pay nothing.
#   - hash gate: after the build, a bundle that is CONTENT-identical to
#     the committed one is not committed - the lane never writes no-op
#     commits. Nuxt's volatile build metadata (buildId uuid, epoch
#     timestamps in the html + builds/meta jsons) is stripped before
#     comparing, so regenerating unchanged source skips instead of
#     churning build ids into history.
#   - fail-loud: any build failure aborts the merge pass loudly.
#     static/ is NEVER hand-resolved; it is regenerated, or the merge
#     fails. (The root build.sh conflict-marker gate stays as belt
#     and suspenders; this script re-checks before committing.)
#
# Test/offline override:
#   EMBED_LANE_BUILD_CMD - the command that regenerates the bundle.
#   The real pass uses webui/scripts/build.sh.

STATIC="http_channel/static"
LANE_PREFIX="embed-lane: rebuild webui bundle for"

# ui_changed <base> <head>: non-empty output when the diff touches
# webui source (additions, edits, renames AND deletions - removing a
# component changes the bundle too).
ui_changed() {
	git diff --name-only "$1" "$2" -- "webui" ":(glob)modules/*/ui/**"
}

# strip_volatile: nuxt build ids (uuids) and epoch-millisecond build
# timestamps are metadata, not bundle content - they differ on every
# real generate even when the source is unchanged.
strip_volatile() {
	sed -E 's/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/BUILDID/g; s/17[0-9]{11}/EPOCHMS/g'
}

# canonical_hash_disk: content hash of $STATIC on disk with volatile
# build metadata (in paths and in contents) stripped.
canonical_hash_disk() {
	local path norm
	while IFS= read -r -d '' path; do
		norm="$(strip_volatile <<<"${path#"$STATIC"/}")"
		printf '%s\n' "$norm"
		strip_volatile <"$path" | sha256sum
	done < <(find "$STATIC" -type f -print0 | sort -z) | sha256sum
}

# canonical_hash_head: the same hash over the committed $STATIC tree
# (what HEAD already owns, i.e. the last lane commit's bundle).
canonical_hash_head() {
	local rec sha_part sha path norm
	while IFS= read -r -d '' rec; do
		sha_part="${rec%%$'	'*}"
		path="${rec#*$'	'}"
		sha="${sha_part##* }"
		norm="$(strip_volatile <<<"${path#"$STATIC"/}")"
		printf '%s\n' "$norm"
		git cat-file blob "$sha" | strip_volatile | sha256sum
	done < <(git ls-tree -r -z HEAD -- "$STATIC") | sha256sum
}

lint_static() {
	if grep -rqE '<<<<<<<|Stashed changes' "$STATIC"; then
		echo "FATAL: embed-lane: conflict markers in the regenerated $STATIC" >&2
		exit 1
	fi
}

# Base selection - the change gate is only as good as its base.
#
# HEAD^1 is the pre-landing tip only for a real merge commit. After a
# fast-forward of a MULTI-commit branch, HEAD^1 is the branch's
# second-to-last card commit, and the gate reads just that one commit's
# diff - a needed regen then reads as a Go-only merge (seen live,
# 2026-10-23: a 5-commit ff skipped the webui bundle regen).
#
# The default therefore never trusts HEAD^1 on a plain commit:
#   - merge commit -> HEAD^1 (first parent is the pre-landing tip).
#   - plain commit + origin/dev a STRICT ancestor of HEAD -> origin/dev.
#     A diff against an older base is a SUPERSET of the true diff; a
#     wider diff can only over-trigger a regen, which the hash gate
#     absorbs. Strictness also closes the empty-diff trap: origin/dev
#     == HEAD would make every gate read "nothing changed".
#   - plain commit with no such marker -> the pre-merge tip is not
#     provable, so the gate is BYPASSED: the regen runs unconditionally
#     and the hash gate decides. The fail direction that matters is a
#     needed regen that reads as Go-only - never silently skipping.
REMOTE_BASE="origin/dev"

# select_base <explicit-or-empty>: sets BASE (a commit), BASE_TRUST
# (human-readable reason for the log) and GATE_TRUSTED (1 = the gate's
# skip verdicts are safe; 0 = never let the gate skip). Sourced runs
# call this directly; the assertions live in embed-lane-base-test.sh.
select_base() {
	BASE="" BASE_TRUST="none" GATE_TRUSTED=0
	if [ -n "$1" ]; then
		if ! BASE="$(git rev-parse -q --verify "$1^{commit}")"; then
			echo "FATAL: embed-lane: --base '$1' is not a commit" >&2
			exit 1
		fi
		BASE_TRUST="explicit (--base)"
		GATE_TRUSTED=1
	elif git rev-parse -q --verify HEAD^2 >/dev/null 2>&1; then
		BASE="$(git rev-parse HEAD^1)"
		BASE_TRUST="merge parent HEAD^1"
		GATE_TRUSTED=1
	else
		local origin_tip
		if origin_tip="$(git rev-parse -q --verify "$REMOTE_BASE^{commit}" 2>/dev/null)" \
			&& [ "$origin_tip" != "$(git rev-parse HEAD)" ] \
			&& git merge-base --is-ancestor "$origin_tip" HEAD; then
			BASE="$origin_tip"
			BASE_TRUST="$REMOTE_BASE (at or behind the pre-merge tip; superset diff)"
			GATE_TRUSTED=1
		elif git rev-parse -q --verify HEAD^1 >/dev/null 2>&1; then
			BASE="$(git rev-parse HEAD^1)"
			BASE_TRUST="UNPROVEN HEAD^1 (plain commit)"
		else
			fail_no_base # root commit: no parent, no marker - loud stop
		fi
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
	if ! "${EMBED_LANE_BUILD_CMD:-webui/scripts/build.sh}"; then
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
	if [ "$(canonical_hash_disk)" = "$(canonical_hash_head)" ]; then
		echo "==> embed-lane: hash gate: bundle content unchanged since the last lane commit - skip"
		return 1
	fi
	git add -A -- "$STATIC"
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

	select_base "$base"
	echo "==> embed-lane: change gate base $(git rev-parse --short "$BASE") [${BASE_TRUST}]"

	if [ "$GATE_TRUSTED" != 1 ]; then
		echo "==> embed-lane: WARNING: pre-merge tip unprovable - a multi-commit fast-forward" >&2
		echo "       cannot be ruled out, so the change gate is BYPASSED: regenerating" >&2
		echo "       unconditionally and letting the hash gate decide." >&2
		echo "       Pass --base <pre-merge-tip> for a normal gate run." >&2
	elif [ -z "$(ui_changed "$BASE" HEAD)" ]; then
		echo "==> embed-lane: change gate: no webui source in $(git rev-parse --short "$BASE")..HEAD - skip (Go-only merge pays nothing)"
		return 0
	fi

	regen_bundle

	lane_commit "$context" || true # hash-gate skip is a clean exit, never a failure
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
	main "$@"
fi