#!/bin/bash
# 114-tools-one-row - the Tools tab renders ONE row per tool domain
# (card t_0c8d88f2, the journey-upkeep follow-up to t_7871a56a).
#
# Endo's in-app comment on the served tools tab (2026-11-07): "why are
# there 2 `files` sections?". The duplicate-row bug class was fixed in
# t_7871a56a and pinned at UNIT level (webui/test/registryTools.test.ts
# holds the row model: two rows on one domain stays impossible). This
# recipe is the missing BROWSER-level pin - it asserts what the served
# bundle actually renders, not what the pure row model computes.
#
# Pins:
#   a  one row per domain - the files domain renders exactly ONE row
#                           named "files", and no domain (a row name's
#                           dotted prefix) renders twice. The red shape
#                           is the t_7871a56a bug: a "files" row PLUS a
#                           "files.glob"/"files.grep" row beside it.
#   b  absorbed detail    - the files row's expanded detail carries the
#                           domain's real wire identities (files.read,
#                           files.write, files.edit, files.batch,
#                           files.glob, files.grep). The card body said
#                           "read/write/edit actions": the flat
#                           action-protocol `files` tool retired in
#                           t_d8fbad57 and the dotted names are the wire
#                           now - same law, the identities moved from
#                           actions[] to sibling tools.
#   c  detail coherence   - EVERY row's detail lines belong to that row's
#                           own domain; a files.* line under any other
#                           row is the same duplicate-section bug one
#                           step later.
#   d  fold-aware render  - the scope extension of 2026-11-07 (the
#                           one-way door t_5c9bb50a fixed): disabling the
#                           files domain from the tab KEEPS its row -
#                           dimmed, toggleable (aria "enable files"),
#                           detail intact. A row that VANISHES on a
#                           disable is the red shape.
#   e  two reads disagree - while the fold names all six, the bare
#                           GET /tools (the model's executable registry)
#                           drops every files.* name, and the fold-aware
#                           GET /tools?disabled=1 keeps all six stamped
#                           disabled:true. The management surface and the
#                           model's registry are different reads on
#                           purpose; both are pinned here. Graded as a
#                           DIFF against the pre-toggle snapshot, never as
#                           an absolute name list: the lane fold is USER
#                           state (a live repro rides it), so only the six
#                           files.* names may move in either read.
#   f  re-enable           - the fold clears (wire AND the lane home's
#                           config.yaml) and the row renders on again.
#
# Lane-home scoped by law: the toggles write the serving lane's
# config.yaml, so OMEGA_HOME must name the lane's home (the bin/ dir the
# serve process owns), never a checkout root. Home resolution mirrors
# cmd/omega/home.go: OMEGA_HOME wins, else OMEGA_BIN's directory (the
# nightly runner's master-copy workflow exports OMEGA_BIN only). Both
# unset dies loudly. Residue-free: the cleanup trap re-enables only the
# names STILL folded, so a green run leaves the fold untouched and a red
# one puts it back; a leftover fold from a crashed earlier run is cleared
# at pre-flight instead of reding this run's toggle. The pre-flight owns
# the recipe's OWN files.* residue and nothing else: every other name in
# the lane fold is user state, read for the diff assertions and never
# cleared.
#
# Drive-by-aria-label (worker-onboarding law): the toggle is clicked
# through its aria state machine ("disable files" -> "enable files") -
# ONE click switches (card t_5051d523: the arm/confirm second click is
# dead); snapshot refs invalidate between clicks.
. "${JROOT:?run must export JROOT}/lib.sh"

if [ -z "${OMEGA_HOME:-}" ]; then
  [ -n "${OMEGA_BIN:-}" ] || die "set OMEGA_HOME or OMEGA_BIN to the serving lane home"
  OMEGA_HOME="$(dirname "$OMEGA_BIN")"
