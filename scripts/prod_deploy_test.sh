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
# The copy-boot legs are executed, not just inspected: a per-case exec.<step>
# marker makes the stub run the payload for real against a fixture home, with
# a stub curl (so health is an answer in a file) and a fake binary. The rewrite
# case and the no-health case are therefore the SUT's own shell logic under
# test, not a string grep.
#
# Pinned here:
#   A1  the arg guard: no version -> nonzero, usage, NOTHING is touched
#   A2  a non-tag / injection-shaped version is refused the same way
#   A3  the already-live short-circuit: one health read, no stop, exit 0
#   A4  the full EIGHT-step order and the soak stamp (stdout AND the
#       deploy.log line), plus the copy-boot payload's real-shape checks
#   A5  the health-poll timeout path exits nonzero, loudly, with the last
#       health body, and never stamps
#   A6  a failed fetch never stops, places or starts anything
#   A7  an unreachable host is refused before the unit is stopped
#   A8  the 30s bound and the BatchMode law live in the SUT source
#   C1  a GREEN copy-boot, executed: the staged binary boots a copy of the
#       home, health answers at the target, the migration's record checks out,
#       and only then do place/start/stamp run
#   C2  a copy-boot whose health never answers: abort BEFORE place, restart the
#       OLD binary, exit nonzero, stamp nothing
#   C3  a copy-boot whose boot rewrites one session file: same abort shape, and
#       the failure names the file and both hashes
#   C4  the copy lives under the copy root (/tmp by default) and never in the
#       live home
#   C5  the copy-boot port is not the prod port (default 18099 vs 8099, and
#       8099 is refused before any ssh)
#
# The timeout legs run under PROD_DEPLOY_HEALTH_TIMEOUT=1 and
# PROD_DEPLOY_COPYBOOT_TIMEOUT=1 so no bounded production wait is paid here;
# the production defaults are asserted from the SUT source (A8, C5) instead.

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
# lacks_word <haystack> <word> - a whole step token (restart-old contains "start")
lacks_word() { ! grep -Eq "(^| )$2( |$)" <<<"$1"; }

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
if [ -f "$STUB_DIR/exec.$step" ] && [ "$rc" -eq 0 ]; then
	# Execute the remote payload for real against the case's fixture home: the
	# remote paths in the marker are rewritten to the fixture, everything else
	# (the port, the timeout, the copy root) is the SUT's own value.
	prefix=$(printf '%s' "${cmd% bash -euo pipefail -s}" |
		sed -e "s|DEPLOY_HOME=[^ ]*|DEPLOY_HOME=$STUB_DIR/home|" \
			-e "s|DEPLOY_STAGE=[^ ]*|DEPLOY_STAGE=$STUB_DIR/stage|")
	env $prefix bash -euo pipefail -s <"$STUB_DIR/payload.$step"
	exit $?
fi
exit "$rc"
STUB
chmod +x "$FX/bin/ssh"

# fake curl: the copy-boot's health poll, answered from a file. Absent file =
# nothing is listening (exit 7, exactly what real curl says).
cat >"$FX/bin/curl" <<'CURLSTUB'
#!/usr/bin/env bash
set -uo pipefail
if [ -n "${STUB_DIR:-}" ] && [ -f "$STUB_DIR/health.copy" ]; then
	cat "$STUB_DIR/health.copy"
	exit 0
fi
exit 7
CURLSTUB
chmod +x "$FX/bin/curl"

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
		PROD_DEPLOY_COPYBOOT_TIMEOUT="${COPY_TO:-60}" \
		PROD_DEPLOY_COPYBOOT_ROOT="${COPY_ROOT:-/tmp}" \
		PROD_DEPLOY_COPYBOOT_PORT="${COPY_PORT:-18099}" \
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

