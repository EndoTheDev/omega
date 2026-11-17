#!/usr/bin/env bash
set -uo pipefail

# dev-server-kill.sh - identity-verified dev-server killer (merge-pass kill-step).
#
# Why: the merge pass prunes worktrees but never the processes they leaked.
# Two live incidents in one window - 12 leaked `nuxt dev` trees (their cards
# done and merged) held the embed lane's `npm ci` hostage, and one verification
# serve squatted :18311 for twelve hours.
#
# RULINGS (Endo, 2026-11-16 - do not re-litigate):
#   - repo-internal and standalone; the tick calls it in its merge pass LATER,
#     on Endo's one-line wiring word. This script ships alone.
#   - DEFAULT IS DRY-RUN: print the verdict table, kill nothing. --apply
#     performs the kills. The dry-run output is the test surface.
#   - NEVER-KILL CLASS IS LAW: the lane serve (:8098), prod (:8099), any
#     process tied to a running/ready/review (or otherwise live) card, an
#     omega.exe whose owning card cannot be proven, and anything that cannot
#     be identity-verified. Those are KEEP or UNKNOWN, never KILL.
#
# PROBE CONSTRAINT (live evidence, 2026-11-16 tick): wmic is DEAD on this host
# build and powershell is CRON-BLOCKED, so neither may be the source. Plain
# `ps` misses Windows pids. The probe is therefore stdlib python + ctypes
# (kernel32 toolhelp snapshot, NtQueryInformationProcess + ReadProcessMemory
# for the target's PEB command line and current directory) - no psutil, no
# native tool that can be blocked. Listening ports come from `netstat -ano`,
# which is a plain console exe.
#
# Usage:
#
#   scripts/dev-server-kill.sh [--apply] [--table <file>] [--repo <dir>]
#
#   --apply          perform `taskkill /F /PID` on KILL verdicts. Without it
#                    the run is a pure dry-run (always exit 0).
#   --table <file>   judge a FIXTURE process table instead of the live host
#                    (test surface; no real process is touched and --apply is
#                    a no-op). One row per line:
#
#                      pid|name|cmdline|cwd|card-status[|ports[|ppid]]
#
#                    card-status is the owning card's status (`-` when the
#                    owner cannot be proven); the optional trailing fields are
#                    a comma-separated listening-port list (the evidence a
#                    live run reads from netstat) and the parent pid (the tree
#                    rule: a child that cannot be verified rides a KILL
#                    ancestor - the leaked `nuxt dev` tree shape). Directive
#                    lines start with `#`: `#webui-live: <id>,<id>` names the
#                    live cards that own webui work (live mode reads them from
#                    the board). No `|` may appear inside a field.
#   --repo <dir>     the checkout to attribute processes to. Default: this
#                    script's repo root, unwrapped out of `.worktrees/<card>`.
#
# Output (stdout), one line per candidate, then the summary:
#
#   KILL <pid> <evidence>
#   KEEP <pid> <evidence>
#   UNKNOWN <pid> <evidence>
#   KILL-STEP: <n> kill / <k> keep / <u> unknown
#
# A dev server is identified by its own command line (node driving nuxt /
# nuxi / vite): a node process that is NOT a dev server is reported UNKNOWN
# even inside a dead card's worktree - killing a language server or a test
# runner because it shares a directory is not this script's job. Diagnostics
# go to stderr; stdout stays the verdict table.

usage() {
  cat <<'USAGE'
dev-server-kill.sh - identity-verified dev-server killer (merge-pass kill-step).

  scripts/dev-server-kill.sh [--apply] [--table <file>] [--repo <dir>]

  --apply          perform `taskkill /F /PID` on KILL verdicts. Without it the
                   run is a pure dry-run (always exit 0).
  --table <file>   judge a FIXTURE process table instead of the live host (the
                   test surface; no real process is touched and --apply is a
                   no-op). One row per line: pid|name|cmdline|cwd|card-status
                   [|ports[|ppid]]. Directive lines start with `#`:
                   `#webui-live: id`.
  --repo <dir>     the checkout to attribute processes to. Default: this
                   script's repo root, unwrapped out of `.worktrees/<card>`.

Output: `KILL|KEEP|UNKNOWN <pid> <evidence>` per candidate, then
`KILL-STEP: <n> kill / <k> keep / <u> unknown`.
USAGE
}

