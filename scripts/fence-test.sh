#!/usr/bin/env bash
set -u   # no -e: refused operations are expected exits, the counters decide

# fence-test.sh - shell checks for bin/shims/git, the machine fence
# (card t_8b32a09a). Run: bash scripts/fence-test.sh
#
# Fenced mode stamps OMEGA_WORKER=1: push refuses, every dev/master
# landing refuses, ordinary work forwards. Unstamped mode forwards to
# the real git byte-for-byte - proved by a push that must actually
# land on a local bare remote.

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SHIM="$ROOT/bin/shims/git"
[ -f "$SHIM" ] || { echo "FAIL: shim missing at $SHIM"; exit 1; }

pass=0; fail=0
ok()    { pass=$((pass + 1)); }
bad()   { fail=$((fail + 1)); echo "FAIL: $1" >&2; }

# Fixture: a repo with disjoint master/dev/feat branches (separate files,
# merges never conflict) and a local bare origin.
FIX="$(mktemp -d)"
trap 'rm -rf "$FIX"' EXIT
git -C "$FIX" -c init.defaultBranch=master init -q repo
R="$FIX/repo"
G() { git -C "$R" -c user.name=fence -c user.email=fence@local "$@"; }  # real git, setup + asserts
S() { env -C "$R" "$SHIM" "$@"; }                 # shim, unstamped
F() { env -C "$R" OMEGA_WORKER=1 "$SHIM" "$@"; }  # shim, fenced

# Repo-local identity: the unstamped passthrough runs bare git with no -c
# flags (the `merge dev` below is a real merge commit), and a CI runner has
# no global user.name/email - a dev box does. Seed the fixture's identity
# so the commit does not depend on global config.

G config user.name fence
G config user.email fence@local
G commit -q --allow-empty -m base
G branch -q dev HEAD
G branch -q feat HEAD
G checkout -q dev
echo dev > "$R/dev.txt"; G add dev.txt;   G commit -q -m devline
G checkout -q feat
echo feat > "$R/feat.txt"; G add feat.txt; G commit -q -m featline
DEVSHA="$(G rev-parse dev)"
G checkout -q -b pushless
git init -q --bare "$FIX/origin.git"
G remote add origin "$FIX/origin.git"

must_block() {  # label, then the fenced invocation
  local label="$1"; shift
  out=$("$@" 2>&1); rc=$?
  [ "$rc" -ne 0 ]         && ok || bad "$label: exited 0 (must refuse)"
  printf '%s' "$out" | grep -q "FENCE:" && ok || bad "$label: refusal did not name the law: $out"
}
must_pass() {   # label, then the invocation
  local label="$1"; shift
  "$@" >/dev/null 2>&1 && ok || bad "$label: refused an ordinary operation (exit $?)"
}

O() { git -C "$FIX/origin.git" "$@"; }    # asserts on the bare remote

# --- fenced: push is always refused, and nothing lands ---
must_block "push"                F push origin pushless:refs/heads/attpush
O rev-parse --verify --quiet refs/heads/attpush >/dev/null 2>&1 && \
  bad "push: the attempt LANDED on origin - the fence lied" || ok
must_block "push --dry-run"      F push --dry-run origin HEAD
must_block "push -C (global opt)" env -C "$FIX" OMEGA_WORKER=1 "$SHIM" -C "$R" push origin pushless
must_block "merge dev"           F merge dev
must_block "merge --ff-only master" F merge --ff-only master
must_block "merge origin/master" F merge origin/master
must_block "rebase master"       F rebase master
must_block "rebase onto dev"     F rebase --onto dev HEAD~1
must_block "reset --hard dev"    F reset --hard dev
must_block "reset --hard origin/master" F reset --hard origin/master
must_block "pull origin dev"     F pull origin dev
must_block "fetch dev refspec"   F fetch origin dev
must_block "fetch dev:dev"       F fetch origin +refs/heads/dev:refs/heads/dev
must_block "cherry-pick dev"     F cherry-pick dev
must_block "branch -f dev"       F branch -f dev HEAD
must_block "branch -m dev"       F branch -m dev dev-renamed
must_block "update-ref dev"      F update-ref refs/heads/dev HEAD

# a commit while dev is checked out IS a landing
F checkout -q dev
must_block "commit on dev checkout" F -c user.name=fence -c user.email=fence@local commit --allow-empty -m land
[ "$(G rev-parse dev)" = "$DEVSHA" ] && ok || bad "commit-on-dev: dev tip moved despite the refusal"

# --- fenced: ordinary work forwards ---
F checkout -q feat
must_pass "status"      F status --porcelain
must_pass "log"         F log -1 --oneline
must_pass "tag"         F tag fence-t1
must_pass "add+commit"  sh -c 'echo more >> "'"$R"'/feat.txt" && env -C "'"$R"'" OMEGA_WORKER=1 "'"$SHIM"'" add feat.txt && env -C "'"$R"'" OMEGA_WORKER=1 "'"$SHIM"'" -c user.name=fence -c user.email=fence@local commit -q -m more'
F checkout -q dev; F checkout -q feat   # unstamped-dev checkout/switch forwards

