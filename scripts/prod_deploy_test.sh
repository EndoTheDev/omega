#!/usr/bin/env bash
set -uo pipefail

# prod_deploy_test.sh - stubbed-ssh tests for scripts/prod-deploy.sh.
# Run: bash scripts/prod_deploy_test.sh
#
# No live ssh, no live pibtw: every remote leg goes through ssh, so a PATH
# stub stands in for it (same shape as factory-dispatch_test.sh's stubbed
# hermes). The stub dispatches on the step marker the SUT puts on the remote
# command line - "OMEGA_DEPLOY_STEP=<step> ... bash -s" - and answers from
# per-case files: health.pre / health.post (the version /health reports
# before and after the restart), health.rc, and <step>.rc for the mutating
# legs.
#
# Pinned here:
#   A1  the arg guard: no version -> nonzero, usage, NOTHING is touched
#   A2  a non-tag / injection-shaped version is refused the same way
#   A3  the already-live short-circuit: one health read, no stop, exit 0
#   A4  the full order and the soak stamp (stdout AND the deploy.log line)
#   A5  the health-poll timeout path exits nonzero, loudly, with the last
#       health body, and never stamps
#   A6  a failed fetch never places or starts anything
#   A7  an unreachable host is refused before the unit is stopped
#   A8  the 30s bound and the BatchMode law live in the SUT source
#
# The timeout legs run under PROD_DEPLOY_HEALTH_TIMEOUT=1 so the bounded 30s
# wait is never paid here; the production default is asserted from the SUT
# source (A8) instead of by waiting.

SUT="$(cd "$(dirname "$0")" && pwd)/prod-deploy.sh"

pass=0
fail=0
ok() { pass=$((pass + 1)); }
bad() { fail=$((fail + 1)); echo "FAIL: $1"; }

# chk <label> <command...> - the command's exit status is the verdict.
# if/then/else, never `A && ok || bad`: that shape trips shellcheck SC2015.
chk() {
	local label=$1
	shift
	if "$@"; then
		ok
	else
		bad "$label"
	fi
}

# has <haystack> <needle> - fixed-string containment
has() { grep -qF -- "$2" <<<"$1"; }
# has_re <haystack> <ere>
has_re() { grep -Eq -- "$2" <<<"$1"; }
# lacks <haystack> <needle>
lacks() { ! grep -q -- "$2" <<<"$1"; }

FX=$(mktemp -d "${TMPDIR:-/tmp}/proddeploy-fx.XXXXXX")
# The stub rides PATH, so its directory must be a POSIX path: an MSYS PATH
# entry in C:/ form splits at the drive-letter colon and the stub never
# resolves (the real ssh answers instead).
FX_POSIX=$(cd "$FX" && pwd)
mkdir -p "$FX/bin"

cat >"$FX/bin/ssh" <<'STUB'
#!/usr/bin/env bash
# fake ssh: the remote command is always the LAST argument -
# <ssh opts...> <host> "OMEGA_DEPLOY_STEP=<step> [VAR=value ...] bash -s"
set -uo pipefail
cmd=${*: -1}
step=$(printf '%s' "$cmd" | sed -n 's/^OMEGA_DEPLOY_STEP=\([A-Za-z0-9_-]*\).*/\1/p')
if [ -z "$step" ]; then
	echo "fake ssh: no OMEGA_DEPLOY_STEP marker in [$cmd]" >&2
	exit 99
fi
printf '%s\n' "$step" >>"${STUB_LOG:?STUB_LOG unset}"
printf '%s\n' "$cmd" >"${STUB_DIR:?STUB_DIR unset}/cmd.$step"
# Consume the remote script so the caller's pipe sees EOF.
cat >"${STUB_DIR:?STUB_DIR unset}/payload.$step"
if [ "$step" = health ]; then
	n=$(cat "$STUB_DIR/health.n" 2>/dev/null || echo 0)
	n=$((n + 1))
	printf '%s' "$n" >"$STUB_DIR/health.n"
	body=$STUB_DIR/health.pre
	if [ -f "$STUB_DIR/health.post" ] && [ "$n" -gt 1 ]; then
		body=$STUB_DIR/health.post
	fi
	rc=$(cat "$STUB_DIR/health.rc" 2>/dev/null || echo 0)
	[ -f "$body" ] && cat "$body"
	exit "$rc"
