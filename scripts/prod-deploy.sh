#!/usr/bin/env bash
set -euo pipefail
# --- the pin: no line here may pipe a writer into an early-exit grep --------
# `printf '%s' "$OUT" | grep -q` under pipefail hands back the WRITER's
# SIGPIPE when grep leaves at the first match, so a needle that IS present
# reads as missing - and the negated form reads it as absent, which is a false
# green that hides a defect. A herestring has no writer, so grep's early exit
# costs nothing. This file scans its own live lines; comments may name the
# shape, so they are skipped.
_PIN_SHAPE='\|[[:space:]]*grep[[:space:]]+-[a-zA-Z]*q'
_PIN_SELF="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/$(basename "${BASH_SOURCE[0]}")"
_pin_hits="$(grep -vE '^[[:space:]]*#' "$_PIN_SELF" | grep -nE "$_PIN_SHAPE" || true)"
[ -z "$_pin_hits" ] \
	|| { printf '%s: pipes a writer into grep -q (SIGPIPE under pipefail): %s\n' "${_PIN_SELF##*/}" "$_pin_hits" >&2; exit 1; }


# prod-deploy.sh <version-tag>
#
# The prod deploy leg of the push word: ship ONE released version to pibtw
# and prove it came up. Mechanical only - it never tags, never pushes, never
# touches the board.
#
#   usage: scripts/prod-deploy.sh v0.17.3
#
# Steps, printed as they run:
#   0. pre-flight - read the live /health version. Already the target:
#                   "already live: <ver>", exit 0, nothing is touched.
#   1. stop       - systemctl --user stop omega
#   2. fetch      - on the Pi: curl the release tarball + checksums.txt from
#                   the GitHub release, verify sha256, extract to staging.
#                   No gh on the remote, no scp from here.
#   3. place      - previous binary -> omega.bak, install -m 0755 into place,
#                   assert `omega --version` reports the target tag
#   4. start      - systemctl --user start omega
#   5. health     - poll /health until status ok AND version == target,
#                   bounded at 30s, loud fail carrying the last health body
#   6. soak stamp - print the stamp and append one line to deploy.log
#
# Contract: HIS ruling 2026-11-16 - one word does everything, and this is
# the deploy leg of the push word. The sequence is to be named in
# todos/release-driver.md; that file is not in this tree as the script
# lands (card t_a29812bc), so the six steps above are the quoted contract.
#
# The remote layout is pibtw's CURRENT manual layout, probed not invented:
#   OMEGA_HOME  /home/endo/.omega
#   binary      $OMEGA_HOME/omega  (with the manual omega.bak sibling)
#   unit        omega.service, ExecStart=/home/endo/.omega/omega serve
#   health      http://127.0.0.1:8099/health  (server.port in config/config.yaml)
#   deploy log  $OMEGA_HOME/deploy.log       (created on first deploy)
#
# Auth is key-based only: BatchMode=yes makes a missing key or a password
# prompt fail loudly instead of hanging the push. No password is ever an
# argument of this script or of any call it makes.
#
# Env seams (defaults are the production values; the test suite drives the
# timeout case through them so a bounded 30s wait is never paid in a test):
#   PROD_DEPLOY_HOST           default pibtw
#   PROD_DEPLOY_HEALTH_TIMEOUT default 30   (seconds)

REMOTE_HOST=${PROD_DEPLOY_HOST:-pibtw}
REMOTE_HOME=/home/endo/.omega
REMOTE_BIN=$REMOTE_HOME/omega
REMOTE_BAK=$REMOTE_HOME/omega.bak
REMOTE_LOG=$REMOTE_HOME/deploy.log
REMOTE_STAGE=/tmp/omega-deploy.stage
HEALTH_URL=http://127.0.0.1:8099/health
RELEASE_BASE=https://github.com/EndoTheDev/omega/releases/download
PLATFORM=linux_arm64
HEALTH_TIMEOUT=${PROD_DEPLOY_HEALTH_TIMEOUT:-30}
HEALTH_INTERVAL=2

usage() {
	echo "usage: scripts/prod-deploy.sh <version-tag>  (e.g. v0.17.3)" >&2
}