table=""
repo_arg=""
apply=0
while [ $# -gt 0 ]; do
  case "$1" in
  --apply)
    apply=1
    ;;
  --table)
    [ $# -ge 2 ] || {
      echo "dev-server-kill: --table needs a file" >&2
      exit 2
    }
    table="$2"
    shift
    ;;
  --repo)
    [ $# -ge 2 ] || {
      echo "dev-server-kill: --repo needs a directory" >&2
      exit 2
    }
    repo_arg="$2"
    shift
    ;;
  -h | --help)
    usage
    exit 0
    ;;
  *)
    echo "dev-server-kill: unknown argument: $1" >&2
    usage >&2
    exit 2
    ;;
  esac
  shift
done

# The probe is stdlib python (3.x): no psutil, no third-party module.
py=""
for cand in "${DEV_SERVER_KILL_PYTHON:-}" python3 python; do
  [ -n "$cand" ] || continue
  if command -v "$cand" >/dev/null 2>&1; then
    py="$cand"
    break
  fi
done
if [ -z "$py" ]; then
  echo "dev-server-kill: no python interpreter found (set DEV_SERVER_KILL_PYTHON)" >&2
  exit 2
fi

# A path handed to the native interpreter must be Windows-visible: MSYS /tmp
# is not a path python can open.
winpath() {
  if command -v cygpath >/dev/null 2>&1; then
    cygpath -m "$1"
  else
    printf '%s\n' "$1"
  fi
}

script_dir="$(cd "$(dirname "$0")" && pwd)"
root="$(cd "$script_dir/.." && pwd)"
if [ -n "$repo_arg" ]; then
  if [ -n "$table" ]; then
    # Under --table the repo is only a path PREFIX to attribute fixture rows
    # against: it need not exist on this host.
    repo="$repo_arg"
  else
    repo="$(cd "$repo_arg" && pwd)" || {
      echo "dev-server-kill: --repo is not a directory: $repo_arg" >&2
      exit 2
    }
  fi