fi
rc=$(cat "$STUB_DIR/$step.rc" 2>/dev/null || echo 0)
exit "$rc"
STUB
chmod +x "$FX/bin/ssh"

# cs <tag> - create the per-case fixture dir without running the SUT
cs() {
	CASE=$FX/case-$1
	mkdir -p "$CASE"
	: >"$CASE/log"
}

# run <tag> [args...] - OUT, RC, STEPS (the remote step order) and CASE dir
run() {
	local tag=$1
	shift
	cs "$tag"
	OUT=$(PATH="$FX_POSIX/bin:$PATH" STUB_DIR="$CASE" STUB_LOG="$CASE/log" \
		PROD_DEPLOY_HEALTH_TIMEOUT="${HEALTH_TO:-30}" \
		bash "$SUT" "$@" 2>&1)
	RC=$?
	STEPS=$(paste -sd' ' - <"$CASE/log")
}

# health_at <version>: what /health reports (pre = before the restart)
health_at() { printf '{"status":"ok","version":"%s"}' "$1" >"$CASE/health.pre"; }
# health_after <version>: what /health reports once the new unit is up
health_after() { printf '{"status":"ok","version":"%s"}' "$1" >"$CASE/health.post"; }
# fail_step <step> <rc>
fail_step() { echo "$2" >"$CASE/$1.rc"; }
# payload <step> / cmdline <step>
payload() { cat "$CASE/payload.$1"; }
cmdline() { cat "$CASE/cmd.$1"; }

# --- A1: no version -> usage, nonzero, nothing touched -----------------------
run A1
chk "A1: bare call exited 0" test "$RC" -ne 0
chk "A1: usage line missing - got [$OUT]" \
	has "$OUT" "usage: scripts/prod-deploy.sh <version-tag>"
chk "A1: touched the remote with no version: [$STEPS]" test -z "$STEPS"

# --- A2: non-tag and injection-shaped args are refused -----------------------
# (a legal tag is proven accepted by A4's full run; v1.0.0-alpha and
# v0.15.2-nightly.20261107 are legal shapes and ride the optional suffix)
for arg in 0.17.3 v1.2 1.2.3 v1 v1.0.0- v1.0.0/../x; do
	run A2-arg "$arg"
	chk "A2: '$arg' accepted" test "$RC" -ne 0
	chk "A2: '$arg' not refused by the shape guard - got [$OUT]" \
		has "$OUT" "not a version tag"
	chk "A2: '$arg' touched the remote: [$STEPS]" test -z "$STEPS"
done
# the shell-metacharacter probe: a real tag with a command appended
run A2-inj 'v1.0.0; rm -rf /'
chk "A2: injection-shaped arg accepted" test "$RC" -ne 0
chk "A2: injection-shaped arg not refused by the shape guard" \
	has "$OUT" "not a version tag"
chk "A2: injection-shaped arg touched the remote: [$STEPS]" test -z "$STEPS"

# --- A3: already live -> one health read, exit 0 -----------------------------
cs A3
health_at v0.99.0
run A3 v0.99.0
chk "A3: already-live run exited $RC" test "$RC" -eq 0
chk "A3: 'already live: v0.99.0' missing - got [$OUT]" \
	has "$OUT" "already live: v0.99.0"
chk "A3: want exactly one health read, got [$STEPS]" test "$STEPS" = health

# --- A4: full deploy order + the soak stamp ----------------------------------
cs A4
health_at v0.1.0
health_after v0.99.0
run A4 v0.99.0
chk "A4: happy path exited $RC - got [$OUT]" test "$RC" -eq 0
chk "A4: step order wrong - got [$STEPS]" \
	test "$STEPS" = "health stop fetch place start health stamp"
chk "A4: soak stamp missing the version - got [$OUT]" \
	has "$OUT" "soak started: v0.99.0 at "
chk "A4: soak stamp missing an ISO-8601 UTC time - got [$OUT]" \
	has_re "$OUT" 'soak started: v0\.99\.0 at [0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z'
chk "A4: deploy log path not reported - got [$OUT]" \
	has "$OUT" "deploy log:   /home/endo/.omega/deploy.log"