# copyboot_fixture <id> - a fixture home + staging tree the copy-boot payload can
# work on, plus a fake binary that behaves like the shipped one.
#   mode=green   the staged binary migrates the legacy store and serves
#   mode=slow    it serves nothing (health never answers)
#   mode=rewrite it serves, but appends to an existing session file first
copyboot_fixture() {
	local mode=$1
	mkdir -p "$CASE/home/config" "$CASE/home/data/sessions" "$CASE/stage/omega-v0.99.0-linux_arm64"
	printf 'provider:\n  type: ollama\n  model_name: dummy\n  host: http://127.0.0.1:9\n' \
		>"$CASE/home/config/config.yaml"
	# a non-legacy home: jsonl files, no omega.db (the legacy branch is C1b below)
	printf '%s\n' '{"v":1,"kind":"session","session":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","created_at":"2026-10-01T00:00:00Z","updated_at":"2026-10-01T00:00:00Z"}' \
		>"$CASE/home/data/sessions/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.jsonl"
	case $mode in
	green)
		# a legacy store that migrates cleanly: the source archived aside and the
		# known-broken record written with equal stamps
		printf 'not-a-db' >"$CASE/home/data/omega.db"
		cat >"$CASE/stage/omega-v0.99.0-linux_arm64/omega" <<'FAKEBIN'
#!/usr/bin/env bash
set -uo pipefail
cp "$OMEGA_HOME/data/omega.db" "$OMEGA_HOME/data/omega.db.2026-11-25.bak"
rm -f "$OMEGA_HOME/data/omega.db"
printf '%s\n' '{"v":1,"kind":"session","session":"72863811adb93cd5ca47642c9b8393f6","created_at":"2026-10-12T19:45:35.496622592Z","updated_at":"2026-10-12T19:45:35.496622592Z"}' \
	>"$OMEGA_HOME/data/sessions/72863811adb93cd5ca47642c9b8393f6.jsonl"
printf '%s\n' "$OMEGA_HOME" >"$STUB_DIR/boot.home"
sleep 30
FAKEBIN
		;;
	slow)
		printf 'not-a-db' >"$CASE/home/data/omega.db"
		cat >"$CASE/stage/omega-v0.99.0-linux_arm64/omega" <<'FAKEBIN'
#!/usr/bin/env bash
set -uo pipefail
printf '%s\n' "$OMEGA_HOME" >"$STUB_DIR/boot.home"
sleep 30
FAKEBIN
		;;
	rewrite)
		printf 'not-a-db' >"$CASE/home/data/omega.db"
		cat >"$CASE/stage/omega-v0.99.0-linux_arm64/omega" <<'FAKEBIN'
#!/usr/bin/env bash
set -uo pipefail
printf '%s\n' "$OMEGA_HOME" >"$STUB_DIR/boot.home"
printf '%s\n' '{"v":1,"kind":"meta","session":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}' \
	>>"$OMEGA_HOME/data/sessions/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.jsonl"
sleep 30
FAKEBIN
		;;
	esac
	chmod +x "$CASE/stage/omega-v0.99.0-linux_arm64/omega"
	printf '%s\n' 'copy' >"$CASE/exec.copyboot"
}

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

# --- A4: full deploy order (eight steps) + the soak stamp --------------------
cs A4
health_at v0.1.0
health_after v0.99.0
run A4 v0.99.0
chk "A4: happy path exited $RC - got [$OUT]" test "$RC" -eq 0
chk "A4: step order wrong - got [$STEPS]" \
	test "$STEPS" = "health fetch stop copyboot place start health stamp"
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
# the copy-boot payload's real-shape checks
chk "A4: copy-boot step does not carry the target version" \
	has "$(cmdline copyboot)" "DEPLOY_VERSION=v0.99.0"
chk "A4: copy-boot step does not carry the staged tree" \
	has "$(cmdline copyboot)" "DEPLOY_STAGE=/tmp/omega-deploy.stage"
chk "A4: copy-boot step does not carry the copy port" \
	has "$(cmdline copyboot)" "DEPLOY_COPY_PORT=18099"
chk "A4: copy-boot payload does not boot the STAGED binary" \
	has "$(payload copyboot)" 'bin="$DEPLOY_STAGE/omega-$DEPLOY_VERSION-$DEPLOY_PLATFORM/omega"'
