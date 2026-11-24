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
#   1. fetch      - on the Pi: curl the release tarball + checksums.txt from
#                   the GitHub release, verify sha256, extract to staging.
#                   No gh on the remote, no scp from here.
#   2. stop       - systemctl --user stop omega
#   3. copy-boot  - boot the STAGED binary against a COPY of the live home on
#                   the copy-boot port; require health at the target version,
#                   every pre-existing session file hash unchanged, and (when
#                   the copy carries a legacy store) a green migration whose
#                   known-broken record keeps updated_at == created_at
#   4. place      - previous binary -> omega.bak, install -m 0755 into place,
#                   assert `omega --version` reports the target tag
#   5. start      - systemctl --user start omega
#   6. health     - poll /health until status ok AND version == target,
#                   bounded at 30s, loud fail carrying the last health body
#   7. soak stamp - print the stamp and append one line to deploy.log
#
# Contract: HIS ruling 2026-11-16 - one word does everything, and this is
# the deploy leg of the push word. The sequence is to be named in
# todos/release-driver.md; that file is not in this tree as the script
# lands (card t_a29812bc), so the eight steps above are the quoted contract.
#
# The copy-boot rule (step 3) is why a red deploy leaves prod alone. v0.18.0
# crash-looped the unit on boot: the legacy store migration derived a session's
# updated_at instead of copying it, its own read-back gate caught the
# divergence, and omega.service (Restart=always) restarted forever. So before
# anything is placed, the staged binary boots the real shape. The unit is
# stopped when the copy is taken (a stopped unit cannot tear a JSONL append or
# leave a live WAL), the copy lives under /tmp and never in the live home, only
# a binary that came up over it is installed, and ANY red starts the OLD binary
# again and exits nonzero - no soak stamp, prod keeps serving the old version,
# the release stays published.
#
# That leg is a step of THIS script, not a systemd ExecStartPre: a pre-step runs
# before the new binary is placed, so a red copy-boot would leave the unit
# pointing at a binary that cannot boot while the old version stops serving.
# In-script, a red copy-boot restores the old process and the flip never
# happens.
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
#   PROD_DEPLOY_HOST             default pibtw
#   PROD_DEPLOY_HEALTH_TIMEOUT   default 30    (seconds)
#   PROD_DEPLOY_COPYBOOT_PORT    default 18099 (must not be the prod port)
#   PROD_DEPLOY_COPYBOOT_TIMEOUT default 60    (seconds)
#   PROD_DEPLOY_COPYBOOT_ROOT    default /tmp  (the copy's parent, never the home)

REMOTE_HOST=${PROD_DEPLOY_HOST:-pibtw}
REMOTE_HOME=/home/endo/.omega
REMOTE_BIN=$REMOTE_HOME/omega
REMOTE_BAK=$REMOTE_HOME/omega.bak
REMOTE_LOG=$REMOTE_HOME/deploy.log
REMOTE_STAGE=/tmp/omega-deploy.stage
REMOTE_COPY_ROOT=${PROD_DEPLOY_COPYBOOT_ROOT:-/tmp}
HEALTH_URL=http://127.0.0.1:8099/health
RELEASE_BASE=https://github.com/EndoTheDev/omega/releases/download
PLATFORM=linux_arm64
HEALTH_TIMEOUT=${PROD_DEPLOY_HEALTH_TIMEOUT:-30}
HEALTH_INTERVAL=2
COPYBOOT_PORT=${PROD_DEPLOY_COPYBOOT_PORT:-18099}
COPYBOOT_TIMEOUT=${PROD_DEPLOY_COPYBOOT_TIMEOUT:-60}
# The session that crash-looped v0.18.0: created_at == updated_at, never
# refreshed after creation. Its migrated header must keep both stamps equal.
REGRESSION_SESSION=72863811adb93cd5ca47642c9b8393f6

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
# The copy-boot must never boot on top of the live service.
PROD_PORT=$(printf '%s' "$HEALTH_URL" | sed -n 's|.*:\([0-9][0-9]*\)/.*|\1|p')
if [ "$COPYBOOT_PORT" = "$PROD_PORT" ]; then
	die "copy-boot port $COPYBOOT_PORT is the prod port - refusing to boot on top of prod"
fi

command -v ssh >/dev/null 2>&1 || die "ssh not found - this leg needs the key-based ssh to $REMOTE_HOST"

# --- 0. pre-flight -----------------------------------------------------------
step 0/8 "pre-flight: live version on $REMOTE_HOST"
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

# --- 1. fetch ----------------------------------------------------------------
step 1/8 "fetch $VERSION ($PLATFORM) from the GitHub release"
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
echo "  staged at $REMOTE_STAGE (the old unit keeps serving until the stop below)"

