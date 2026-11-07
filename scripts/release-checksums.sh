#!/usr/bin/env bash
set -euo pipefail

# release-checksums.sh <dist-dir>
#
# Generates <dist-dir>/checksums.txt for a release. Every asset gets TWO
# lines, both carrying the same SHA256:
#
#   1. the full release filename (sha256sum output format) - what the
#      current updater matches, by substring:
#        3e9a490...  omega-v0.15.2-windows_amd64.zip
#   2. a bare platform-tag alias - what binaries built before 2026-10-16
#      (v0.6.0 and older) require, because their baked-in verifyChecksum
#      matches entries by strings.HasSuffix(filename, "<goos>_<goarch>")
#      and a real filename never ends with the bare tag (it ends with
#      .zip or .tar.gz), so those installs abort self-update with
#      "checksums.txt has no entry for <tag>" and strand:
#        3e9a490...  windows_amd64
#
# The wild binaries cannot be patched; the release side must stay legible
# to both matchers (card t_3d123470). Every future checksums.txt format
# change must keep both shapes green - scripts/release-checksums_test.sh
# pins the contract, and the legacy consumer shape is pinned from the Go
# side in cmd/omega/checksum_test.go.
#
# Once no supported install predates v0.7.0 the alias lines can be
# dropped; until then they are load-bearing for self-update.

dist="${1:?usage: release-checksums.sh <dist-dir>}"
out="$dist/checksums.txt"

shopt -s nullglob
assets=("$dist"/omega-*.zip "$dist"/omega-*.tar.gz)
shopt -u nullglob

if [ "${#assets[@]}" -eq 0 ]; then
	echo "release-checksums.sh: no omega-* assets in $dist" >&2
	exit 1
fi

tmp="$(mktemp "$dist/.checksums.XXXXXX")"
trap 'rm -f "$tmp"' EXIT

for f in "${assets[@]}"; do
	name="$(basename "$f")"
	hash="$(cd "$dist" && sha256sum -- "$name")"
	hash="${hash%% *}"
	# Emit both lines ourselves, never sha256sum's verbatim: MSYS
	# sha256sum defaults to binary mode and marks it ("hash *name"),
	# the Linux runner does not ("hash  name") - Fields-tolerant on the
	# consumer side, but the release file gets ONE canonical shape.
	# The hash itself is raw-byte (binary) on every platform.
	printf '%s  %s\n' "$hash" "$name" >>"$tmp"
	# omega-v0.15.2-nightly.20261107-windows_amd64.tar.gz -> windows_amd64
	bare="${name%.*}" # drop .zip / .gz
	bare="${bare%.tar}" # drop .tar (two-part extension)
	bare="${bare##*-}" # drop through the last dash
	printf '%s  %s\n' "$hash" "$bare" >>"$tmp"
done

mv "$tmp" "$out"
trap - EXIT