chk "A4: copy-boot payload does not exclude logs/ and backups/" \
	has "$(payload copyboot)" "--exclude=./logs --exclude=./backups"
chk "A4: copy-boot payload does not hash the session files" \
	has "$(payload copyboot)" sha256sum
chk "A4: copy-boot payload does not name the known-broken session" \
	has "$(payload copyboot)" "DEPLOY_REGRESSION_SESSION.jsonl"
chk "A4: copy-boot payload does not poll the copy port" \
	has "$(payload copyboot)" 'http://127.0.0.1:$DEPLOY_COPY_PORT/health'
chk "A4: copy-boot payload does not bound its health wait" \
	has "$(payload copyboot)" 'deadline=$((SECONDS + DEPLOY_COPY_TIMEOUT))'
chk "A4: copy-boot payload boots before the home is touched" \
	has "$(payload copyboot)" 'OMEGA_HOME="$copy" OMEGA_PORT="$DEPLOY_COPY_PORT" "$bin" serve'

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
chk "A5: stamped a deploy that never came up: [$STEPS]" lacks_word "$STEPS" stamp
chk "A5: no rollback line for a binary that is live but mute" \
	has "$OUT" "Roll back with: ssh pibtw 'cp /home/endo/.omega/omega.bak /home/endo/.omega/omega"
polls=$(grep -c '^health$' "$CASE/log" || true)
chk "A5: want >=2 health polls before timeout, got ${polls:-0}" \
	test "${polls:-0}" -ge 2

# --- A6: fetch failure never stops, places or starts ------------------------
cs A6
health_at v0.1.0
fail_step fetch 1
run A6 v0.99.0
chk "A6: failed fetch exited 0" test "$RC" -ne 0
chk "A6: fetch failure not named - got [$OUT]" \
	has "$OUT" "fetch or checksum verify failed for v0.99.0"
chk "A6: stopped the unit on a failed fetch: [$STEPS]" lacks_word "$STEPS" stop
chk "A6: placed after a failed fetch: [$STEPS]" lacks_word "$STEPS" place
chk "A6: started after a failed fetch: [$STEPS]" lacks_word "$STEPS" start
chk "A6: copy-booted after a failed fetch: [$STEPS]" lacks_word "$STEPS" copyboot
chk "A6: claims the unit is stopped when it never was - got [$OUT]" \
	lacks "$OUT" "the unit is stopped"

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

# --- C1: a GREEN copy-boot, executed for real, then the flip -----------------
COPY_ROOT=$FX/case-C1/copyroot
mkdir -p "$COPY_ROOT"
cs C1
health_at v0.1.0
health_after v0.99.0
copyboot_fixture green
printf '{"status":"ok","version":"v0.99.0"}' >"$CASE/health.copy"
run C1 v0.99.0
unset COPY_ROOT
chk "C1: green copy-boot run exited $RC - got [$OUT]" test "$RC" -eq 0
chk "C1: the order skipped or reordered the copy-boot - got [$STEPS]" \
	test "$STEPS" = "health fetch stop copyboot place start health stamp"
chk "C1: the executed copy-boot did not report green - got [$OUT]" \
	has "$OUT" "copy-boot: ok at v0.99.0"
chk "C1: the executed copy-boot did not see the legacy migration - got [$OUT]" \
	has "$OUT" "copy-boot: legacy migration green"
chk "C1: the deployed binary was stamped despite the gate - got [$OUT]" \
	has "$OUT" "soak started: v0.99.0"

# --- C2: no health on the copy -> abort before place, restart the old binary --
COPY_ROOT=$FX/case-C2/copyroot
COPY_TO=1
mkdir -p "$COPY_ROOT"
cs C2
health_at v0.1.0
copyboot_fixture slow
run C2 v0.99.0
unset COPY_ROOT COPY_TO
chk "C2: a dead copy-boot exited 0" test "$RC" -ne 0
chk "C2: the failure is not named - got [$OUT]" \
	has "$OUT" "health never answered ok at v0.99.0 within 1s"