fi
CONFIG="$OMEGA_HOME/config/config.yaml"
[ -d "$OMEGA_HOME" ] || die "OMEGA_HOME does not exist: $OMEGA_HOME (set it to the serving lane's home, never a checkout root)"
FILES_NAMES="files.read files.write files.edit files.batch files.grep files.glob"
FILES_SORTED="$(printf '%s\n' $FILES_NAMES | sort | tr '\n' ' ' | sed 's/ $//')"

# --- wire + fold helpers ---------------------------------------------------

# fold_names: every name GET /disabled carries under tools, sorted and
# space separated (empty = nothing folded). GET /disabled is the fold's
# rendered truth, and on a live lane part of that fold is USER state this
# recipe neither assumes clean nor clears. A read that FAILED is not a
# clean fold: an empty body from a dropped call would skip the
# pre-flight's clear, leave the row rendered in its off state, and the
# toggle click would then find no "disable files" button at all (the
# no-op-toggle red, one step earlier than the toggle).
fold_names() {
  local body code
  body=$(curl -s -w '\n%{http_code}' "$JOURNEY_BASE_URL/disabled")
  code="$(printf '%s' "$body" | tail -1)"
  case "$code" in
    200) ;;
    *) die "GET /disabled did not answer (code=${code:-<none>}) - the fold state is unknown, not clean" ;;
  esac
  printf '%s' "$body" | sed '$d' | python3 -c '
import json, sys
try:
    fold = (json.load(sys.stdin) or {}).get("tools", []) or []
except Exception:
    sys.exit(0)
print(" ".join(sorted(str(n) for n in fold)))' 2>/dev/null | tr -d '\r'
}

# files_fold: the files.* names the fold carries (empty = the recipe's own
# residue is gone; who else is folded is not this function's business).
files_fold() {
  python3 -c 'import sys; print(" ".join(n for n in sys.argv[1].split() if n.startswith("files.")))' "$(fold_names)"
}

# wire_names: every name the bare GET /tools lists (the model's executable
# registry), sorted and space separated. A failed read DIES: the diff
# assertions grade one snapshot against another, so a silent empty would
# grade every difference against nothing.
wire_names() {
  local body code
  body=$(curl -s -w '\n%{http_code}' "$JOURNEY_BASE_URL/tools")
  code="$(printf '%s' "$body" | tail -1)"
  case "$code" in
    200) ;;
    *) die "GET /tools did not answer (code=${code:-<none>}) - the registry snapshot is unknown, not empty" ;;
  esac
  printf '%s' "$body" | sed '$d' | python3 -c '
import json, sys
try:
    rows = json.load(sys.stdin) or []
except Exception:
    sys.exit(0)
print(" ".join(sorted(str(t.get("name", "")) for t in rows if t.get("name"))))' 2>/dev/null | tr -d '\r'
}

# set_minus <left> <right>: the names in left and not right, sorted and
# space separated. A wire name carries no space, so the joined form IS the
# set, and comparing two of these grades a fold change as a change.
set_minus() {
  python3 -c '
import sys
l, r = set(sys.argv[1].split()), set(sys.argv[2].split())
print(" ".join(sorted(l - r)))' "$1" "$2"
}

# clear_files_fold: re-enable whatever files.* names are still folded
# (the cleanup trap and the pre-flight both ride it).
clear_files_fold() {
  local n
  for n in $(files_fold); do
    curl -s -X PUT "$JOURNEY_BASE_URL/disabled/tools" \
      -H 'Content-Type: application/json' \
      -d "{\"name\": \"$n\", \"enabled\": true}" >/dev/null 2>&1 || true
  done
}
trap clear_files_fold EXIT

# wire_files_count <query>: how many files.* names GET /tools<query>
# answers (query "" = the model's registry, "?disabled=1" = the
# fold-aware management read).
wire_files_count() {
  curl -s "$JOURNEY_BASE_URL/tools$1" | python3 -c '
import json, sys
rows = json.load(sys.stdin) or []
print(len([t for t in rows if str(t.get("name", "")).startswith("files.")]))' 2>/dev/null | tr -d '\r'
}