else
  case "$root" in
  */.worktrees/*) repo="${root%%/.worktrees/*}" ;;
  *) repo="$root" ;;
  esac
fi

run_dir="$(mktemp -d "${TMPDIR:-/tmp}/dev-server-kill.XXXXXX")" || {
  echo "dev-server-kill: cannot create a temp dir" >&2
  exit 2
}
cleanup() { rm -rf -- "$run_dir" 2>/dev/null || true; }
trap cleanup EXIT

netstat_file=""
if [ -z "$table" ]; then
  MSYS_NO_PATHCONV=1 netstat -ano >"$run_dir/netstat.txt" 2>/dev/null || :
  netstat_file="$run_dir/netstat.txt"
else
  [ -f "$table" ] || {
    echo "dev-server-kill: fixture table not found: $table" >&2
    exit 2
  }
fi

cat >"$run_dir/probe.py" <<'PY'
"""dev-server-kill probe: enumerate node/omega processes, verify identity,
print one KILL|KEEP|UNKNOWN verdict line per candidate.

Live mode reads the process table from kernel32 (toolhelp snapshot) and each
candidate's command line + current directory from its own PEB
(NtQueryInformationProcess -> ProcessBasicInformation -> RTL_USER_PROCESS_PARAMETERS).
Both are plain Win32 calls through ctypes: no wmic, no powershell, no psutil.

Table mode (--table <fixture>) judges rows instead, so the test suite never
looks at a real process.
"""

import os
import re
import sqlite3
import sys

DEAD = {"done", "merged", "archived", "cancelled", "canceled", "closed"}
UNPROVEN = {"", "-", "unknown", "none", "unproven", "missing"}
LIVE_PORTS = {"8098": "lane serve", "8099": "prod"}
# Any non-dead, non-unproven status is treated as LIVE: a status this script
# does not recognise must never make a kill decision (never-kill law).
DEVSERVER_TOKENS = re.compile(r"(?i)(?:^|[/\\])(?:nuxt|nuxi|vite)(?:[.\-][\w.]*)?$")
CARD_ID = re.compile(r"(?i)^t_[0-9a-z]+$")
WORKTREE = re.compile(r"(?i)(?:^|/)\.worktrees/([^/\\\s\"']+)")


def norm(path):
  return (path or "").replace("\\", "/")


def tokens(cmdline):
  out = []
  for raw in (cmdline or "").split():
    tok = raw.strip("\"'").strip()
    if tok:
      out.append(tok)
  return out


def devserver_token(name, cmdline):
  """The argv token that makes this process a dev server, or "" if none.

  A dev server is node whose OWN argv drives nuxt/nuxi/vite: a node process
  that is not one (a language server, a test runner) is never a kill
  candidate, whatever directory it sits in.
  """
  if "node" not in (name or "").lower():
    return ""
  for tok in tokens(cmdline):
    if DEVSERVER_TOKENS.search(norm(tok)):
      return tok
  return ""


def is_serve(name, cmdline):
  if (name or "").lower() != "omega.exe" and (name or "").lower() != "omega":
    return False
  return any(tok.lower() == "serve" for tok in tokens(cmdline))


def worktree_of(cwd, cmdline):
  """(card-segment, matched-path) of the first .worktrees/<seg> sighting."""
  for value in (norm(cwd), norm(cmdline)):
    m = WORKTREE.search(value)
    if m:
      return m.group(1), value
  return "", ""


def in_tree(path_value, root):
  root = norm(root).rstrip("/").lower() + "/"
  for tok in [norm(path_value)] + tokens(path_value):
    if norm(tok).lower().startswith(root):
      return True
  return False


def clip(text, limit=140):
  text = " ".join((text or "").split())
  return text if len(text) <= limit else text[: limit - 3] + "..."


# ---------------------------------------------------------------- board reads

def board_dbs():
  """Candidate board databases, env-first then the stock hermes locations."""
  seen, out = set(), []

  def add(path):
    if not path:
      return
    path = os.path.abspath(path)
    if path in seen or not os.path.isfile(path):
      return
    seen.add(path)
    out.append(path)

  add(os.environ.get("HERMES_KANBAN_DB", ""))
  roots = []
  for key in ("HERMES_KANBAN_HOME",):
    if os.environ.get(key):
      roots.append(os.environ[key])
  if os.environ.get("LOCALAPPDATA"):
    roots.append(os.path.join(os.environ["LOCALAPPDATA"], "hermes", "kanban"))
  if os.environ.get("HOME"):
    roots.append(os.path.join(os.environ["HOME"], ".hermes", "kanban"))
  for base in roots:
    add(os.path.join(base, "kanban.db"))
    boards = os.path.join(base, "boards")
    if os.path.isdir(boards):
      for name in sorted(os.listdir(boards)):
        add(os.path.join(boards, name, "kanban.db"))
  return out


def read_board():
  """(status_by_id, live_webui_ids). A live status in ANY board wins over a
  dead one: the merge-safe direction is to keep."""
  status, webui, unreadable = {}, [], []
  for db in board_dbs():
    try:
      con = sqlite3.connect("file:%s?mode=ro" % db.replace("\\", "/"), uri=True)
      rows = con.execute("select id, status, title, body from tasks").fetchall()
      con.close()
    except sqlite3.OperationalError as exc:
      # A db without a tasks table is not a board (the empty stock kanban.db,
      # an archived or foreign board set): skip it silently. Anything else is
      # worth a word - the run then has less status truth and says so.
      if "no such table" not in str(exc):
        unreadable.append("%s (%s)" % (os.path.basename(os.path.dirname(db)), exc))
      continue
    except Exception as exc:                      # a locked/foreign db is not fatal
      unreadable.append("%s (%s)" % (os.path.basename(os.path.dirname(db)), exc))
      continue
    for card_id, card_status, title, body in rows:
      card_status = (card_status or "").strip().lower()
      prior = status.get(card_id)
      if prior is None or (prior in DEAD and card_status not in DEAD):
        status[card_id] = card_status
      haystack = ("%s %s" % (title or "", body or "")).lower()
      if card_status not in DEAD and card_status not in UNPROVEN:
        if "webui" in haystack or "nuxt" in haystack:
          if card_id not in webui:
            webui.append(card_id)
  if unreadable:
    sys.stderr.write("dev-server-kill: board read partial: %s\n" % "; ".join(unreadable))
  return status, webui


# --------------------------------------------------------------- live process

def live_rows():
  import ctypes
  import ctypes.wintypes as wintypes

  k32 = ctypes.WinDLL("kernel32", use_last_error=True)
  ntdll = ctypes.WinDLL("ntdll", use_last_error=True)
  TH32CS_SNAPPROCESS = 0x00000002
  PROC_QUERY_INFORMATION = 0x0400
  PROC_VM_READ = 0x0010

  class PROCESSENTRY32W(ctypes.Structure):
    _fields_ = [
      ("dwSize", wintypes.DWORD),
      ("cntUsage", wintypes.DWORD),
      ("th32ProcessID", wintypes.DWORD),
      ("th32DefaultHeapID", ctypes.POINTER(ctypes.c_ulong)),
      ("th32ModuleID", wintypes.DWORD),
      ("cntThreads", wintypes.DWORD),
      ("th32ParentProcessID", wintypes.DWORD),
      ("pcPriClassBase", ctypes.c_long),
      ("dwFlags", wintypes.DWORD),
      ("szExeFile", wintypes.WCHAR * 260),
    ]

  class PROCESS_BASIC_INFORMATION(ctypes.Structure):
    _fields_ = [
      ("Reserved1", ctypes.c_void_p),
      ("PebBaseAddress", ctypes.c_void_p),
      ("Reserved2", ctypes.c_void_p * 2),
      ("UniqueProcessId", ctypes.c_void_p),
      ("Reserved3", ctypes.c_void_p),
    ]

  def snapshot():
    snap = k32.CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0)
    ent = PROCESSENTRY32W()
    ent.dwSize = ctypes.sizeof(ent)
    rows = []
    if not k32.Process32FirstW(snap, ctypes.byref(ent)):
      k32.CloseHandle(snap)
      return rows
    while True:
      rows.append((ent.th32ProcessID, ent.th32ParentProcessID, ent.szExeFile))
      if not k32.Process32NextW(snap, ctypes.byref(ent)):
        break
    k32.CloseHandle(snap)
    return rows

  def read_mem(handle, addr, size):
    buf = ctypes.create_string_buffer(size)
    got = ctypes.c_size_t(0)
    if not k32.ReadProcessMemory(handle, ctypes.c_void_p(addr), buf, size, ctypes.byref(got)):
      return None
    return buf.raw[: got.value]

  def read_ustr(handle, addr):
    """A UNICODE_STRING at addr: {USHORT len, USHORT max, DWORD pad, PVOID buf}."""
    raw = read_mem(handle, addr, 16)
    if not raw:
      return None
    length = int.from_bytes(raw[0:2], "little")
    bufptr = int.from_bytes(raw[8:16], "little")
    if length == 0:
      return ""
    if not bufptr:
      return None
    data = read_mem(handle, bufptr, length)
    if data is None:
      return None
    return data.decode("utf-16-le", "replace")

  def identity(pid):
    handle = k32.OpenProcess(PROC_QUERY_INFORMATION | PROC_VM_READ, False, pid)
    if not handle:
      return None, None, "OpenProcess err %d" % ctypes.get_last_error()
    try:
      pbi = PROCESS_BASIC_INFORMATION()
      ret = ctypes.c_ulong(0)
      status = ntdll.NtQueryInformationProcess(
        handle, 0, ctypes.byref(pbi), ctypes.sizeof(pbi), ctypes.byref(ret)
      )
      if status != 0:
        return None, None, "NtQueryInformationProcess %#x" % (status & 0xFFFFFFFF)
      params_raw = read_mem(handle, pbi.PebBaseAddress + 0x20, 8)
      if not params_raw:
        return None, None, "PEB->ProcessParameters unreadable"
      params = int.from_bytes(params_raw, "little")
      # RTL_USER_PROCESS_PARAMETERS (x64): CurrentDirectory.DosPath at 0x38
      # (CURDIR carries a trailing HANDLE, so DllPath 0x50, ImagePathName 0x60,
      # CommandLine 0x70).
      cmdline = read_ustr(handle, params + 0x70)
      cwd = read_ustr(handle, params + 0x38)
      if cmdline is None:
        return None, cwd, "command line unreadable"
      return cmdline, cwd, ""
    finally:
      k32.CloseHandle(handle)

  ports = read_netstat()
  rows = []
  for pid, ppid, name in snapshot():
    low = (name or "").lower()
    if low not in ("node.exe", "omega.exe"):
      continue
    cmdline, cwd, err = identity(pid)
    rows.append(
      {
        "pid": pid,
        "ppid": ppid,
        "name": name,
        "cmdline": cmdline or "",
        "cwd": cwd or "",
        "status": None,          # resolved per candidate, from the board
        "ports": ports.get(pid, []),
        "error": err,
      }
    )
  return rows


def read_netstat():
  path = ARGS.netstat
  if not path:
    return {}
  ports = {}
  try:
    with open(path, "r", encoding="utf-8", errors="replace") as handle:
      for line in handle:
        parts = line.split()
        if len(parts) < 5 or parts[0].upper() not in ("TCP", "UDP"):
          continue
        if "LISTENING" not in line.upper():
          continue
        local, pid = parts[1], parts[-1]
        if not pid.isdigit():
          continue
        port = local.rsplit(":", 1)[-1]
        if port.isdigit():
          ports.setdefault(int(pid), []).append(int(port))
  except OSError:
    return {}
  return ports


# -------------------------------------------------------------- fixture table

def fixture_rows(path):
  rows, webui_live = [], []
  with open(path, "r", encoding="utf-8", errors="replace") as handle:
    for line in handle:
      line = line.rstrip("\r\n")
      if not line.strip():
        continue
      if line.lstrip().startswith("#"):
        m = re.match(r"\s*#\s*webui-live:\s*(.*)$", line, re.I)
        if m:
          webui_live = [x.strip() for x in m.group(1).split(",") if x.strip()]
        continue
      cells = line.split("|")
      if len(cells) < 5:
        sys.stderr.write("dev-server-kill: fixture row needs 5 fields: %s\n" % line)
        continue
      pid = cells[0].strip()
      ports, ppid = [], 0
      if len(cells) > 5 and cells[5].strip():
        ports = [int(p) for p in cells[5].replace(",", " ").split() if p.isdigit()]
      if len(cells) > 6 and cells[6].strip().isdigit():
        ppid = int(cells[6].strip())
      rows.append(
        {
          "pid": int(pid) if pid.isdigit() else 0,
          "ppid": ppid,
          "name": cells[1].strip(),
          "cmdline": cells[2].strip(),
          "cwd": cells[3].strip(),
          "status": cells[4].strip(),
          "ports": ports,
          "error": "",
        }
      )
  return rows, webui_live


# ------------------------------------------------------------------- verdicts

def verdict(row, repo, status_by_id, live_webui):
  """One verdict per candidate. Never-kill classes are checked FIRST."""
  name = row["name"]
  cmdline = row["cmdline"]
  cwd = row["cwd"]
  token = devserver_token(name, cmdline)
  devserver = bool(token)
  serve = is_serve(name, cmdline)
  what = "dev server" if devserver else ("omega serve" if serve else "node process")
  shape = "dev server (%s)" % token if devserver else what

  hit = [(p, LIVE_PORTS[str(p)]) for p in sorted(set(row["ports"])) if str(p) in LIVE_PORTS]
  if hit:
    port, label = hit[0]
    return "KEEP", "listener on :%d (%s) - never-kill class" % (port, label)

  if row["error"]:
    return "UNKNOWN", "identity unreadable (%s) - keep" % row["error"]

  seg, _ = worktree_of(cwd, cmdline)
  if seg:
    status = row["status"] if row["status"] is not None else status_by_id.get(seg, "")
    status = (status or "").strip().lower()
    if status in UNPROVEN:
      return "UNKNOWN", "%s in .worktrees/%s but the owning card is unproven (keep)" % (
        shape,
        seg,
      )
    if status not in DEAD:
      return "KEEP", "%s in .worktrees/%s (card status: %s, live) - keep" % (
        shape,
        seg,
        status,
      )
    if devserver:
      return "KILL", "%s in .worktrees/%s (card status: %s): %s" % (
        shape,
        seg,
        status,
        clip(cmdline, 110),
      )
    if serve:
      return "KILL", "omega serve in .worktrees/%s (card status: %s): %s" % (
        seg,
        status,
        clip(cmdline, 110),
      )
    return "UNKNOWN", "%s (not a dev server) in .worktrees/%s (card status: %s) - keep" % (
      what,
      seg,
      status,
    )

  main = in_tree(cwd, repo) or in_tree(cmdline, repo)
  if devserver:
    if not main:
      return "UNKNOWN", "dev server outside %s (cwd %s) - keep" % (repo, clip(cwd, 60))
    if live_webui:
      return "KEEP", "main-checkout dev server while live card(s) %s own webui work - keep" % (
        ",".join(live_webui[:3])
      )
    return "KILL", "main-checkout dev server with no live webui card: %s" % clip(cmdline)
  if serve:
    ports = ",".join(":%d" % p for p in sorted(set(row["ports"]))) or "no listener"
    return "KEEP", "omega serve (%s) - owner unprovable, never-kill class" % ports
  return "UNKNOWN", "%s, no card attribution: %s" % (what, clip(cmdline))


class Args(object):
  pass


ARGS = Args()

RANK = {"KILL": 0, "KEEP": 1, "UNKNOWN": 2}


def main(argv):
  import argparse

  parser = argparse.ArgumentParser(add_help=False)
  parser.add_argument("--repo", required=True)
  parser.add_argument("--table", default="")
  parser.add_argument("--netstat", default="")
  parser.add_argument("--self", default="")
  ns = parser.parse_args(argv)
  ARGS.repo = ns.repo
  ARGS.table = ns.table
  ARGS.netstat = ns.netstat

  status_by_id, live_webui = {}, []
  if ns.table:
    rows, live_webui = fixture_rows(ns.table)
    for row in rows:
      if row["status"] and row["status"].strip().lower() not in UNPROVEN:
        seg, _ = worktree_of(row["cwd"], row["cmdline"])
        if seg:
          status_by_id.setdefault(seg, row["status"])
  else:
    if not sys.platform.startswith("win"):
      sys.stderr.write(
        "dev-server-kill: live enumeration is a Windows probe (host is %s); "
        "use --table on this platform\n" % sys.platform
      )
      return 0
    status_by_id, live_webui = read_board()
    rows = live_rows()

  skip = set()
  for value in (ns.self, os.getppid(), os.getpid()):
    try:
      skip.add(int(value))
    except (TypeError, ValueError):
      pass

  rows = [row for row in rows if row["pid"] not in skip]
  by_pid = {row["pid"]: row for row in rows}
  decided = {}

  def decide(row, depth=0, seen=None):
    """Verdict for one row, then the TREE rule: a candidate that cannot be
    verified on its own but whose ancestor is a KILL rides the kill - that is
    the leaked `nuxt dev` tree shape (esbuild/vite children orphaned by a
    parent kill). A KEEP is never downgraded: the port law and the live-card
    law are absolute."""
    pid = row["pid"]
    if pid in decided:
      return decided[pid]
    decided[pid] = None  # cycle guard
    kind, evidence = verdict(row, ns.repo, status_by_id, live_webui)
    seen = seen or set()
    seen.add(pid)
    parent = row.get("ppid") or 0
    if kind == "UNKNOWN" and depth < 32 and parent in by_pid and parent not in seen:
      pkind, _ = decide(by_pid[parent], depth + 1, seen)
      if pkind == "KILL":
        parent_row = by_pid[parent]
        pseg, _ = worktree_of(parent_row["cwd"], parent_row["cmdline"])
        pkindname = "dev server" if devserver_token(
          parent_row["name"], parent_row["cmdline"]
        ) else "process"
        kind = "KILL"
        evidence = "tree child of KILL %d (%s in .worktrees/%s)" % (
          parent,
          pkindname,
          pseg or "?",
        )
    decided[pid] = (kind, evidence)
    return decided[pid]

  out = []
  for row in rows:
    kind, evidence = decide(row)
    out.append((RANK[kind], row["pid"], "%s %d %s" % (kind, row["pid"], evidence)))
  for _, _, line in sorted(out):
    print(line)
  return 0


if __name__ == "__main__":
  sys.exit(main(sys.argv[1:]))
PY

probe_args=(
  --repo "$(winpath "$repo")"
  --self "$$"
)
if [ -n "$table" ]; then
  probe_args+=(--table "$(winpath "$table")")
else
  probe_args+=(--netstat "$(winpath "$netstat_file")")
fi

verdicts="$run_dir/verdicts.txt"
"$py" "$(winpath "$run_dir/probe.py")" "${probe_args[@]}" >"$verdicts" 2>"$run_dir/probe.err"
probe_status=$?
if [ -s "$run_dir/probe.err" ]; then
  cat "$run_dir/probe.err" >&2
fi
if [ "$probe_status" -ne 0 ]; then
  echo "dev-server-kill: probe failed (exit $probe_status)" >&2
  exit 2
fi

kill_n=0
keep_n=0
unknown_n=0
failed=0
while IFS= read -r line; do
  [ -n "$line" ] || continue
  printf '%s\n' "$line"
  case "$line" in
  KILL\ *) kill_n=$((kill_n + 1)) ;;
  KEEP\ *) keep_n=$((keep_n + 1)) ;;
  UNKNOWN\ *) unknown_n=$((unknown_n + 1)) ;;
  esac
done <"$verdicts"

if [ "$apply" -eq 1 ] && [ -z "$table" ] && [ "$kill_n" -gt 0 ]; then
  kill_lines="$(sed -n 's/^KILL \([0-9][0-9]*\) .*/\1/p' "$verdicts")"
  for pid in $kill_lines; do
    if out="$(MSYS_NO_PATHCONV=1 taskkill /F /PID "$pid" 2>&1)"; then
      printf 'APPLY %s ok: %s\n' "$pid" "$(printf '%s' "$out" | tr -d '\r' | head -1)"
    else
      failed=$((failed + 1))
      printf 'APPLY %s FAILED: %s\n' "$pid" "$(printf '%s' "$out" | tr -d '\r' | head -1)"
    fi
  done
elif [ "$apply" -eq 1 ] && [ -n "$table" ]; then
  echo "dev-server-kill: --apply is a no-op under --table (fixture pids are fake)" >&2
fi

echo "KILL-STEP: $kill_n kill / $keep_n keep / $unknown_n unknown"

if [ "$apply" -eq 1 ] && [ "$failed" -gt 0 ]; then
  exit 1
fi
exit 0