chk "C2: the copy-boot log tail was not printed - got [$OUT]" \
	has "$OUT" "--- copy-boot log tail ---"
chk "C2: placed after a dead copy-boot: [$STEPS]" lacks_word "$STEPS" place
chk "C2: started the new binary after a dead copy-boot: [$STEPS]" lacks_word "$STEPS" start
chk "C2: stamped a deploy that never passed the gate: [$STEPS]" lacks_word "$STEPS" stamp
chk "C2: did not restart the OLD binary - got [$STEPS]" has "$STEPS" "copyboot restart-old"
chk "C2: the abort did not say prod stays on the old version - got [$OUT]" \
	has "$OUT" "prod stays on the old version"
chk "C2: the abort did not confirm the old binary is serving - got [$OUT]" \
	has "$OUT" "the OLD binary is serving again"

# --- C3: a boot that rewrites a session file aborts the same way -------------
COPY_ROOT=$FX/case-C3/copyroot
mkdir -p "$COPY_ROOT"
cs C3
health_at v0.1.0
copyboot_fixture rewrite
printf '{"status":"ok","version":"v0.99.0"}' >"$CASE/health.copy"
run C3 v0.99.0
unset COPY_ROOT
chk "C3: a rewriting boot exited 0" test "$RC" -ne 0
chk "C3: the rewrite is not named - got [$OUT]" \
	has "$OUT" "the boot REWROTE ./aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.jsonl"
chk "C3: the rewrite failure does not carry both hashes - got [$OUT]" \
	has_re "$OUT" 'sha256 [0-9a-f]{64}, was [0-9a-f]{64}'
chk "C3: placed after a rewriting boot: [$STEPS]" lacks_word "$STEPS" place
chk "C3: stamped after a rewriting boot: [$STEPS]" lacks_word "$STEPS" stamp
chk "C3: did not restart the OLD binary - got [$STEPS]" has "$STEPS" restart-old

# --- C4: the copy lives under the copy root, never the live home -------------
chk "C4: default copy root is not /tmp - got [$SUT_SRC]" \
	has_re "$SUT_SRC" '^REMOTE_COPY_ROOT=\$\{PROD_DEPLOY_COPYBOOT_ROOT:-/tmp\}$'
chk "C4: the copy is not made under the copy root" \
	has "$(payload copyboot)" 'mktemp -d "$DEPLOY_COPY_ROOT/omega-copyboot.XXXXXX"'
chk "C4: the copy is not removed on the way out" \
	has "$(payload copyboot)" 'rm -rf "$copy"'
chk "C4: the payload copies the live home INTO the copy, not the other way" \
	has "$(payload copyboot)" 'tar -C "$DEPLOY_HOME" --exclude=./logs --exclude=./backups -cf - .'
BOOT_HOME=$(cat "$FX/case-C1/boot.home" 2>/dev/null || true)
chk "C4: the executed boot did not record its home" test -n "$BOOT_HOME"
chk "C4: the copy-boot ran inside the LIVE home: [$BOOT_HOME]" \
	lacks "$BOOT_HOME" "/home/endo/.omega"
chk "C4: the copy-boot home is not under the copy root: [$BOOT_HOME]" \
	has "$BOOT_HOME" "case-C1/copyroot/omega-copyboot."

# --- C5: the copy-boot port is not the prod port -----------------------------
chk "C5: SUT lost the 18099 copy-boot port default - got [$SUT_SRC]" \
	has_re "$SUT_SRC" '^COPYBOOT_PORT=\$\{PROD_DEPLOY_COPYBOOT_PORT:-18099\}$'
cs C5
COPY_PORT=8099
run C5 v0.99.0
unset COPY_PORT
chk "C5: booting the copy on the prod port was allowed" test "$RC" -ne 0
chk "C5: the prod-port refusal is not named - got [$OUT]" \
	has "$OUT" "is the prod port"
chk "C5: touched the remote before refusing the prod port: [$STEPS]" test -z "$STEPS"

echo "prod_deploy_test.sh: $pass passed, $fail failed"
[ "$fail" -eq 0 ]