# wire_stamped: "<files.* count> <count stamped disabled:true>" on the
# fold-aware read - "6 6" while the whole domain is off, "6 0" when on.
wire_stamped() {
  curl -s "$JOURNEY_BASE_URL/tools?disabled=1" | python3 -c '
import json, sys
rows = json.load(sys.stdin) or []
sel = [t for t in rows if str(t.get("name", "")).startswith("files.")]
print(len(sel), sum(1 for t in sel if t.get("disabled")))' 2>/dev/null | tr -d '\r'
}

# fold_has <name>: non-zero unless config.yaml's `disabled:` fold names
# it. The scan stays inside the fold block (Go-yaml keeps the document's
# style, and a bare grep of the whole file would match any unrelated use
# of the name), and the flow shape a bare-{} document re-serializes to -
# {disabled: {tools: [name]}} - is read too (92-registry-toggles law).
fold_has() {
  [ -f "$CONFIG" ] || return 1
  if awk -v want="$1" '
    fold && found { next }
    /^disabled:/ { fold = 1; next }
    fold && /^[A-Za-z_]/ { fold = 0 }
    fold && $0 ~ "^[[:space:]]*- " want "[[:space:]]*$" { found = 1 }
    END { exit (found ? 0 : 1) }
  ' "$CONFIG"; then
    return 0
  fi
  python3 - "$CONFIG" "$1" <<'PY'
import re, sys
text = open(sys.argv[1]).read()
want = re.escape(sys.argv[2])
for line in text.splitlines():
    i = line.find("disabled:")
    if i < 0:
        continue
    rest = line[i + len("disabled:"):]
    if re.search(r'[\{,\[]\s*"?' + want + r'"?\s*[,}\]]', rest):
        sys.exit(0)
    sys.exit(1)
sys.exit(1)
PY
}

# --- browser probes (numbers serialize unquoted; strings arrive quoted) ----

# row_names: the served row names, comma separated, in render order.
row_names() {
  jsq '(()=>{const n=[...document.querySelectorAll(".tools-list .registry-row")].map(r=>((r.querySelector(".reg-row-name")||{}).textContent||"").trim()).filter(Boolean);return n.join(",")})()' 2>&1 | tail -1 | tr -d '"'
}

# files_rows: how many served rows are named exactly "files".
files_rows() {
  jsq '(()=>{const n=[...document.querySelectorAll(".tools-list .registry-row")].map(r=>((r.querySelector(".reg-row-name")||{}).textContent||"").trim());return n.filter(x=>x==="files").length})()' 2>&1 | tail -1
}

# dup_domains: the domains (a row name's dotted prefix) rendered by more
# than one row, comma separated - the t_7871a56a red shape, generic.
dup_domains() {
  jsq '(()=>{const n=[...document.querySelectorAll(".tools-list .registry-row")].map(r=>((r.querySelector(".reg-row-name")||{}).textContent||"").trim()).filter(Boolean);const c={};for(const x of n){const d=x.split(".")[0];c[d]=(c[d]||0)+1}return Object.keys(c).filter(d=>c[d]>1).join(",")})()' 2>&1 | tail -1 | tr -d '"'
}

# missing_files_detail: the files-domain identities the files row's
# expanded detail does NOT carry, comma separated (empty = complete).
missing_files_detail() {
  jsq '(()=>{const want=["files.read","files.write","files.edit","files.batch","files.glob","files.grep"];const r=[...document.querySelectorAll(".tools-list .registry-row")].find(x=>((x.querySelector(".reg-row-name")||{}).textContent||"").trim()==="files");if(!r)return want.join(",");const got=[...r.querySelectorAll(".registry-detail-line")].map(l=>((l.textContent||"").split(" - ")[0]||"").trim());return want.filter(w=>got.indexOf(w)<0).join(",")})()' 2>&1 | tail -1 | tr -d '"'
}

