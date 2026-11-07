#!/usr/bin/env bash
set -uo pipefail

# release-checksums_test.sh - fixture tests for scripts/release-checksums.sh.
# Run: bash scripts/release-checksums_test.sh
#
# Pins the BOTH-SHAPES contract (card t_3d123470): every asset emits its
# full-filename line (current updaters, substring match) AND a bare
# platform-tag alias line (pre-Oct-16 binaries <= v0.6.0, whose baked-in
# verifyChecksum matches strings.HasSuffix(filename, "<goos>_<goarch>")).
# A checksums.txt that drops either shape strands the binary generation
# that needs it: the aliases are load-bearing until no supported install
# predates v0.7.0.

SUT="$(cd "$(dirname "$0")" && pwd)/release-checksums.sh"

pass=0
fail=0
ok() { pass=$((pass + 1)); }
bad() { fail=$((fail + 1)); echo "FAIL: $1"; }

new_dist() {
	local d
	d="$(mktemp -d "${TMPDIR:-/tmp}/relcs-fx.XXXXXX")"
	echo "$d"
}

# run_sut <dist>: exit code -> RC
run_sut() {
	bash "$SUT" "$1" >/dev/null 2>&1
	RC=$?
}

# sha <file>: the sha256sum hash field only
sha() { sha256sum -- "$1" | awk '{print $1}'; }

# legacy_suffix_match <dist> <tag>: 0 when some checksums.txt line's SECOND
# field ends exactly with <tag> - the pre-Oct-16 strings.HasSuffix lookup.
legacy_suffix_match() {
	local dist=$1 tag=$2
	awk -v t="$tag" '
		$2 ~ t "$" { found = 1; exit }
		END { exit found ? 0 : 1 }
	' "$dist/checksums.txt"
}

# --- C1: full lines, sha256sum format, correct hashes -----------------------
fx=$(new_dist)
echo win >"$fx/omega-v0.9.9-windows_amd64.zip"
echo lin >"$fx/omega-v0.9.9-linux_amd64.tar.gz"
echo pi >"$fx/omega-v0.9.9-linux_arm64.tar.gz"
run_sut "$fx"
[ "$RC" -eq 0 ] && ok || bad "C1: script exited $RC"
for pair in "windows_amd64.zip" "linux_amd64.tar.gz" "linux_arm64.tar.gz"; do
	h=$(sha "$fx/omega-v0.9.9-$pair")
	grep -qF "$h  omega-v0.9.9-$pair" "$fx/checksums.txt" \
		&& ok || bad "C1: full line missing for omega-v0.9.9-$pair"
done

# --- C2: bare-tag alias lines, one per asset, same hash ----------------------
for tag in windows_amd64 linux_amd64 linux_arm64; do
	h=$(sha "$fx/omega-v0.9.9-$tag".*)
	if grep -qF "$h  $tag" "$fx/checksums.txt"; then
		ok
	else
		bad "C2: alias line '$h  $tag' missing"
	fi
	legacy_suffix_match "$fx" "$tag" \
		&& ok || bad "C2: no line ends with '$tag' - legacy suffix matcher strands"
done

# --- C3: exactly two lines per asset, no checksums.txt self-entry ------------
lines=$(wc -l <"$fx/checksums.txt")
[ "$lines" -eq 6 ] && ok || bad "C3: want 6 lines (2 per asset), got $lines"
	grep -q "checksums.txt" "$fx/checksums.txt" \
		&& bad "C3: checksums.txt lists itself" || ok

# --- C4: nightly-shaped filenames alias to the bare tag ----------------------
fx=$(new_dist)
echo n >"$fx/omega-v0.15.2-nightly.20261107-linux_arm64.tar.gz"
run_sut "$fx"
[ "$RC" -eq 0 ] && ok || bad "C4: script exited $RC"
h=$(sha "$fx/omega-v0.15.2-nightly.20261107-linux_arm64.tar.gz")
	grep -qF "$h  linux_arm64" "$fx/checksums.txt" \
		&& ok || bad "C4: nightly alias line missing or wrong tag"
legacy_suffix_match "$fx" "linux_arm64" \
	&& ok || bad "C4: legacy suffix matcher cannot find nightly entry"

# --- C5: partial dist (zip only, no tar.gz) - no glob leakage ----------------
fx=$(new_dist)
echo solo >"$fx/omega-v0.9.9-darwin_arm64.zip"
run_sut "$fx"
[ "$RC" -eq 0 ] && ok || bad "C5: script exited $RC on zip-only dist"
lines=$(wc -l <"$fx/checksums.txt")
[ "$lines" -eq 2 ] && ok || bad "C5: want 2 lines for one asset, got $lines"
	grep -q "omega-\*" "$fx/checksums.txt" \
		&& bad "C5: literal glob leaked into output" || ok

# --- C6: empty dist refuses loud ---------------------------------------------
fx=$(new_dist)
run_sut "$fx"
[ "$RC" -ne 0 ] && ok || bad "C6: empty dist must exit nonzero"
[ -e "$fx/checksums.txt" ] \
	&& bad "C6: empty dist must not write checksums.txt" || ok

# --- C7: re-run overwrites (idempotent on re-build) --------------------------
fx=$(new_dist)
echo a >"$fx/omega-v1.0.0-windows_amd64.zip"
bash "$SUT" "$fx" >/dev/null 2>&1
bash "$SUT" "$fx" >/dev/null 2>&1
lines=$(wc -l <"$fx/checksums.txt")
[ "$lines" -eq 2 ] && ok || bad "C7: re-run duplicated lines: got $lines"

echo "release-checksums_test.sh: $pass passed, $fail failed"
[ "$fail" -eq 0 ]