# --- 2. stop -----------------------------------------------------------------
step 2/8 "stop unit omega (the copy is taken from a stopped unit)"
ssh_run stop <<'REMOTE' || die "systemctl --user stop omega failed"
systemctl --user stop omega
REMOTE
UNIT_STOPPED=1
echo "  stopped"

# --- 3. copy-boot ------------------------------------------------------------
# The gate this leg exists for: boot the staged binary over a COPY of the live
# home before anything is placed. A red here starts the old binary again and exits.
step 3/8 "copy-boot: $VERSION against a copy of $REMOTE_HOME (port $COPYBOOT_PORT, ${COPYBOOT_TIMEOUT}s bound)"
COPYBOOT_OUT=""
if ! COPYBOOT_OUT=$(ssh_run copyboot \
	"DEPLOY_VERSION=$VERSION" \
	"DEPLOY_PLATFORM=$PLATFORM" \
	"DEPLOY_HOME=$REMOTE_HOME" \
	"DEPLOY_STAGE=$REMOTE_STAGE" \
	"DEPLOY_COPY_PORT=$COPYBOOT_PORT" \
	"DEPLOY_COPY_TIMEOUT=$COPYBOOT_TIMEOUT" \
	"DEPLOY_COPY_ROOT=$REMOTE_COPY_ROOT" \
	"DEPLOY_REGRESSION_SESSION=$REGRESSION_SESSION" <<'REMOTE'
copy=$(mktemp -d "$DEPLOY_COPY_ROOT/omega-copyboot.XXXXXX") \
	|| { echo "copy-boot: cannot make a copy home under $DEPLOY_COPY_ROOT" >&2; exit 1; }
log="$DEPLOY_COPY_ROOT/omega-copyboot.log"
boot_pid=""
pre=""
baks_before=""
cleanup() {
	if [ -n "$boot_pid" ] && kill -0 "$boot_pid" 2>/dev/null; then
		kill "$boot_pid" 2>/dev/null || true
		wait "$boot_pid" 2>/dev/null || true
	fi
	rm -f "$pre" "$baks_before"
	rm -rf "$copy"
}
trap cleanup EXIT

# The copy: the whole home minus logs/ and backups/. The unit is stopped, so the
# files are a consistent snapshot - no torn JSONL append, no live WAL.
tar -C "$DEPLOY_HOME" --exclude=./logs --exclude=./backups -cf - . \
	| tar -C "$copy" -xf - \
	|| { echo "copy-boot: copying $DEPLOY_HOME to $copy failed" >&2; exit 1; }
[ -d "$copy/data" ] || { echo "copy-boot: the copy carries no data/ directory" >&2; exit 1; }
legacy=0
if [ -f "$copy/data/omega.db" ]; then
	legacy=1
fi

# The pre-boot hashes: every session file the copy already carries.
pre="$DEPLOY_COPY_ROOT/omega-copyboot.pre.sha"
: >"$pre"
if [ -d "$copy/data/sessions" ]; then
	(cd "$copy/data/sessions" && find . -type f -print | sort | xargs -r sha256sum) >"$pre"
fi
pre_count=$(wc -l <"$pre")
# Archives already present, by name: a green migration renames the store to a NEW
# dated name (rename keeps the source's mtime, so time cannot tell them apart).
baks_before="$DEPLOY_COPY_ROOT/omega-copyboot.baks"
: >"$baks_before"
for f in "$copy"/data/omega.db.*.bak; do
	[ -f "$f" ] && printf '%s\n' "${f##*/}" >>"$baks_before"
done

bin="$DEPLOY_STAGE/omega-$DEPLOY_VERSION-$DEPLOY_PLATFORM/omega"
[ -x "$bin" ] || { echo "copy-boot: staged binary missing: $bin" >&2; exit 1; }
OMEGA_HOME="$copy" OMEGA_PORT="$DEPLOY_COPY_PORT" "$bin" serve >"$log" 2>&1 &
boot_pid=$!

deadline=$((SECONDS + DEPLOY_COPY_TIMEOUT))
ready=0
last=""
while :; do
	body=$(curl -fsS -m 3 "http://127.0.0.1:$DEPLOY_COPY_PORT/health" 2>/dev/null) || body=""
	if [ -n "$body" ]; then
		last=$body
		st=$(printf '%s' "$body" | sed -n 's/.*"status"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
		ver=$(printf '%s' "$body" | sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
		if [ "$st" = ok ] && [ "$ver" = "$DEPLOY_VERSION" ]; then
			ready=1
			break
		fi
	fi
	if [ "$SECONDS" -ge "$deadline" ]; then
		break
	fi
	sleep 1
done
if [ "$ready" -ne 1 ]; then
	echo "copy-boot: health never answered ok at $DEPLOY_VERSION within ${DEPLOY_COPY_TIMEOUT}s (last body: ${last:-<no answer>})" >&2
	echo "--- copy-boot log tail ---" >&2
	tail -40 "$log" >&2 || true
	exit 1
fi

# A boot must never rewrite a session file that was already there: that is the
# failure class this step exists to catch, and health cannot see it.
while read -r sum rel; do
	# MSYS coreutils marks a binary-mode path with a leading '*' and GNU/Linux does
	# not; strip it so the same payload reads both hosts.
	rel=${rel#\*}
	[ -e "$copy/data/sessions/$rel" ] \
		|| { echo "copy-boot: the boot removed a session file: $rel" >&2; exit 1; }
	now=$(sha256sum "$copy/data/sessions/$rel" | cut -d' ' -f1)
	if [ "$now" != "$sum" ]; then
		echo "copy-boot: the boot REWROTE $rel (sha256 $now, was $sum)" >&2
		exit 1
	fi
done <"$pre"
if [ "$legacy" -eq 0 ]; then
	now_count=$(find "$copy/data/sessions" -type f -print 2>/dev/null | wc -l)
	if [ "$now_count" != "$pre_count" ]; then
		echo "copy-boot: the boot added session files over a non-legacy store" >&2
		exit 1
	fi
fi

if [ "$legacy" -eq 1 ]; then
	# The store was pre-JSONL: the migration must have gone green, which means the
	# source is archived aside under a name the copy did not carry before, and the
	# known-broken row is now a file.
	archived=""
	for f in "$copy"/data/omega.db.*.bak; do
		[ -f "$f" ] || continue
		if ! grep -qxF "${f##*/}" "$baks_before"; then
			archived=$f
			break
		fi
	done
	[ -n "$archived" ] \
		|| { echo "copy-boot: the legacy store was never archived - the migration did not run" >&2; exit 1; }
	file="$copy/data/sessions/$DEPLOY_REGRESSION_SESSION.jsonl"
	[ -f "$file" ] \
		|| { echo "copy-boot: no migrated file for $DEPLOY_REGRESSION_SESSION" >&2; exit 1; }
	hdr=$(head -n 1 "$file")
	c=$(printf '%s' "$hdr" | sed -n 's/.*"created_at":"\([^"]*\)".*/\1/p')
	u=$(printf '%s' "$hdr" | sed -n 's/.*"updated_at":"\([^"]*\)".*/\1/p')
	if [ -z "$c" ] || [ "$u" != "$c" ]; then
		echo "copy-boot: $DEPLOY_REGRESSION_SESSION reads back created_at [$c] updated_at [$u] - the v0.18.0 divergence" >&2
		exit 1
	fi
	echo "copy-boot: legacy migration green; $DEPLOY_REGRESSION_SESSION keeps updated_at == created_at == $c"
fi
echo "copy-boot: ok at $DEPLOY_VERSION; $pre_count session file(s) byte-identical after the boot"
REMOTE
); then
	printf '%s\n' "$COPYBOOT_OUT" >&2
	if ssh_run restart-old <<'REMOTE'
systemctl --user start omega
REMOTE
	then
		UNIT_STOPPED=0
		echo "  the OLD binary is serving again" >&2
	fi
	die "copy-boot failed - $VERSION was never placed, prod stays on the old version" \
		"the release stays published. The copy-boot log tail is above."
fi
printf '%s\n' "$COPYBOOT_OUT"
echo "  copy-boot green"

# --- 4. place ----------------------------------------------------------------
step 4/8 "place binary at $REMOTE_BIN"
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

# --- 5. start ----------------------------------------------------------------
step 5/8 "start unit omega"
ssh_run start <<'REMOTE' || die "systemctl --user start omega failed"
systemctl --user start omega
REMOTE
UNIT_STOPPED=0
echo "  started"

# --- 6. health ---------------------------------------------------------------
step 6/8 "health check: $HEALTH_URL until status ok at $VERSION (${HEALTH_TIMEOUT}s bound)"
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

# --- 7. soak stamp -----------------------------------------------------------
step 7/8 "soak stamp"
STAMP=$(date -u +%Y-%m-%dT%H:%M:%SZ)
ssh_run stamp \
	"DEPLOY_VERSION=$VERSION" \
	"DEPLOY_STAMP=$STAMP" \
	"DEPLOY_LOG=$REMOTE_LOG" <<'REMOTE' || die "appending to $REMOTE_LOG failed"
printf '%s %s deployed\n' "$DEPLOY_STAMP" "$DEPLOY_VERSION" >>"$DEPLOY_LOG"
REMOTE
echo "soak started: $VERSION at $STAMP"
echo "deploy log:   $REMOTE_LOG"