# detail_domain_breaks: detail lines rendered under a row that is not
# their own domain ("<row>:<identity>", comma separated). Catches the
# same duplicate-section bug a step later: the files identities absorbed
# into, or leaked under, the wrong section.
detail_domain_breaks() {
  jsq '(()=>{const bad=[];for(const r of document.querySelectorAll(".tools-list .registry-row")){const name=((r.querySelector(".reg-row-name")||{}).textContent||"").trim();const dom=name.split(".")[0];for(const l of r.querySelectorAll(".registry-detail-line")){const id=((l.textContent||"").split(" - ")[0]||"").trim();if(!id)continue;if(id.split(".")[0]!==dom)bad.push(name+":"+id)}}return bad.join(",")})()' 2>&1 | tail -1 | tr -d '"'
}

# expand_all: click every collapsed row expander (one eval, Vue re-renders
# the detail lists after it).
expand_all() {
  jsq '(()=>{let n=0;for(const b of document.querySelectorAll(".tools-list .registry-expander")){if((b.getAttribute("aria-label")||"").indexOf("expand ")===0){b.click();n++}}return n})()' 2>&1 | tail -1
}

# row_is_off / row_is_on: booleans for the toggle round-trip waits.
# Card t_d92a1f74: the ON row's accessible name carries the consequence of
# the click it offers ("disable files - absent from the agent tool list"),
# so the ON match is fold-aware; the OFF row's "enable files" stays exact.
ROW_IS_OFF='(()=>{const r=[...document.querySelectorAll(".tools-list .registry-row")].find(x=>((x.querySelector(".reg-row-name")||{}).textContent||"").trim()==="files");const b=r&&r.querySelector("button.registry-toggle");return !!r&&r.classList.contains("registry-off")&&!!b&&b.getAttribute("aria-label")==="enable files"})()'
ROW_IS_ON='(()=>{const r=[...document.querySelectorAll(".tools-list .registry-row")].find(x=>((x.querySelector(".reg-row-name")||{}).textContent||"").trim()==="files");const b=r&&r.querySelector("button.registry-toggle");const l=(b&&b.getAttribute("aria-label"))||"";return !!r&&!r.classList.contains("registry-off")&&!!b&&(l==="disable files"||l.startsWith("disable files - "))})()'

# toggle_click <exact-aria> <prefix-fallback>: click the pane's registry
# toggle matching the aria state machine once. Prints 1 when clicked,
# 0 when not found.
toggle_click() {
  jsq "(()=>{const b=[...document.querySelectorAll('.tools-list button.registry-toggle')].find(x=>{const l=x.getAttribute('aria-label')||'';return l==='$1'||l.startsWith('$2')});if(!b){return 0}b.click();return 1})()" 2>&1 | tail -1
}

# --- pre-flight: the fold this recipe toggles must start clean ------------
leftover=$(files_fold)
if [ -n "$leftover" ]; then
  step "pre-flight: clearing a leftover files fold ($leftover)"
  clear_files_fold
  [ -z "$(files_fold)" ] || die "pre-flight could not clear the files fold: $(files_fold)"
fi

# --- boot: the SPA as the customer opens it -------------------------------
open_app
js 'localStorage.clear()' >/dev/null 2>&1
ab reload >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true
sleep 1

# The pane is identified by its CHIP SET plus a rendered width, never by
# a pane class: edge panes carry no edge-left/edge-right class, and
# kind-* follows the ACTIVE tab (so it drifts off the right pane the
# moment Tools is selected).
TOOLS_PANE='(()=>{const p=[...document.querySelectorAll(".dock-pane")].find(x=>x.getBoundingClientRect().width>0&&[...x.querySelectorAll(".dock-tab")].some(t=>((t.getAttribute("aria-label")||t.textContent||"").trim())==="Tools"));return p?1:0})()'
if [ "$(jsq "$TOOLS_PANE" 2>&1 | tail -1)" != "1" ]; then
  js 'document.querySelector("[aria-label=\"Toggle right sidebar\"]")?.click()'
  wait_js '(()=>{const p=[...document.querySelectorAll(".dock-pane")].find(x=>x.getBoundingClientRect().width>0&&[...x.querySelectorAll(".dock-tab")].some(t=>((t.getAttribute("aria-label")||t.textContent||"").trim())==="Tools"));return !!p})()' 10 \
    || die "no rendered pane hosts the Tools chip (right sidebar closed and the toggle did not open it)"