die() {
	printf '\nprod-deploy.sh: FAIL: %s\n' "$1" >&2
	if [ $# -gt 1 ]; then
		printf 'prod-deploy.sh: %s\n' "$2" >&2
	fi
	if [ "${UNIT_STOPPED:-0}" = 1 ]; then
		printf 'prod-deploy.sh: the unit is stopped. Restore the previous binary with:\n' >&2
		printf '  ssh %s systemctl --user start omega   (or: cp %s %s first)\n' \
			"$REMOTE_HOST" "$REMOTE_BAK" "$REMOTE_BIN" >&2
	fi
	exit 1
}

step() {
	printf '\n[%s] %s\n' "$1" "$2"
}

# ssh_run <step> [KEY=value ...] - runs the remote script on stdin under a
# step marker, key-based only, no prompts.
ssh_run() {
	local step_name=$1
	shift
	ssh -o BatchMode=yes -o ConnectTimeout=10 "$REMOTE_HOST" \
		"OMEGA_DEPLOY_STEP=$step_name $* bash -euo pipefail -s"
}

# health_ready <body> - status ok AND the version is the target.
health_ready() {
	local body=${1:-} got
	[ -n "$body" ] || return 1
	grep -Eq '"status"[[:space:]]*:[[:space:]]*"ok"' <<<"$body" || return 1
	got=$(printf '%s' "$body" |
		sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
	[ "$got" = "$VERSION" ]
}

# health_body - the remote /health body on stdout; exits nonzero when the
# service is not answering.
health_body() {
	ssh_run health "DEPLOY_HEALTH_URL=$HEALTH_URL" <<'REMOTE'
curl -fsS -m 3 "$DEPLOY_HEALTH_URL"
REMOTE
}

# poll_health - one remote health poll; sets HEALTH_LAST, returns 0 only when
# the body reports status ok at the target version.
poll_health() {
	local rc=0
	HEALTH_LAST=$(health_body 2>/dev/null) || rc=$?
	[ "$rc" -eq 0 ] && health_ready "$HEALTH_LAST"
}

VERSION=${1:-}
if [ -z "$VERSION" ]; then
	usage
	die "version tag is required"
fi
# Strict shape: also the injection guard - the tag rides a remote command line.
if ! grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z][0-9A-Za-z.]*)?$' <<<"$VERSION"; then
	die "not a version tag: '$VERSION' (want vMAJOR.MINOR.PATCH or vMAJOR.MINOR.PATCH-nightly.YYYYMMDD)"
fi

command -v ssh >/dev/null 2>&1 || die "ssh not found - this leg needs the key-based ssh to $REMOTE_HOST"

# --- 0. pre-flight -----------------------------------------------------------
step 0/6 "pre-flight: live version on $REMOTE_HOST"
LIVE_RC=0
LIVE_BODY=$(health_body 2>/dev/null) || LIVE_RC=$?
if [ "$LIVE_RC" -eq 255 ]; then
	die "cannot reach $REMOTE_HOST over ssh (key-based auth, BatchMode)"
fi
if [ "$LIVE_RC" -eq 0 ]; then
	echo "  live: $(printf '%s' "$LIVE_BODY" | sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
	if health_ready "$LIVE_BODY"; then
		echo "already live: $VERSION"
		exit 0
	fi
else
	echo "  live: no answer from $HEALTH_URL (unit down, or a deploy is in flight)"
fi

# --- 1. stop -----------------------------------------------------------------
step 1/6 "stop unit omega"
ssh_run stop <<'REMOTE' || die "systemctl --user stop omega failed"
systemctl --user stop omega
REMOTE
UNIT_STOPPED=1
echo "  stopped"

# --- 2. fetch ----------------------------------------------------------------
step 2/6 "fetch $VERSION ($PLATFORM) from the GitHub release"
ssh_run fetch \
	"DEPLOY_VERSION=$VERSION" \
	"DEPLOY_PLATFORM=$PLATFORM" \
	"DEPLOY_BASE=$RELEASE_BASE" \
	"DEPLOY_STAGE=$REMOTE_STAGE" <<'REMOTE' || die "fetch or checksum verify failed for $VERSION"
