#!/bin/bash
# 83-session-context - the pinned context journey (t_9de5b009): the
# model picker and the workspace chip WRITE the session row, and a
# reload restores both on reopen - "sticks" is restore, not just an
# optimistic chip. Wire truth is the /sessions row (model + workspace
# fields); presentation truth is the composer chips after a fresh SPA
# load.
JOURNEY_NEEDS_MODEL=1
. "${JROOT:?run must export JROOT}/lib.sh"

# fresh_open: same stale-editing-state trap as 77-session-branch - a
# long-lived chrome keeps SPA state, about:blank forces a real remount.
fresh_open() { ab open "about:blank" >/dev/null 2>&1; open_app; }

# deq: ab eval prints JS strings JSON-quoted ("beam") - a value compared
# against row_field (plain) or interpolated into a JS string comparator
# must be unquoted first (the house dequote, cf 30-retry's sed). Strip
# the outer quote pair, unescape the inner ones.
deq() { sed -e 's/^"//' -e 's/"$//' -e 's/\\"/"/g'; }

# jstr: shell value -> a JS string literal (every backslash doubled,
# every quote escaped). Windows drive paths carry backslashes; a raw
# interpolation would eat them ("\U" becomes U, "\t" a tab) inside the
# double-quoted JS comparators. Same escaping a JSON body needs.
jstr() { printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g'; }

# row_model / row_workspace: wire truth from the /sessions row.
row_field() {
  curl -s "$JOURNEY_BASE_URL/sessions" | python3 -c "
import sys, json
rows = json.load(sys.stdin)
for r in rows:
    if r['id'] == '$1':
        print(r.get('$2', '') or '')
        break
"
}

JRN_ID=$(new_journey_session ctx-pin | tail -1)
TAG=$(journey_tag)
LABEL="(journey) ctx-pin $TAG"
label_journey_session "$JRN_ID" "$LABEL"

fresh_open
open_journey_row "$LABEL"

# --- the model picker writes the row --------------------------------
# the chip waits for the models fetch to seed it (boot fires /models
# without blocking; the chip fills within ~a second on a healthy lane)
wait_js '!!(document.querySelector(".model-select:not(.thinking-select) .model-chip .chip-name")?.textContent ?? "")' 10 \
  || die "model chip did not render"
default=$(jsq 'document.querySelector(".model-select:not(.thinking-select) .model-chip .chip-name")?.textContent ?? ""' 2>&1 | tail -1 | deq)
[ -n "$default" ] || die "model chip rendered empty"

jsq 'document.querySelector(".model-select:not(.thinking-select) .model-chip")?.click()' >/dev/null 2>&1
wait_js '!!document.querySelector(".model-list li")' 10
# pick the first option that differs from the chip's current model
# target is read INSIDE js (cur comes from the DOM) - it exits as a
# plain model name once deq'd, safe to interpolate into the li matcher
target=$(jsq '(()=>{const cur = document.querySelector(".model-select:not(.thinking-select) .model-chip .chip-name")?.textContent ?? ""; const opts = Array.from(document.querySelectorAll(".model-list li")); const other = opts.find(li => (li.textContent || "").trim() && (li.textContent || "").trim() !== cur.trim()); return other ? (other.textContent || "").trim() : "";})()' 2>&1 | tail -1 | deq)
if [ -z "$target" ]; then
  # one-model lane: the picker cannot move anything - the pin halves
  # below still prove the restore path over a wire-written pin
  step "lane serves one model - picker change skipped"
  curl -s -X PATCH "$JOURNEY_BASE_URL/sessions/$JRN_ID" \
    -H 'Content-Type: application/json' -d "{\"model\": \"$default\"}" >/dev/null
else
  jsq "(()=>{const li = Array.from(document.querySelectorAll(\".model-list li\")).find(li => (li.textContent || \"\").trim() === \"$target\"); if (!li) return 0; li.click(); return 1;})()" >/dev/null 2>&1
  pinned=0
  for i in 1 2 3 4 5 6 7 8 9 10; do
    pinned=$(row_field "$JRN_ID" model)
    [ "$pinned" = "$target" ] && break
    sleep 1
  done
  assert_eq "$pinned" "$target" "picker change wrote the session row (model)"
fi

# --- reload: the pin restores ---------------------------------------
fresh_open
open_journey_row "$LABEL"
restored=$(jsq 'document.querySelector(".model-select:not(.thinking-select) .model-chip .chip-name")?.textContent ?? ""' 2>&1 | tail -1 | deq)
assert_eq "$restored" "$target" "pinned model restored on reopen"

# --- the workspace chip writes the row ------------------------------
# t_ca699ff3: the pin target is derived from the LANE over the wire,
# never hardcoded - a non-Linux lane's home is a different shape, and
# its root listing is a drive root. t_ccf74d88's effective-workdir law
# decides WHERE the popover roots itself: a bound, un-pinned session's
# chip presents the knob, so the click strand rides a row of the knob's
# listing (a browsable child the cleared chip cannot mirror); a
# knobless lane's chip opens at the drive root ("/" on linux,
# volume-qualified on windows: t_7dcdc98e) and prefers the dir named
# "home" when one exists (every Linux box: the pinned value stays what
# this journey always asserted, byte-identical). Fallback: pins are
# wire-written when no row the popover would render is left to click
# (the same lane-capability precedent as the one-model strand above);
# the restore and clear strands below stay fully UI-driven. RESOLVED
# is the listing's own path shape - the same symlink-resolved spelling
# the PATCH stores.
WSPICK=$(JOURNEY_BASE_URL="$JOURNEY_BASE_URL" python3 -c '
import json, os, sys, urllib.parse, urllib.request
opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
root_url = os.environ["JOURNEY_BASE_URL"].rstrip("/")
base = root_url + "/workspaces/browse"
def browse(p):
    url = base if p is None else base + "?" + urllib.parse.urlencode({"path": p})
    return json.load(opener.open(url, timeout=10))
dirs = browse(None).get("dirs", [])
knob = ""
try:
    settings = json.load(opener.open(root_url + "/settings", timeout=10))
    for g in settings.get("groups", []):
        for k in g.get("knobs", []):
            if k.get("key") == "server.default_workspace":
                knob = k.get("value") or ""
except Exception:
    pass
target = resolved = ""
def try_row(row):
    global target, resolved
    try:
        resolved = browse(row["path"])["path"]
        target = row["path"]
        return True
    except Exception:
        return False
strand = ""
# t_ccf74d88 effective-workdir law in the UI: a bound, unpinned
# session chip presents the knob, so the popover opens at the knob
# listing - only ITS rows are clickable there. A knob lane pin
# therefore rides a knob-listing row (a browsable child, a value the
# cleared chip cannot mirror); the root listing home preference
# only applies where the chip has no knob to present (knobless lanes,
# linux/CI: byte-identical).
if knob:
    try:
        kres = browse(knob)["path"]
        for d in browse(kres).get("dirs", []):
            if try_row(d):
                strand = "ui"
                break
    except Exception:
        pass
if strand != "ui" and not knob:
    pref = [d for d in dirs if d["name"] == "home"]
    for row in pref + dirs:
        if try_row(row):
            strand = "ui"
            break
if strand != "ui" and knob:
    # wire strand: the knob has no browsable child row to click -
    # pin a browsable child directly if one exists (a pin distinct
    # from the knob keeps the restore-vs-clear chip states visibly
    # different on every lane), else the knob itself.
    target = resolved = ""
    try:
        kres = browse(knob)["path"]
        resolved = kres
        for d in browse(kres).get("dirs", []):
            try:
                resolved = browse(d["path"])["path"]
                break
            except Exception:
                continue
        strand = "wire"
    except Exception:
        pass
if strand != "ui" and strand != "wire":
    strand = "none"
sys.stdout.reconfigure(newline="\n")
print(strand + "\t" + target + "\t" + resolved + "\t" + knob)
') || die "workspace lane probe crashed"
STRAND=$(printf '%s' "$WSPICK" | cut -f1)
TARGET=$(printf '%s' "$WSPICK" | cut -f2)
RESOLVED=$(printf '%s' "$WSPICK" | cut -f3)
KNOB=$(printf '%s' "$WSPICK" | cut -f4)
if [ "$STRAND" != "ui" ] && [ "$STRAND" != "wire" ]; then
  die "lane offers no browsable workspace pin (no usable root row, default_workspace unset)"
fi

if [ "$STRAND" = "ui" ]; then
  jsq 'document.querySelector(".ws-btn")?.click()' >/dev/null 2>&1
  wait_js '!!document.querySelector(".ws-pop .ws-dirs .ws-dir")' 10 \
    || die "workspace popover did not render the lane listing (knob dir on a knob lane, the root else)"
  clicked=$(jsq "(()=>{const t = \"$(jstr "$TARGET")\"; const b = Array.from(document.querySelectorAll(\".ws-pop .ws-dir\")).find(x => (x.getAttribute(\"title\") || \"\") === t); if (!b) return 0; b.click(); return 1;})()" 2>&1 | tail -1 | deq)
  assert_eq "$clicked" "1" "popover rendered the lane-derived workspace dir"
  # descend into it: the crumb carries the RESOLVED dir path (the same
  # wire shape the listing serves and the PATCH stores)
  wait_js "(()=>{const c = document.querySelector(\".ws-pop .ws-crumb\"); return !!c && c.getAttribute(\"title\") === \"$(jstr "$RESOLVED")\";})()" 10 \
    || die "workspace popover never landed on the lane-derived dir"
  jsq 'document.querySelector(".ws-pop .ws-apply")?.click()' >/dev/null 2>&1
  step "workspace pin rides the popover browse"
else
  step "no popover-clickable workspace row - write strand rides the wire, restore+clear stay UI"
  curl -s -X PATCH "$JOURNEY_BASE_URL/sessions/$JRN_ID" \
    -H 'Content-Type: application/json' \
    -d "{\"workspace\": \"$(jstr "$RESOLVED")\"}" >/dev/null
fi

ws=""
for i in 1 2 3 4 5 6 7 8 9 10; do
  ws=$(row_field "$JRN_ID" workspace)
  [ "$ws" = "$RESOLVED" ] && break
  sleep 1
done
assert_eq "$ws" "$RESOLVED" "workspace chip wrote the session row"

# --- reload: the folder restores ------------------------------------
# the chip name is the product's baseName shape (the last /-segment;
# a windows-drive value contains no "/", so the whole value renders -
# the same mirror the product's p.split("/").pop() performs).
# compared inside js - the deq house sed does not unescape doubled
# backslashes, a windows-drive chip value would read back mangled.
fresh_open
open_journey_row "$LABEL"
case "$RESOLVED" in
  */*) ws_base=${RESOLVED##*/} ;;
  *)   ws_base=$RESOLVED ;;