fi
js '(()=>{const p=[...document.querySelectorAll(".dock-pane")].find(x=>x.getBoundingClientRect().width>0&&[...x.querySelectorAll(".dock-tab")].some(t=>((t.getAttribute("aria-label")||t.textContent||"").trim())==="Tools"));const t=[...p.querySelectorAll(".dock-tab")].find(e=>((e.getAttribute("aria-label")||e.textContent||"").trim())==="Tools");t.click();return 1})()' >/dev/null 2>&1
wait_js '(()=>{return !!document.querySelector(".tools-list .registry-row")})()' 15 \
  || die "the Tools tab never rendered a row (wrong lane, or the tools panel failed to mount)"
step "served rows: $(row_names)"

# --- (a) one row per domain ----------------------------------------------
assert_eq "$(files_rows)" "1" "the files domain renders exactly ONE row"
assert_eq "$(dup_domains)" "" "no domain renders two rows (the t_7871a56a duplicate-section red shape)"
step "ok: (a) one row per domain, no duplicate rows"

# --- (b)+(c) the absorbed detail and its coherence ------------------------
expand_all >/dev/null
wait_js '(()=>{const ex=[...document.querySelectorAll(".tools-list .registry-expander")];return ex.length>0&&ex.every(b=>(b.getAttribute("aria-label")||"").indexOf("collapse ")===0)})()' 10 \
  || die "the row detail never expanded (expand_all clicked no expander, or the detail list is gone)"
assert_eq "$(missing_files_detail)" "" "the files row carries read/write/edit (+batch) and files.glob/files.grep as absorbed detail"
assert_eq "$(detail_domain_breaks)" "" "every detail line belongs to its own row's domain"
step "ok: (b)+(c) the files identities sit under the files row and nowhere else"

# --- (d) the fold-aware render: disabling the domain keeps its row -------
# The snapshots the diff assertions grade against: taken AFTER the
# pre-flight (the recipe's own files.* residue is gone) and BEFORE the
# click (the lane exactly as its owner left it). This recipe reads that
# fold and never assumes it empty.
WIRE_SNAP="$(wire_names)"
FOLD_SNAP="$(fold_names)"
step "pre-toggle snapshot: $(printf '%s' "$FOLD_SNAP" | wc -w | tr -d ' ') tool(s) folded on the lane, outside this recipe's domain and left alone"

# ONE click commits (card t_5051d523): the arm/confirm two-step that
# needed a second click is dead, so the click writes and the row
# re-renders off.
# A click that ANSWERS is not a write that LANDED (mechanism: the lane drops a
# write under load). The row's off state arrives after the page's POST has been
# served and the fold has re-rendered, so grade the WIRE first and re-drive the
# same press once when it never took: while the wire is still clean the button
# still says "disable files", so the second press cannot fold the domain twice.
# The wire grade names a FILES name: any-tool presence is already true on a
# lane whose owner folded something else.
clicked=$(toggle_click "disable files" "disable files - ")
assert_eq "$clicked" "1" "the files row's toggle answered the click"
if ! poll_http "$JOURNEY_BASE_URL/disabled" '"files\.read"' 15; then
  step "the fold never took the click (GET /disabled still carries no files.* name) - pressing once more"
  toggle_click "disable files" "disable files - " >/dev/null
fi
wait_js "$ROW_IS_OFF" 10 \
  || die "one click did not switch the files row - the one-way door: the row must STAY, dimmed and toggleable"
