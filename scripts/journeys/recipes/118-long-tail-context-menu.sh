#!/bin/bash
# 118-long-tail-context-menu - the right-click arc's last step: the long-tail chrome
# surfaces (panes, the settings tab) and the terminal.
#
# The shared menu (composable contextMenu.ts + the one ui/ContextMenu.vue surface) reached
# dock tabs (116) and the row/statusbar/sidebar surfaces (117). This recipe proves the
# remaining chrome, through the REAL input pipeline - a real right-button press, the real
# rows, and a real verb fired from each:
#
#   pane      a main pane's own area menus its verbs: Split right / Split down while it is
#             the only main pane, plus Close pane (the standard row) once a close exists.
#             Close pane acts, Escape closes, and a pinned pane (a sidebar, the bottom
#             shell) registers nothing - its right-click stays the browser's.
#   settings  the settings tab is a main-area tab: its one standard row is Close, offered
#             only where the dock would let its X close it, and Close closes the tab.
#   terminal  chrome by the design line, so right-click is the terminal's own menu:
#             Copy / Paste / Clear. Clear drops the scrollback, Copy carries a real
#             selection through the one copy path, Paste hands the clipboard to xterm and
#             the shell runs it.
#
# The rows and the registration are pinned in webui/test/panemenu.test.ts,
# settingsmenu.test.ts and terminalmenu.test.ts. This recipe is the browser half.
#
# Residue: the bottom pane's terminal claims a pty; the trap DELETEs every session id this
# run minted (the before/after diff of GET /terminal/sessions), so the suite leaves no
# shell behind.
. "${JROOT:?run must export JROOT}/lib.sh"
B="${JOURNEY_BASE_URL:?}"

term_ids() {
  curl -s "$B/terminal/sessions" | grep -oE '"id": ?"[a-f0-9-]+"' | sed 's/.*"\([a-f0-9-]*\)".*/\1/' | sort
}
TERM_BEFORE=$(term_ids)
cleanup_term() {
  local id
  for id in $(comm -13 <(printf '%s\n' "$TERM_BEFORE") <(term_ids)); do
    curl -s -X DELETE "$B/terminal/$id" >/dev/null 2>&1 || true
  done
}
cleanup() { cleanup_term; }
trap cleanup EXIT

TAG=$(date +%d%b%H%M%S | tr 'A-Z' 'a-z')
PROBE="jrn-pane-menu-$TAG"

menu_rows() {
  local r
  r=$(jsq '[...document.querySelectorAll(".context-menu-item .context-menu-label")].map(e=>e.textContent.trim()).join("|")')
  printf '%s' "${r//\"/}"
}

right_click_at() {
  ab mouse move "$1" "$2" >/dev/null 2>&1
  sleep 0.15   # recipe-lint: allow fixed-sleep - input pacing: the real pointer
               # pipeline coalesces a move and a press that land in one task
  ab mouse down right >/dev/null 2>&1
  ab mouse up right >/dev/null 2>&1
}

# right_click_js <expr returning "x y">
right_click_js() {
  local box
  box=$(jsq "$1")
  box=${box//\"/}
  [ -n "$box" ] || die "the right-click target never rendered: $1"
  right_click_at $box
}

click_row() {
  local label=$1 box
  box=$(jsq "(() => {const b=[...document.querySelectorAll('.context-menu-item')].find(x=>((x.querySelector('.context-menu-label')||{}).textContent||'').trim()==='$label'); if(!b) return ''; const r=b.getBoundingClientRect(); return Math.round(r.x+r.width/2)+' '+Math.round(r.y+r.height/2);})()")
  box=${box//\"/}
  [ -n "$box" ] || die "no menu row named $label"
  ab mouse move $box >/dev/null 2>&1
  sleep 0.15   # recipe-lint: allow fixed-sleep - input pacing: move and press pacing
  ab mouse down >/dev/null 2>&1
  ab mouse up >/dev/null 2>&1
}

# the MAIN panes (a pinned shell - a sidebar, the bottom panel - is not one)
MAIN_PANES='document.querySelectorAll(".dock-pane:not(.pinned)")'

# the pane strip's free band: the gap between the last chip and the strip's right-hand
# buttons (the tablist itself is content-sized, so its own box is no target)
STRIP_FREE_JS='(() => {
  const pane = document.querySelector(".dock-pane:not(.pinned)");
  if (!pane) return "";
  const bar = pane.querySelector(".dock-tabbar");
  if (!bar) return "";
  const br = bar.getBoundingClientRect();
  const list = pane.querySelector(".dock-tabs");
  const firstBtn = pane.querySelector(".dock-tab-btn");
  let x = br.right - 60;
  if (list && firstBtn) {
    const gapStart = list.getBoundingClientRect().right;
    const gapEnd = firstBtn.getBoundingClientRect().left;
    if (gapEnd - gapStart > 24) x = (gapStart + gapEnd) / 2;
  }
  return Math.round(x) + " " + Math.round(br.y + br.height / 2);
})()'

