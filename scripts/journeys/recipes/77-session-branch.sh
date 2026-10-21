#!/bin/bash
# 77-session-branch - the session management parity journey (t_6bf696e4):
# the sidebar's new row actions work against the REAL backend - branch
# forks the session (child carries the lineage badge, opens, and its
# transcript inherits the parent's history), rename edits the label
# inline and survives a reload, export is the /export-parity JSONL.
JOURNEY_NEEDS_MODEL=1
. "${JROOT:?run must export JROOT}/lib.sh"

# fresh_open: this recipe runs against a long-lived chrome (one the
# runner did NOT launch with a profile reset), so ab open to the SAME
# url is a no-op and the SPA keeps stale editing state (renamingId from
# a prior run hides every row's meta). about:blank first forces a real
# navigation, so open_app re-mounts the app clean.
fresh_open() { ab open "about:blank" >/dev/null 2>&1; open_app; }

# open_row <label-fallback>: OPEN the journey session with a title
# selector click. open_journey_row's find-text click HOVERS first - and
# the hover reveals the row-action cluster under the cursor, so the
# click can land on the rename pencil: the editor opens (meta hides, no
# transcript). The title element click bubbles to the row's open emit.
open_row() {
  local fallback="$1" i=0 out
  while [ "$i" -lt 20 ]; do
    out=$(jsq "(()=>{const rows = Array.from(document.querySelectorAll(\".session-list li\")); const r = rows.find(x => (x.innerText || \"\").includes(\"$fallback\")); if (!r) return 0; const t = r.querySelector(\".session-title\"); if (!t) return 0; t.click(); return 1;})()" 2>&1 | tail -1)
    [ "$out" = "1" ] && return 0
    sleep 1
    i=$((i + 1))
  done
  die "journey session row never appeared in sidebar: $fallback"
}

JRN_ID=$(new_journey_session branch-root | tail -1)
TAG=$(journey_tag)
ROOTLABEL="(journey) branch-root $TAG"
label_journey_session "$JRN_ID" "$ROOTLABEL"

# seed one exchange so the child inherits something visible
send_journey_message "$JRN_ID" "branch seed - reply with exactly: seedok"
step "seeded exchange"

