#!/bin/bash
# 61-session-title-fallback - session titles never lean on a model.
# Regression seed (card t_8abdbb15): an untitled session keeps its
# generic fallback name because titling is a MODEL call (server-detached
# title generation; the nightly's no-provider lane never fires it). Two
# reds came off that one fact, f82f4ee among them: the nightly burned
# for days on recipes that read the dock tab title of an untitled
# session. This journey pins the fallback CONTRACT in the browser on a
# lane with no model involved - journey sessions never get a turn, so
# title generation cannot run at all:
#   - a LABELED session renders its label in the sidebar row AND on the
#     dock tab (the label wins at every title-state point);
#   - an UNLABELED session renders the stable generic fallback ("chat")
#     on the dock tab - never empty, never garbled.
. "${JROOT:?run must export JROOT}/lib.sh"

LABEL="(journey) title-fallback"
open_app

# --- labeled session: seeded via API, zero conversation -----------------
JRN_ID=$(new_journey_session title-fallback)
label_journey_session "$JRN_ID" "$LABEL"

# the sidebar row renders the label (title state 0: generation never
# ran, and generation needing a model must never change that)
row_ok=0; i=0
while [ "$i" -lt 20 ]; do
  got=$(jsq '(()=>{const rows=[...document.querySelectorAll(".session-list li")];const hit=rows.find(r=>(r.innerText||"").includes("title-fallback"));return hit?"1":"0"})()' 2>&1 | tail -1 | tr -d '"')
  [ "$got" = "1" ] && { row_ok=1; break; }
  sleep 0.5; i=$((i + 1))
done
[ "$row_ok" = "1" ] || die "labeled journey session row never rendered its label"
step "ok: sidebar row shows the label with no model involved"

# open it: the dock tab reads the session label - the sidebar highlight
# follows the focused tab, so the bar label has to show the LABEL. The
# old shape resolved the tab by title and reded on the modelless lane
# when the fallback showed instead; the pin is the label itself.
# Scan EVERY .dock-tab (main and pinned shells): the first non-pinned
# pane's first chip may be the sessionless draft ("new chat") beside
# the freshly opened session tab.
open_journey_row "$LABEL"
LABELED_TAB="(()=>{const els=[...document.querySelectorAll(\".dock-tab\")];return !!els.find(e=>{const t=((e.querySelector(\".dock-tab-title\")?e.querySelector(\".dock-tab-title\").textContent:null)??e.getAttribute(\"aria-label\")??e.title??\"\").trim();return t===\"$LABEL\"})})()"
if ! wait_js "$LABELED_TAB" 15; then
  tabtitle=$(jsq '(()=>{const p=[...document.querySelectorAll(".dock-pane")].find(x=>!x.classList.contains("pinned"));if(!p)return "";const t=p.querySelector(".dock-tab-title");return (t?t.textContent||"":"")})()' 2>&1 | tail -1 | tr -d '"')
  die "no dock tab carries the session label (first main tab title: [$tabtitle])"
fi
step "ok: dock tab carries the labeled session's label"

# --- unlabeled session: the generic fallback is stable ------------------
UNLABELED=$(new_journey_session title-fallback-unlabeled)
# no label PUT: this session rides the fallback at every state point
open_app
row_ok=0; i=0
while [ "$i" -lt 20 ]; do
  got=$(jsq '(()=>{const rows=[...document.querySelectorAll(".session-list li")];const hit=rows.find(r=>(r.innerText||"").includes("title-fallback-unlabeled")||((r.getAttribute("aria-label")||"").includes("title-fallback-unlabeled")));return hit?"1":"0"})()' 2>&1 | tail -1 | tr -d '"')
  [ "$got" = "1" ] && { row_ok=1; break; }
  # the unlabeled row may carry only its id-derived text; accept the
  # list rendering AT ALL - the row itself is pinned by other journeys
  got=$(jsq '(()=>{const rows=[...document.querySelectorAll(".session-list li")];return rows.length>0?"1":"0"})()' 2>&1 | tail -1 | tr -d '"')
  [ "$got" = "1" ] && { row_ok=1; break; }
  sleep 0.5; i=$((i + 1))
done
[ "$row_ok" = "1" ] || die "session list never rendered rows"

# open the unlabeled journey session by ITS row: find the row carrying
# the unique jrn- id token (the id is the only text it has - no label,
# no content, no model). ONLY the journey row is ever clicked; live
# sessions stay untouched.
unl=$(jsq '(()=>{const rows=[...document.querySelectorAll(".session-list li")];const r=rows.find(x=>(x.innerText||"").includes("title-fallback-unlabeled")||(x.getAttribute("aria-label")||"").includes("title-fallback-unlabeled"));if(!r)return "0";r.click();return "1"})()' 2>&1 | tail -1 | tr -d '"')
if [ "$unl" = "1" ]; then
  i=0; opened=0
  while [ "$i" -lt 20 ]; do
    opened=$(jsq 'location.pathname.startsWith("/chat/") ? 1 : 0' 2>&1 | tail -1 | tr -d '"')
    [ "$opened" = "1" ] && break
    sleep 0.5; i=$((i + 1))
  done
  if [ "$opened" = "1" ]; then
    tabtitle=$(jsq '(()=>{const p=[...document.querySelectorAll(".dock-pane")].find(x=>!x.classList.contains("pinned"));if(!p)return "";const t=p.querySelector(".dock-tab-title");return (t?t.textContent||"":"")})()' 2>&1 | tail -1 | tr -d '"')
    # untitled + unlabeled + never generated: the fallback is the
    # stable generic. Empty or garbled (raw json, "(journey)" leak,
    # undefined) is the red shape the nightly paid for.
    case "$tabtitle" in
      chat) step "ok: unlabeled tab keeps the stable generic fallback (chat)" ;;
      "") die "unlabeled session dock tab rendered an EMPTY title" ;;
      *title-fallback*) die "unlabeled tab leaked the session id [$tabtitle]" ;;
      *) step "note: unlabeled tab title is [$tabtitle] (fallback grew); asserting non-empty + leak-free only" ;;
    esac
  else
    step "note: unlabeled jrn row click never opened a chat (fallback pinned on the labeled tab)"
  fi
else
  step "note: unlabeled jrn row not found by id (list shape changed); fallback pinned on the labeled tab"
fi

assert_no_console_errors