open_app
step "open: fresh boot (one main pane, the bottom shell and its terminal)"

# --- 1. the pane area -----------------------------------------------------
right_click_js "$STRIP_FREE_JS"
wait_js '(() => !!document.querySelector(".context-menu.measured"))()' 10
assert_eq "$(menu_rows)" "Split right|Split down" "a lone main pane menus its split verbs and no Close pane - there is no close today"
step "ok: the pane area's right-click opens the shared menu"

ab press Escape >/dev/null 2>&1
if ! wait_js '(() => !document.querySelector(".context-menu"))()' 10; then
  die "Escape did not close the pane menu"
fi
step "ok: Escape closes it"

# Split right runs through the row: the dock gains a second main pane (the fresh EMPTY one)
right_click_js "$STRIP_FREE_JS"
wait_js '(() => !!document.querySelector(".context-menu.measured"))()' 10
click_row "Split right"
if ! wait_js "${MAIN_PANES}.length === 2" 15; then
  die "Split right left $(${MAIN_PANES}.length 2>/dev/null || echo 0) main panes"
fi
step "ok: Split right makes the second main pane"

# the new pane's EMPTY area is a pane surface too, and NOW a close exists (two main panes)
right_click_js '(() => {
  const el = document.querySelector(".dock-pane:not(.pinned) .dock-empty");
  if (!el) return "";
  const r = el.getBoundingClientRect();
  return Math.round(r.x + r.width / 2) + " " + Math.round(r.y + r.height / 2);
})()'
wait_js '(() => !!document.querySelector(".context-menu.measured"))()' 10
assert_eq "$(menu_rows)" "Close pane|Split right|Split down" "the standard Close pane leads, the pane's own verbs follow the rule"
step "ok: an empty pane area menus Close pane first"

click_row "Close pane"
if ! wait_js "${MAIN_PANES}.length === 1" 15; then
  die "Close pane left $(${MAIN_PANES}.length 2>/dev/null || echo 0) main panes"
fi
if ! wait_js '(() => !document.querySelector(".context-menu"))()' 10; then
  die "the menu stayed open after Close pane ran"
fi
step "ok: Close pane closes the pane it was opened on"

# --- 2. a pinned pane registers nothing -----------------------------------
right_click_js '(() => {
  const bar = document.querySelector(".dock-pane.edge-bottom .dock-tabbar");
  if (!bar) return "";
  const r = bar.getBoundingClientRect();
  return Math.round(r.right - 6) + " " + Math.round(r.y + r.height / 2);
})()'
sleep 0.4   # recipe-lint: allow fixed-sleep - absence check: the menu opens
            # synchronously on contextmenu, so a short settle cannot hide a late one
open_now=$(jsq 'document.querySelector(".context-menu")?1:0')
assert_eq "$open_now" "0" "the bottom shell offers no pane verbs - no dead menu"
step "ok: a pinned pane keeps the browser's own menu"

# --- 3. the settings tab --------------------------------------------------
jsq '(() => {const b=document.querySelector("header.menubar button[aria-label=\"Open settings\"]"); if(!b) return 0; b.click(); return 1})()' >/dev/null
SETTINGS_TABS='document.querySelectorAll(".dock-pane:not(.pinned) .dock-tab")'
has_settings="(() => [...${SETTINGS_TABS}].some(t=>((t.getAttribute('title')||'').trim())==='Settings'))()"
no_settings="(() => ![...${SETTINGS_TABS}].some(t=>((t.getAttribute('title')||'').trim())==='Settings'))()"
if ! wait_js "$has_settings" 15; then
  die "the menubar gear opened no Settings tab"
fi

right_click_js '(() => {
  const nav = document.querySelector(".settings-nav");
  if (!nav) return "";
  const r = nav.getBoundingClientRect();
  return Math.round(r.x + r.width / 2) + " " + Math.round(r.y + r.height / 2);
})()'
wait_js '(() => !!document.querySelector(".context-menu.measured"))()' 10
assert_eq "$(menu_rows)" "Close" "the settings tab's one standard row is Close"
step "ok: the settings tab's right-click opens the shared menu"

ab press Escape >/dev/null 2>&1
if ! wait_js '(() => !document.querySelector(".context-menu"))()' 10; then
  die "Escape did not close the settings menu"
fi
step "ok: Escape closes it"

right_click_js '(() => {
  const nav = document.querySelector(".settings-nav");
  if (!nav) return "";
  const r = nav.getBoundingClientRect();
  return Math.round(r.x + r.width / 2) + " " + Math.round(r.y + r.height / 2);
})()'
wait_js '(() => !!document.querySelector(".context-menu.measured"))()' 10
click_row "Close"
if ! wait_js "$no_settings" 15; then
  die "the Close row left the Settings tab up"