esac
ws_ok=$(jsq "(()=>{const t = document.querySelector(\".ws-btn .chip-name:not(.ws-unset)\")?.textContent ?? \"\"; return t === \"$(jstr "$ws_base")\";})()" 2>&1 | tail -1)
assert_eq "$ws_ok" "true" "pinned workspace restored on reopen"

# --- clear: the chip drops the pin ----------------------------------
jsq 'document.querySelector(".ws-btn")?.click()' >/dev/null 2>&1
wait_js '!!document.querySelector(".ws-pop .ws-clear")' 10
jsq 'document.querySelector(".ws-pop .ws-clear")?.click()' >/dev/null 2>&1
cleared=0
for i in 1 2 3 4 5 6 7 8 9 10; do
  cleared=$(row_field "$JRN_ID" workspace)
  [ -z "$cleared" ] && break
  sleep 1
done
assert_eq "$cleared" "" "workspace clear removed the pin"
# the chip after clear mirrors the t_ccf74d88 effective-workdir law:
# an empty row pin falls back to the server's default_workspace knob -
# the chip only renders the unset blank on a knob-less lane. On a knob
# lane the cleared chip keeps showing the knob's basename. Render
# states lag the wire by a SPA beat - poll, never one-shot read.
if [ -z "$KNOB" ]; then
  wait_js '!!document.querySelector(".ws-btn .ws-unset")' 10 \
    || die "chip never rendered the unset state after clear"
else
  knob_base=${KNOB##*/}
  knob_ok=$(jsq "(()=>{const t = document.querySelector(\".ws-btn .chip-name:not(.ws-unset)\")?.textContent ?? \"\"; return t === \"$(jstr "$knob_base")\";})()" 2>&1 | tail -1)
  assert_eq "$knob_ok" "true" "cleared chip mirrors the default workspace knob"
fi

assert_no_console_errors
step "journey green"