# wait for the run to persist (the store is the source of truth; the
# SPA's background /sessions poll is too coarse to drive a tight assert)
seeded=0
for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do
  seeded=$(curl -s "$JOURNEY_BASE_URL/sessions" | python3 -c "
import sys, json
rows = json.load(sys.stdin)
print(1 if any(r['id'] == '$JRN_ID' and r.get('message_count', 0) > 0 for r in rows) else 0)
")
  [ "$seeded" = "1" ] && break
  sleep 2
done
assert_eq "$seeded" "1" "seeded exchange persisted (store message_count > 0)"

fresh_open
open_row "(journey) branch-root "

# reload-pulled row meta: a fresh open re-fetches /sessions, so the row's
# counts render without racing the background poll
has_meta=0
for i in 1 2 3 4 5 6 7 8; do
  has_meta=$(jsq '(()=>{const rows = Array.from(document.querySelectorAll(".session-list li")); const r = rows.find(x => (x.innerText || "").includes("branch-root")); if (!r) return 0; return (r.innerText || "").match(/msg/) ? 1 : 0;})()' 2>&1)
  [ "$has_meta" = "1" ] && break
  sleep 1
done
assert_eq "$has_meta" "1" "row meta (counts) renders"

# --- branch: click the fork action on the journey row ---------------
# polled: the row re-renders with the list refresh; the click is the
# hover cluster's first button (aria-label Branch session), and .stop
# keeps it from opening the row.
branched=0
for i in 1 2 3 4 5 6 7 8 9 10; do
  branched=$(jsq '(()=>{const rows = Array.from(document.querySelectorAll(".session-list li")); const r = rows.find(x => (x.innerText || "").includes("branch-root")); if (!r) return 0; const b = r.querySelector("button[aria-label=\"Branch session\"]"); if (!b) return 0; b.click(); return 1;})()' 2>&1)
  [ "$branched" = "1" ] && break
  sleep 1
done
assert_eq "$branched" "1" "branch action clicked"

# the app POSTs /branch, refreshes the list and opens the child: poll
# for a badge-bearing row (the CHILD - only parent_id rows carry it).
child_found=0
for i in 1 2 3 4 5 6 7 8 9 10 11 12; do
  child_found=$(jsq '(()=>{const rows = Array.from(document.querySelectorAll(".session-list li")); const r = rows.find(x => x.querySelector(".session-branch")); if (!r) return 0; return r.classList.contains("active") ? 1 : 2;})()' 2>&1)
  [ "$child_found" = "1" ] && break
  sleep 1
done
assert_eq "$child_found" "1" "child row renders with the branch badge and is the active tab"

# the PARENT row still renders badge-less (part 3: the mark is the
# child's lineage, not the parent's)
parent_badge=0
for i in 1 2 3 ; do
  parent_badge=$(jsq '(()=>{const rows = Array.from(document.querySelectorAll(".session-list li")); const r = rows.find(x => (x.innerText || "").includes("branch-root") && (x.innerText || "").match(/msg/)); if (!r) return 0; return r.querySelector(".session-branch") ? 1 : 0;})()' 2>&1)
  [ "$parent_badge" = "1" ] && break
  sleep 1
done
assert_eq "$parent_badge" "0" "parent row renders without the badge"

# the child's transcript inherits the parent's history as a prefix -
# the verbatim USER turn is always persisted, so it survives regardless
# of how the model phrased its answer
inh=0
for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14; do
  inh=$(jsq '(()=>{const r = document.querySelector(".session-branch"); if (!r) return 0; const t = document.body.innerText || ""; return t.includes("branch seed") ? 1 : 0;})()' 2>&1)
  [ "$inh" = "1" ] && break
  sleep 2
done
assert_eq "$inh" "1" "child transcript inherits the parent's history (seed user turn visible)"

# --- rename: inline edit on the child row ---------------------------
NEWLABEL="(journey) branch-child $TAG"
renamed=0
for i in 1 2 3 4 5 6 7 8 9 10; do
  renamed=$(jsq '(()=>{const rows = Array.from(document.querySelectorAll(".session-list li")); const r = rows.find(x => x.querySelector(".session-branch")); if (!r) return 0; const p = r.querySelector("button[aria-label=\"Rename session\"]"); if (!p) return 0; p.click(); const inp = r.querySelector(".session-rename"); if (!inp) return 0; return 1;})()' 2>&1)
  [ "$renamed" = "1" ] && break
  sleep 1
done
assert_eq "$renamed" "1" "rename action opened the inline editor"

# type + Enter (the input has focus on render); committed -> PUT -> row
# shows the new label
typed=0
for i in 1 2 3 4 5; do
  typed=$(jsq "(()=>{const inp = document.querySelector(\".session-rename\"); if (!inp) return 0; const set = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, \"value\").set; set.call(inp, \"$NEWLABEL\"); inp.dispatchEvent(new Event(\"input\", {bubbles: true})); inp.dispatchEvent(new KeyboardEvent(\"keyup\", {key: \"Enter\", bubbles: true})); return 1;})()" 2>&1)
  [ "$typed" = "1" ] && break
  sleep 1
done
assert_eq "$typed" "1" "rename draft typed + Enter committed"

label_set=0
for i in 1 2 3 4 5 6 7 8; do
  label_set=$(jsq "(()=>{const rows = Array.from(document.querySelectorAll(\".session-list li\")); const r = rows.find(x => (x.innerText || \"\").includes(\"branch-child\")); return r ? 1 : 0;})()" 2>&1)
  [ "$label_set" = "1" ] && break
  sleep 1
done
assert_eq "$label_set" "1" "renamed label renders on the row"

# --- export: the /export-parity JSONL --------------------------------
# the anchor exists on the row and points at the export endpoint
export_anchor=0
export_anchor=$(jsq '(()=>{const rows = Array.from(document.querySelectorAll(".session-list li")); const r = rows.find(x => (x.innerText || "").includes("branch-child")); if (!r) return 0; const a = r.querySelector("a.row-export"); if (!a) return 0; return (a.getAttribute("href") || "").includes("/export") ? 1 : 0;})()' 2>&1)
assert_eq "$export_anchor" "1" "export anchor present on the row (download href)"

# the wire truth: the same shape the TUI /export writes - one
# {"role","content"} line per message, parent history first
CHILD=$(curl -s "$JOURNEY_BASE_URL/sessions?archived=include" | python3 -c "
import sys, json
rows = json.load(sys.stdin)
for r in rows:
    if r.get('label', '') and 'branch-child' in r.get('label', ''):
        print(r['id'])
        break
")
[ -n "$CHILD" ] || die "export: child session not found on the lane"
sleep 1
body=$(curl -s "$JOURNEY_BASE_URL/sessions/$CHILD/export")
role=$(printf '%s' "$body" | head -1 | python3 -c "import sys, json; line=json.loads(sys.stdin.readline()); print(line['role'])")
content=$(printf '%s' "$body" | head -1 | python3 -c "import sys, json; line=json.loads(sys.stdin.readline()); print(line['content'])")
assert_eq "$role" "user" "export first line is the seeded user turn (TUI /export JSONL shape)"
assert_ne "$content" "" "export content present"
cd=$(curl -s -I "$JOURNEY_BASE_URL/sessions/$CHILD/export" | grep -i content-disposition | head -1)
printf '%s' "$cd" | grep -q attachment || die "export: not served as an attachment download ($cd)"
step "ok: export served as attachment download"

# --- reload: the label + the badges survive --------------------------
fresh_open
persist=0
for i in 1 2 3 4 5 6 7 8; do
  persist=$(jsq '(()=>{const rows = Array.from(document.querySelectorAll(".session-list li")); const r = rows.find(x => (x.innerText || "").includes("branch-child")); if (!r) return 0; return r.querySelector(".session-branch") ? 1 : 0;})()' 2>&1)
  [ "$persist" = "1" ] && break
  sleep 1
done
assert_eq "$persist" "1" "label + branch badge survive a reload"

assert_no_console_errors
step "journey green"