chk "A4: stamp payload does not append to the deploy log" \
	has "$(payload stamp)" ">>\"\$DEPLOY_LOG\""
chk "A4: stamp payload line lacks the status word" has "$(payload stamp)" deployed
chk "A4: place step does not carry the probed OMEGA_HOME" \
	has "$(cmdline place)" "DEPLOY_HOME=/home/endo/.omega"
chk "A4: place payload does not install the probed binary path" \
	has "$(payload place)" "install -m 0755 \"\$src\" \"\$DEPLOY_HOME/omega\""
chk "A4: place payload keeps no previous-binary backup" \
	has "$(payload place)" omega.bak
chk "A4: fetch payload does not build the release asset name" \
	has "$(payload fetch)" "omega-\$DEPLOY_VERSION-\$DEPLOY_PLATFORM.tar.gz"
chk "A4: fetch payload does not verify the checksum" has "$(payload fetch)" sha256sum
chk "A4: fetch step does not carry the release download base" \
	has "$(cmdline fetch)" "DEPLOY_BASE=https://github.com/EndoTheDev/omega/releases/download"
chk "A4: health step does not poll the probed endpoint" \
	has "$(cmdline health)" "DEPLOY_HEALTH_URL=http://127.0.0.1:8099/health"
chk "A4: health payload has no bounded curl" has "$(payload health)" "curl -fsS -m 3"
chk "A4: stop payload is not the probed unit name" \
	has "$(payload stop)" "systemctl --user stop omega"
chk "A4: start payload is not the probed unit name" \
	has "$(payload start)" "systemctl --user start omega"

# --- A5: health never reports the new version -> bounded timeout, nonzero ----
HEALTH_TO=1
cs A5
health_at v0.1.0
health_after v0.1.0
run A5 v0.99.0
unset HEALTH_TO
chk "A5: timeout path exited 0" test "$RC" -ne 0
chk "A5: timeout message missing - got [$OUT]" \
	has "$OUT" "health check timed out after 1s"
chk "A5: last health body not quoted in the failure - got [$OUT]" \
	has "$OUT" '"version":"v0.1.0"'
chk "A5: stamped a deploy that never came up: [$STEPS]" lacks "$STEPS" stamp
chk "A5: no rollback line for a binary that is live but mute" \
	has "$OUT" "Roll back with: ssh pibtw 'cp /home/endo/.omega/omega.bak /home/endo/.omega/omega"
polls=$(grep -c '^health$' "$CASE/log" || true)
chk "A5: want >=2 health polls before timeout, got ${polls:-0}" \
	test "${polls:-0}" -ge 2

# --- A6: fetch failure never places or starts --------------------------------
cs A6
health_at v0.1.0
fail_step fetch 1
run A6 v0.99.0
chk "A6: failed fetch exited 0" test "$RC" -ne 0
chk "A6: fetch failure not named - got [$OUT]" \
	has "$OUT" "fetch or checksum verify failed for v0.99.0"
chk "A6: placed after a failed fetch: [$STEPS]" lacks "$STEPS" place
chk "A6: started after a failed fetch: [$STEPS]" lacks "$STEPS" start
chk "A6: no recovery hint after a failure with the unit down" \
	has "$OUT" "the unit is stopped"

# --- A7: unreachable host -> refused before the stop -------------------------
cs A7
echo 255 >"$CASE/health.rc"
run A7 v0.99.0
chk "A7: unreachable host exited 0" test "$RC" -ne 0
chk "A7: unreachable host not named - got [$OUT]" \
	has "$OUT" "cannot reach pibtw over ssh"
chk "A7: stopped the unit on an unreachable host: [$STEPS]" test "$STEPS" = health

# --- A8: the production bound lives in the SUT, not only in the test ---------
SUT_SRC=$(cat "$SUT")
chk "A8: SUT lost its 30s health bound default" \
	has_re "$SUT_SRC" '^HEALTH_TIMEOUT=\$\{PROD_DEPLOY_HEALTH_TIMEOUT:-30\}$'
chk "A8: SUT lost the passwordless-only ssh law" has "$SUT_SRC" "BatchMode=yes"

echo "prod_deploy_test.sh: $pass passed, $fail failed"
[ "$fail" -eq 0 ]