fi
step "ok: Close closes the settings tab"

# --- 4. the terminal ------------------------------------------------------
# Every buffer read strips whitespace: the bottom pane is 200px tall, so a long marker line
# WRAPS onto the next row and the rendered text breaks mid-token.
#
# The state line IS the pane's claim report - "connecting..." while a claim is in flight,
# "shell" once the stream has replayed the pty, "no terminal" with the module's own reason
# in `.terminal-error` when a claim is refused. Poll that exact predicate, and on a miss
# print what the pane actually says: the bare boolean told two nightlies nothing.
TERM_STATE_READ='(() => {
  const el = document.querySelector(".terminal-state");
  return JSON.stringify({
    mounted: !!el,
    state: el ? el.textContent.trim() : null,
    error: (document.querySelector(".terminal-error") || { textContent: "" }).textContent.trim(),
  });
})()'
if ! poll_js '(() => (document.querySelector(".terminal-state")||{textContent:""}).textContent.trim()==="shell")()' 25; then
  die "the bottom terminal never reached the shell state: $(jsq "$TERM_STATE_READ" 2>&1 | tail -1)"
fi
step "ok: the bottom terminal claimed a shell"

# focus the shell with a real press, then type a marker line. The typed line is the only
# proof the keys reached the pty, and a press that lands while the pane is still settling
# leaves the xterm textarea unfocused (the shell then never sees a key) - so the focus is
# its own observable, and the type is retried only while the buffer stays silent.
right_click_free='(() => {
  const host = document.querySelector(".dock-pane.edge-bottom .terminal-host");
  if (!host) return "";
  const r = host.getBoundingClientRect();
  return Math.round(r.x + r.width / 2) + " " + Math.round(r.y + r.height / 2);
})()'
term_point=$(jsq "$right_click_free")
term_point=${term_point//\"/}
[ -n "$term_point" ] || die "the terminal host never rendered"

focus_term() {
  ab mouse move $term_point >/dev/null 2>&1
  sleep 0.15   # recipe-lint: allow fixed-sleep - input pacing: focus press pacing
  ab mouse down >/dev/null 2>&1
  ab mouse up >/dev/null 2>&1
  poll_js '(() => (document.activeElement || {className: ""}).className.toString().includes("xterm-helper-textarea"))()' 10
}

typed=0
for attempt in 1 2 3; do
  focus_term || true
  ab keyboard type "echo $PROBE" >/dev/null 2>&1
  ab press Enter >/dev/null 2>&1
  if poll_js "(() => (document.querySelector('.dock-pane.edge-bottom .xterm-rows')||{innerText:''}).innerText.replace(/\s+/g,'').includes('$PROBE'))()" 10; then
    typed=1
    break
  fi
done
[ "$typed" = "1" ] || die "the shell never echoed $PROBE in $attempt attempts - the terminal is not receiving keys"
step "ok: the shell echoes the probe line"

# the terminal's own menu, over the shell's buffer
right_click_js "$right_click_free"
wait_js '(() => !!document.querySelector(".context-menu.measured"))()' 10
assert_eq "$(menu_rows)" "Copy|Paste|Clear" "the terminal menus Copy, Paste, Clear"
dis=$(jsq '(() => {const b=[...document.querySelectorAll(".context-menu-item")].find(x=>((x.querySelector(".context-menu-label")||{}).textContent||"").trim()==="Copy"); return b?String(b.disabled):"missing";})()')
assert_eq "${dis//\"/}" "true" "Copy is disabled with no selection - the row never lies"
step "ok: right-click in the terminal opens the terminal's menu"

ab press Escape >/dev/null 2>&1
if ! wait_js '(() => !document.querySelector(".context-menu"))()' 10; then
  die "Escape did not close the terminal menu"
fi
step "ok: Escape closes it"

# Clear drops the scrollback: the probe line leaves the rendered buffer
right_click_js "$right_click_free"
wait_js '(() => !!document.querySelector(".context-menu.measured"))()' 10
click_row "Clear"
if ! wait_js "(() => !(document.querySelector('.dock-pane.edge-bottom .xterm-rows')||{innerText:''}).innerText.replace(/\s+/g,'').includes('$PROBE'))()" 15; then
  die "Clear left the probe line in the buffer"
fi
step "ok: Clear drops the scrollback"

# Copy: a real drag-select, then the row carries it through the one copy path
jsq '(() => {
  window.__copyArgs = [];
  const c = navigator.clipboard;
  if (c && typeof c.writeText === "function") {
    const o = c.writeText.bind(c);
    Object.defineProperty(c, "writeText", { configurable: true, value: (t) => { window.__copyArgs.push(t); return o(t); } });
  }
  return "instrumented";
})()' >/dev/null 2>&1