rm -rf "$DEPLOY_STAGE"
mkdir -p "$DEPLOY_STAGE"
cd "$DEPLOY_STAGE"
asset="omega-$DEPLOY_VERSION-$DEPLOY_PLATFORM.tar.gz"
url="$DEPLOY_BASE/$DEPLOY_VERSION/$asset"
echo "  $url"
curl -fsSL --retry 3 --retry-delay 2 -o "$asset" "$url"
curl -fsSL --retry 3 --retry-delay 2 -o checksums.txt "$DEPLOY_BASE/$DEPLOY_VERSION/checksums.txt"
want=$(awk -v t="$DEPLOY_PLATFORM" '$2 == t { print $1; exit }' checksums.txt)
[ -n "$want" ] || { echo "checksums.txt has no $DEPLOY_PLATFORM entry" >&2; exit 1; }
got=$(sha256sum "$asset" | cut -d' ' -f1)
[ "$want" = "$got" ] || { echo "sha256 mismatch for $asset: want $want got $got" >&2; exit 1; }
echo "  sha256 ok: $got"
tar -xzf "$asset"
REMOTE
echo "  staged at $REMOTE_STAGE"

# --- 3. place ----------------------------------------------------------------
step 3/6 "place binary at $REMOTE_BIN"
ssh_run place \
	"DEPLOY_VERSION=$VERSION" \
	"DEPLOY_PLATFORM=$PLATFORM" \
	"DEPLOY_HOME=$REMOTE_HOME" \
	"DEPLOY_STAGE=$REMOTE_STAGE" <<'REMOTE' || die "installing the binary at $REMOTE_BIN failed"
src="$DEPLOY_STAGE/omega-$DEPLOY_VERSION-$DEPLOY_PLATFORM/omega"
[ -f "$src" ] || { echo "staged binary missing: $src" >&2; exit 1; }
if [ -f "$DEPLOY_HOME/omega" ]; then
	cp -p "$DEPLOY_HOME/omega" "$DEPLOY_HOME/omega.bak"
fi
install -m 0755 "$src" "$DEPLOY_HOME/omega"
got=$("$DEPLOY_HOME/omega" --version 2>&1 | head -1)
case $got in
	*"$DEPLOY_VERSION"*) echo "  $got" ;;
	*) echo "placed binary reports [$got], want $DEPLOY_VERSION" >&2; exit 1 ;;
esac
rm -rf "$DEPLOY_STAGE"
REMOTE
echo "  previous binary kept at $REMOTE_BAK"

# --- 4. start ----------------------------------------------------------------
step 4/6 "start unit omega"
ssh_run start <<'REMOTE' || die "systemctl --user start omega failed"
systemctl --user start omega
REMOTE
UNIT_STOPPED=0
echo "  started"

# --- 5. health ---------------------------------------------------------------
step 5/6 "health check: $HEALTH_URL until status ok at $VERSION (${HEALTH_TIMEOUT}s bound)"
DEADLINE=$((SECONDS + HEALTH_TIMEOUT))
HEALTH_LAST=""
READY=0
while :; do
	if poll_health; then
		READY=1
		break
	fi
	if [ "$SECONDS" -ge "$DEADLINE" ]; then
		break
	fi
	sleep "$HEALTH_INTERVAL"
done
if [ "$READY" -ne 1 ]; then
	die "health check timed out after ${HEALTH_TIMEOUT}s - last $HEALTH_URL body: ${HEALTH_LAST:-<no answer>}" \
		"$VERSION is installed but not answering. Roll back with: ssh $REMOTE_HOST 'cp $REMOTE_BAK $REMOTE_BIN && systemctl --user restart omega'"
fi
echo "  ok: $VERSION is serving"

# --- 6. soak stamp -----------------------------------------------------------
step 6/6 "soak stamp"
STAMP=$(date -u +%Y-%m-%dT%H:%M:%SZ)
ssh_run stamp \
	"DEPLOY_VERSION=$VERSION" \
	"DEPLOY_STAMP=$STAMP" \
	"DEPLOY_LOG=$REMOTE_LOG" <<'REMOTE' || die "appending to $REMOTE_LOG failed"
printf '%s %s deployed\n' "$DEPLOY_STAMP" "$DEPLOY_VERSION" >>"$DEPLOY_LOG"
REMOTE
echo "soak started: $VERSION at $STAMP"
echo "deploy log:   $REMOTE_LOG"
