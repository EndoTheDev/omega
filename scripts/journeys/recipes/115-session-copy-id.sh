#!/bin/bash
# 115-session-copy-id - the sessions sidebar's copy-id row action (card
# t_61cc8762, Endo's 0.17.2 batch item 3): hover a session row -> the
# copy affordance appears; click it -> the row's EXACT session id reaches
# the clipboard path and the shell's toast stack confirms it.
#
# The node suite (webui/test/clipboard.test.ts) pins the source shape -
# the ONE copy path, the funnel payload, the hover-cluster placement.
# THIS recipe is the browser witness: real pointer hover, real pointer
# click, the rendered toast.
#
# Clipboard BYTES: this lane is a private http://127.0.0.1 origin, which
# Chromium treats as a secure context, but clipboard READ is permission
# gated and run.sh grants nothing - `ab clipboard read` answers
# NotAllowedError (probed on this lane). So the payload receipt rides the
# app's OWN call: navigator.clipboard.writeText is instrumented before
# the click and the recorded argument must be the row's id, with the
# original primitive still doing the write. A permission-granted
# readText round trip is the separate browser_exec probe on the lane.
. "${JROOT:?run must export JROOT}/lib.sh"

SID=$(new_journey_session copy-id | tail -1)
TAG=$(journey_tag)
LABEL="(journey) copy-id $TAG"
label_journey_session "$SID" "$LABEL"

# REST-seed law: the sidebar has no fetch poller (the push channel
# replaced every poller), so a session created through POST /sessions
# never arrives in an already-open page - load AFTER the seed.
open_app

# expr <tmpl>: substitute the row label + the session id. Keeps the JS
# free of shell-quoting gymnastics (the label carries spaces/parens).
expr() {
  local t="$1"
  t=${t//__LABEL__/$LABEL}
  t=${t//__SID__/$SID}
  printf '%s' "$t"
}

# --- the row lands on the /sessions answer after boot: poll, never a
# one-shot read --------------------------------------------------------------
ROW_JS='
(()=>{
  const L="__LABEL__";
  const rows=[...document.querySelectorAll(".session-list li")];
  const r=rows.find(x=>{const t=x.querySelector(".session-title");return t&&t.textContent.trim()===L;});
  return r;
})()'

row_ready=0
for i in $(seq 1 20); do
  row_ready=$(jsq "$(expr "${ROW_JS%$'\n'}")?1:0" 2>&1 | tail -1)
  [ "$row_ready" = "1" ] && break
  sleep 1
done
assert_eq "$row_ready" "1" "journey row rendered in the sessions sidebar"

# --- pre-hover: the affordance is HIDDEN (the hover-reveal contract) --------
pre=$(jsq "$(expr '
(()=>{
  const L="__LABEL__";
  const rows=[...document.querySelectorAll(".session-list li")];
  const r=rows.find(x=>{const t=x.querySelector(".session-title");return t&&t.textContent.trim()===L;});
  if(!r) return "no-row";
  const c=r.querySelector(".row-actions");
  const b=r.querySelector(".row-copy-id");
  if(!c||!b) return "no-cluster";
  const cs=getComputedStyle(c);
  return (cs.opacity==="0"&&cs.pointerEvents==="none")?1:0;
})()')" 2>&1 | tail -1)
assert_eq "$pre" "1" "copy affordance hidden before the pointer arrives"

# --- real pointer hover over the row body ----------------------------------
ROWC_JS='
(()=>{
  const L="__LABEL__";
  const rows=[...document.querySelectorAll(".session-list li")];
  const r=rows.find(x=>{const t=x.querySelector(".session-title");return t&&t.textContent.trim()===L;});
  if(!r) return "";
  const b=r.getBoundingClientRect();
  return Math.round(b.x+40)+" "+Math.round(b.y+b.height/2);
})()'
pt=$(jsq "$(expr "$ROWC_JS")" 2>&1 | tail -1)
pt=${pt//\"/}
rowx=${pt%% *}
rowy=${pt##* }
assert_ne "$rowx" "" "row centre resolved"
ab mouse move "$rowx" "$rowy" >/dev/null 2>&1

revealed=0
for i in $(seq 1 20); do
  revealed=$(jsq "$(expr '
(()=>{
  const L="__LABEL__";
  const rows=[...document.querySelectorAll(".session-list li")];
  const r=rows.find(x=>{const t=x.querySelector(".session-title");return t&&t.textContent.trim()===L;});
  if(!r) return 0;
  const c=r.querySelector(".row-actions");
  const cs=getComputedStyle(c);
  return (cs.opacity==="1"&&cs.pointerEvents==="auto")?1:0;
})()')" 2>&1 | tail -1)
  [ "$revealed" = "1" ] && break
  sleep 0.25
done
assert_eq "$revealed" "1" "hovering the row reveals the copy affordance"

# --- instrument the ONE copy path's primitive, then click ------------------
# The wrapper records what the APP hands over while the original still
# performs the write; a fallback to execCommand keeps the record intact.
jsq '(()=>{
  window.__copyArgs=[];
  const c=navigator.clipboard;
  if(c&&typeof c.writeText==="function"){
    const o=c.writeText.bind(c);
    Object.defineProperty(c,"writeText",{configurable:true,value:(t)=>{window.__copyArgs.push(t);return o(t);}});
  }
  return "instrumented";
})()' >/dev/null 2>&1

BTN_JS='
(()=>{
  const L="__LABEL__";
  const rows=[...document.querySelectorAll(".session-list li")];
  const r=rows.find(x=>{const t=x.querySelector(".session-title");return t&&t.textContent.trim()===L;});
  if(!r) return "";
  const b=r.querySelector(".row-copy-id");
  if(!b) return "";
  const rb=b.getBoundingClientRect();
  return Math.round(rb.x+rb.width/2)+" "+Math.round(rb.y+rb.height/2);
})()'
pt=$(jsq "$(expr "$BTN_JS")" 2>&1 | tail -1)
pt=${pt//\"/}
cx=${pt%% *}
cy=${pt##* }
assert_ne "$cx" "" "copy button centre resolved"

ab mouse move "$cx" "$cy" >/dev/null 2>&1
sleep 0.2
ab mouse down >/dev/null 2>&1
ab mouse up >/dev/null 2>&1

# --- the live toast confirms, naming the id --------------------------------
toast=0
for i in $(seq 1 20); do
  toast=$(jsq "$(expr '(()=>{
    const want="copied session id __SID__";
    return [...document.querySelectorAll(".notif-toast")].some(t=>(t.innerText||"").includes(want))?1:0;
  })()')" 2>&1 | tail -1)
  [ "$toast" = "1" ] && break
  sleep 0.25
done
assert_eq "$toast" "1" "the toast stack confirms the copy and names the id"

# --- the payload receipt: the app's own clipboard call carried the id ------
payload=$(jsq '(()=>{const a=(window.__copyArgs||[]);return a.length?a[a.length-1]:""})()' 2>&1 | tail -1)
payload=${payload//\"/}
assert_eq "$payload" "$SID" "the row's session id is what the copy path wrote"

# --- .stop: a copy never opens the session ---------------------------------
route=$(jsq 'location.pathname' 2>&1 | tail -1 | tr -d '"')
assert_eq "$route" "/" "the copy click never opens the session row"

assert_no_console_errors
step "ok: session copy-id action verified live"