# --- unstamped: byte-for-byte passthrough, push really lands ---
must_pass "unstamped push lands" S push origin pushless:refs/heads/unfencedpush
O rev-parse --verify --quiet refs/heads/unfencedpush >/dev/null 2>&1 && \
  ok || bad "unstamped push: did not land on origin (passthrough broken)"
must_pass "unstamped merge dev"  S merge dev
must_pass "unstamped reset master" S reset --hard master
must_pass "unstamped work on dev" sh -c 'cd "'"$R"'" && git checkout -q master && git -c user.name=fence -c user.email=fence@local commit -q --allow-empty -m land && git checkout -q feat'

# a stamp of ANY value fences (fail-closed); only absence passes through
must_block "stamp '0' fences too" env -C "$R" OMEGA_WORKER=0 "$SHIM" push origin pushless

# --- the live spawn path: the gateway builds the session env (t_2d53d4fa) ------------------
# Root cause this card closed: the stamp on the `hermes kanban dispatch` invocation dies at
# the gateway boundary - the worker client only submits a claim, and the real repo-writing
# session's env is built by gateway.session_kanban.bind_worker_context inside the worker
# interpreter. Spawn through THAT path here and read the child's /proc environ, exactly as
# the probe did. Needs the hermes checkout on PATH; CI hosts without it stay green.
HERMES_DIR="$HOME/.hermes/hermes-agent"
if [ -f "$HERMES_DIR/gateway/session_kanban.py" ]; then
  PYB=""
  for p in python3 /home/endo/.hermes/tools/python-*/bin/python3; do
    PYTHONPATH="$HERMES_DIR" "$p" -c 'import hermes_cli, gateway.session_kanban' >/dev/null 2>&1 && { PYB="$p"; break; }
  done
  cat > "$FIX/bind_child.py" <<'PYEOF'
# Spawn through the real managed-worker bind, then hold so /proc can be read.
import json, os, sys, time
from pathlib import Path

# Drop the SPAWNING worker's identity: this child is a fenced card's grandchild, and hermes
# refuses kanban mutations from delegated-child contexts. It must behave as a fresh spawn.
for key in [k for k in os.environ if k.startswith("HERMES_KANBAN") or k in
            ("HERMES_SESSION_SOURCE", "HERMES_DELEGATED_CHILD", "HERMES_DELEGATED_CHILD_CONTEXT",
             "OMEGA_WORKER", "OMEGA_WORKER_CARD", "HERMES_TENANT")]:
    os.environ.pop(key, None)

home, hermes, omega = (Path(os.environ[k]) for k in ("BIND_HOME", "BIND_HERMES", "BIND_OMEGA"))
sys.path.insert(0, str(hermes))
os.environ["HERMES_HOME"] = str(home)
os.environ["HERMES_KANBAN_HOME"] = str(home / "kanban-state")
home.mkdir(parents=True, exist_ok=True)
(home / "kanban-state").mkdir(parents=True, exist_ok=True)
workspace = home / "workspace"; workspace.mkdir(parents=True, exist_ok=True)
if os.environ.get("BIND_KNOB") == "1":
    (home / "config.yaml").write_text(
        "kanban:\n  worker_fence_shim_dir: %s\n" % (omega / "bin" / "shims"))
else:
    (home / "config.yaml").write_text("kanban: {}\n")

from hermes_cli import kanban_db as kb
from hermes_cli.kanban_db_connect import connect_closing
db_path = kb.kanban_db_path(board="owned")
kb.create_board("owned")
with connect_closing(board="owned") as conn:
    tid = kb.create_task(conn, title="fence bind probe", assignee="default",
                         workspace_kind="dir", workspace_path=str(workspace))
    task = kb.claim_task(conn, tid)
context = {"task_id": task.id, "board": "owned", "db": str(db_path), "run_id": task.current_run_id,
    "claim_lock": task.claim_lock, "workspace": str(workspace), "workspaces_root": str(home / "workspaces"),
    "profile": "default", "goal_mode": False, "goal_max_turns": None, "max_runtime_seconds": None,
    "branch": None, "tenant": None}
frame = {"policy": {"source": "kanban", "kanban_json": json.dumps(context)}}
from gateway.session_kanban import bind_worker_context
bind_worker_context(frame)   # os.environ.update(env) happens in here
print("BOUND " + task.id, flush=True)
hold = int(os.environ.get("BIND_HOLD_S", "0"))
if hold:
    # Re-exec before holding: /proc/<pid>/environ shows the execve-time env block, NOT
    # glibc setenv() mutations made after it (same reason the real probe worked - the
    # live worker's env crosses an exec boundary). The re-exec carries the post-bind env.
    os.execvp(sys.executable, [sys.executable, "-c", "import time; time.sleep(%d)" % hold])