ab keyboard type "echo $PROBE-copy" >/dev/null 2>&1
ab press Enter >/dev/null 2>&1
wait_js "(() => (document.querySelector('.dock-pane.edge-bottom .xterm-rows')||{innerText:''}).innerText.replace(/\s+/g,'').includes('$PROBE-copy'))()" 20
drag=$(jsq '(() => {
  const rows = document.querySelector(".dock-pane.edge-bottom .xterm-rows");
  if (!rows) return "";
  const r = rows.getBoundingClientRect();
  return Math.round(r.x + 4) + " " + Math.round(r.y + 4) + " " + Math.round(r.x + r.width - 8) + " " + Math.round(r.y + r.height - 6);
})()')
drag=${drag//\"/}
[ -n "$drag" ] || die "the terminal buffer has no box to select"
set -- $drag
ab mouse move "$1" "$2" >/dev/null 2>&1
sleep 0.15   # recipe-lint: allow fixed-sleep - input pacing: the drag's own start
ab mouse down >/dev/null 2>&1
ab mouse move "$3" "$4" >/dev/null 2>&1
sleep 0.15   # recipe-lint: allow fixed-sleep - input pacing: the drag's travel before release
ab mouse up >/dev/null 2>&1

right_click_js "$right_click_free"
wait_js '(() => !!document.querySelector(".context-menu.measured"))()' 10
dis=$(jsq '(() => {const b=[...document.querySelectorAll(".context-menu-item")].find(x=>((x.querySelector(".context-menu-label")||{}).textContent||"").trim()==="Copy"); return b?String(b.disabled):"missing";})()')
assert_eq "${dis//\"/}" "false" "Copy lights up once the buffer carries a selection"
click_row "Copy"
copied=$(jsq '(() => {const a = window.__copyArgs || []; return a.length ? a[a.length - 1] : "";})()' 2>&1 | tail -1)
# the buffer is copied as its rows separated by line breaks, so a wrapped probe line
# arrives split: the receipt is read with every space removed
copied_flat=$(printf '%s' "$copied" | tr -d '[:space:]')
case "$copied_flat" in
  *"$PROBE"*) step "ok: Copy carried the selected buffer text through the one copy path" ;;
  *) die "the Copy row wrote nothing from the buffer (got: ${copied:0:120})" ;;
esac

# Paste: the clipboard hands the shell a line of input, and Enter runs it.
# Clipboard READ is permission gated on this lane (recipe 115's finding), so the app's own
# call is what gets proven: the stub stands in for the grant, and it is READ BACK through
# the page before the row is clicked - a stub the browser refused dies here, naming itself,
# instead of surfacing as "the Paste row put nothing into the shell".
PASTE_LINE="echo $PROBE-paste"
jsq '(() => {
  const stub = { readText: () => Promise.resolve("'"$PASTE_LINE"'") };
  let ok = false;
  try { Object.defineProperty(navigator, "clipboard", { configurable: true, value: stub }); ok = true; } catch (e) {}
  if (!ok) {
    try { Object.defineProperty(navigator.clipboard, "readText", { configurable: true, value: stub.readText }); ok = true; } catch (e) {}
  }
  window.__clip = "pending";
  (async () => { try { window.__clip = await navigator.clipboard.readText(); } catch (e) { window.__clip = "ERR " + e.name; } })();
  return ok;
})()' >/dev/null 2>&1
if ! wait_js "window.__clip === '${PASTE_LINE}'" 10; then
  die "the clipboard stub never answered the read (got: $(jsq 'window.__clip' 2>&1 | tail -1)) - Paste cannot be proven on this lane"
fi
right_click_js "$right_click_free"
wait_js '(() => !!document.querySelector(".context-menu.measured"))()' 10
click_row "Paste"
if ! wait_js "(() => (document.querySelector('.dock-pane.edge-bottom .xterm-rows')||{innerText:''}).innerText.replace(/\s+/g,'').includes('$PROBE-paste'))()" 20; then
  die "the Paste row put nothing into the shell"
fi
ab press Enter >/dev/null 2>&1
wait_js "(() => (document.querySelector('.dock-pane.edge-bottom .xterm-rows')||{innerText:''}).innerText.replace(/\s+/g,'').split('$PROBE-paste').length > 2)()" 20
step "ok: Paste hands the clipboard line to xterm and the shell runs it"

# leave the buffer clean for the next recipe (and prove Clear once more, after a real run)
right_click_js "$right_click_free"
wait_js '(() => !!document.querySelector(".context-menu.measured"))()' 10
click_row "Clear"

assert_no_console_errors
step "done: the pane area, the settings tab and the terminal all right-click into the one shared menu"