assert_eq "$(files_rows)" "1" "the disabled domain keeps its ONE row (no vanish, no split)"
assert_eq "$(dup_domains)" "" "still one row per domain while the domain is off"
assert_eq "$(missing_files_detail)" "" "the dimmed row keeps its full detail (the management surface still says what is off)"
step "ok: (d) the disabled domain keeps its dimmed, toggleable row"

# --- (e) the two reads disagree on purpose --------------------------------
# The files-domain half is absolute: the recipe owns those six names.
assert_eq "$(wire_files_count "")" "0" "bare GET /tools drops every disabled files.* name (the model's registry)"
assert_eq "$(wire_stamped)" "6 6" "fold-aware GET /tools?disabled=1 keeps all six, each stamped disabled"
# The fan-out half is a DIFF against the pre-click snapshot: on a lane whose
# owner folded other tools, only the six files.* names may move, in either
# read. An absolute name here reds on correct product behavior the moment the
# lane carries a live repro fold (t_3845b80e), which wedged every landing.
WIRE_NOW="$(wire_names)"
assert_eq "$(set_minus "$WIRE_SNAP" "$WIRE_NOW")" "$FILES_SORTED" \
  "the bare registry lost exactly the six files.* names (the fan-out)"
assert_eq "$(set_minus "$WIRE_NOW" "$WIRE_SNAP")" "" \
  "the fan-out disabled no tool outside the files domain (bare GET /tools)"
FOLD_NOW="$(fold_names)"
assert_eq "$(set_minus "$FOLD_NOW" "$FOLD_SNAP")" "$FILES_SORTED" \
  "the fold gained exactly the six files.* names (the row toggle fanned out)"
assert_eq "$(set_minus "$FOLD_SNAP" "$FOLD_NOW")" "" \
  "the fan-out dropped nothing the lane fold already carried"
assert_eq "$(files_fold | wc -w | tr -d ' ')" "6" "the fold carries all six names"
for n in $FILES_NAMES; do
  fold_has "$n" || die "the lane home's config.yaml disabled: fold does not persist $n (the fan-out missed a member)"
done
step "ok: (e) model registry drops the domain, management read keeps it, fold persists all six"

# --- (f) re-enable: the fold clears and the row renders on again ----------
clicked=$(toggle_click "enable files" "__arm__")
assert_eq "$clicked" "1" "the single re-enable click landed"
# the mirror of (d): a press the lane dropped leaves the row off, so the button
# is still there to press. Re-drive once, then grade.
if ! poll_js "$ROW_IS_ON" 10; then
  step "the enable never reached the row (the fold still stands) - pressing once more"
  toggle_click "enable files" "__arm__" >/dev/null
fi
wait_js "$ROW_IS_ON" 10 \
  || die "one click did not re-enable (the files row never came back on)"
assert_eq "$(files_fold)" "" "the fold cleared on the wire"
assert_eq "$(wire_stamped)" "6 0" "the management read lists all six, none stamped disabled"
# The mirror of (e): both reads return to the pre-toggle snapshot, the lane
# owner's fold included. Nothing the recipe did may survive in either read.
assert_eq "$(wire_names)" "$WIRE_SNAP" "bare GET /tools returned to its pre-toggle snapshot (lane fold and all)"
assert_eq "$(fold_names)" "$FOLD_SNAP" "the fold returned to its pre-toggle snapshot (lane fold and all)"
for n in $FILES_NAMES; do
  fold_has "$n" && die "the lane home's config.yaml fold kept $n after the re-enable"
done
assert_eq "$(missing_files_detail)" "" "the re-enabled row keeps its six identities"
assert_eq "$(files_rows)" "1" "still exactly one files row after the round-trip"
assert_eq "$(dup_domains)" "" "still one row per domain after the round-trip"
step "ok: (f) re-enable cleared the fold (wire + config.yaml) and the row is on again"

assert_no_console_errors
step "114-tools-one-row GREEN"