PYEOF

  BOUND_PID="" TASKID=""
  spawn_bind() {  # $1 = knob(1/0), $2 = hold seconds; sets BOUND_PID + TASKID
    local knob="$1"
    local hold="$2" out="$FIX/bind-${knob}.out"
    BIND_HOME="$FIX/home-bind-$knob" BIND_KNOB="$knob" BIND_OMEGA="$ROOT" \
    BIND_HERMES="$HERMES_DIR" BIND_HOLD_S="$hold" \
    PYTHONPATH="$HERMES_DIR" "$PYB" "$FIX/bind_child.py" >"$out" 2>"$FIX/bind-${knob}.err" &
    BOUND_PID=$!
    local until=$((SECONDS + 120))
    while [ $SECONDS -lt $until ]; do
      TASKID="$(sed -n 's/^BOUND //p' "$out" | head -1)"
      [ -n "$TASKID" ] && return 0
      kill -0 "$BOUND_PID" 2>/dev/null || break
      sleep 0.5
    done
    TASKID=""
    return 1
  }
  kill_bind() { local p="$1"; { kill -9 "$p" 2>/dev/null; wait "$p" 2>/dev/null; } 2>/dev/null; }

  if [ -z "$PYB" ]; then
    bad "no python can import the hermes tree ($HERMES_DIR) - live-path assertions cannot run"
  else
    # fenced bind: the bound env carries the stamp and the shim dir first on PATH
    if spawn_bind 1 60; then
      if [ -f "/proc/$BOUND_PID/environ" ]; then
        tr '\0' '\n' < "/proc/$BOUND_PID/environ" > "$FIX/bound.env"
      fi
      kill_bind "$BOUND_PID"
      grep -qx 'OMEGA_WORKER=1' "$FIX/bound.env" && ok || bad "bind(fenced): OMEGA_WORKER=1 absent from /proc/$BOUND_PID/environ"
      grep -qx "OMEGA_WORKER_CARD=$TASKID" "$FIX/bound.env" && ok || bad "bind(fenced): OMEGA_WORKER_CARD=$TASKID absent"
      BV="$(grep '^PATH=' "$FIX/bound.env" | sed 's/^PATH=//')"
      [ "${BV%%:*}" = "$ROOT/bin/shims" ] && ok || bad "bind(fenced): shim dir not first on bound PATH: ${BV%%:*}"
      # end-to-end: bare `git` resolved from the BOUND PATH, stamped as bound, refuses - and
      # refuses only because of this env (no manual PATH prefix in the fixture).
      BO="$(grep '^OMEGA_WORKER=' "$FIX/bound.env" | sed 's/^OMEGA_WORKER=//')"
      BC="$(grep '^OMEGA_WORKER_CARD=' "$FIX/bound.env" | sed 's/^OMEGA_WORKER_CARD=//')"
      out=$(env -i OMEGA_WORKER="$BO" OMEGA_WORKER_CARD="$BC" PATH="$BV" git -C "$R" push --dry-run origin 2>&1); rc=$?
      [ "$rc" -ne 0 ] && ok || bad "bound-env push: exited 0 (must refuse)"
      printf '%s' "$out" | grep -q "FENCE:" && ok || bad "bound-env push: refusal did not name the law: $out"
    else
      bad "bind(fenced): worker never bound the task: $(tail -c 400 "$FIX/bind-1.err" 2>/dev/null)"
      kill_bind "${BOUND_PID:-0}"
    fi
    # negative control 1: the same bind path with NO knob stamps nothing
    BOUND_PID=""
    if spawn_bind 0 15; then
      if [ -f "/proc/$BOUND_PID/environ" ]; then
        tr '\0' '\n' < "/proc/$BOUND_PID/environ" > "$FIX/bound0.env"
      fi
      kill_bind "$BOUND_PID"
      ! grep -q '^OMEGA_WORKER=' "$FIX/bound0.env" 2>/dev/null && ok || \
        bad "bind(unfenced): stamp present without the knob"
      grep -q '^HERMES_KANBAN_TASK=' "$FIX/bound0.env" 2>/dev/null && ok || \
        bad "bind(unfenced): the kanban bind did not run at all (control void)"
    else
      bad "bind(unfenced): worker never bound: $(tail -c 400 "$FIX/bind-0.err" 2>/dev/null)"
      kill_bind "${BOUND_PID:-0}"
    fi
    # negative control 2 (main-session shape): sessions without the worker bind carry no
    # stamp - the unstamped passthrough section above is that shape for env-scoped git.
  fi
else
  echo "NOTICE: hermes tree absent ($HERMES_DIR) - live-bind assertions skipped (CI host)"
fi

echo "fence-test: $pass passed, $fail failed"
[ "$fail" -eq 0 ]