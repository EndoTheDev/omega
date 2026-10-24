#!/bin/bash
# 77-mobile-drawer - the mobile drawer repairs journey (t_e76ce7b6).
# Two repros, both user-visible, both ride the journeys suite as
# regression guards:
#   REPRO 1  desktop-close -> mobile -> drawer opens WITH content
#            (the drawer is decoupled from the dock tree: a desktop-closed
#            sidebar prunes the edge pane, but the drawer renders its own
#            content regardless - no bare scrim)
#   REPRO 2  the drawer shows its tab bar (sessions on the left; the seven
#            inspector tabs on the right) and tabs switch the content
#
# No model needed: the drawer is pure shell UI. Runs against the dev lane
# (JOURNEY_BASE_URL, default :8098).
# Card t_aca53fee (16ac10a): the mobile drawer-btn handles are deleted -
# the menubar edge toggles are the drawer open path below 768px; the
# repros pin the drawer behavior through that path now.
. "${JROOT:?run must export JROOT}/lib.sh"

open_app
ab set viewport 1200 800 >/dev/null 2>&1

# clean baseline: the default dock tree (sidebar + main + inspector)
js 'localStorage.clear()' >/dev/null 2>&1
ab reload >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true

panes=$(jsq 'document.querySelectorAll("[data-pane-id]").length')
[ "$panes" -ge 3 ] 2>/dev/null || die "default dock tree did not render ($panes panes)"
step "ok: dock tree renders ($panes panes)"

# capture the REGISTRY-grounded strip BEFORE the strip's pane is closed
# (t_7adf2e7f: a hardcoded expectation went stale with the first drift -
# the assert below now pins the drawer to the strip the desktop sidebar
# renders, and that strip IS INSPECTOR_TAB_COMPONENTS ground truth: the
# loader restores the full set on every fresh boot, and app.vue derives
# the drawer's fallback (drawerTabsForEdge) from the same registry list).
# A future registry tab joins both surfaces and this assert stays green;
# a drawer-derivation regression reds on the mismatch.
desktop_strip=$(jsq '(()=>{const panes=[...document.querySelectorAll(".dock-pane.pinned")]
 .filter(p=>!p.classList.contains("edge-bottom")&&!p.querySelector(".session-list"));
 const pane=panes[0]; if(!pane) return "";
 return [...pane.querySelectorAll(".dock-tab-title")]
  .map(e=>(e.textContent||"").trim()).join(",")})()')
desktop_strip=${desktop_strip#\"}; desktop_strip=${desktop_strip%\"}
[ -n "$desktop_strip" ] || die "could not capture the desktop inspector strip"
step "ok: inspector strip captured: $desktop_strip"

# --- REPRO 1 setup: desktop-close BOTH sidebars (prune the pinned panes) ---
js 'Array.from(document.querySelectorAll("button.icon-btn.edge-toggle")).find(b=>b.getAttribute("aria-label")==="Toggle left sidebar").click()' >/dev/null 2>&1
sleep 0.3
js 'Array.from(document.querySelectorAll("button.icon-btn.edge-toggle")).find(b=>b.getAttribute("aria-label")==="Toggle right sidebar").click()' >/dev/null 2>&1
sleep 0.3
panes=$(jsq 'document.querySelectorAll("[data-pane-id]").length')
# card t_67fc1905: the fresh dock tree is column [main row, bottom shell]
# - 4 panes total. The desktop-close prunes BOTH SIDEBARS, not the
# bottom shell (it has no menubar sidebar among the toggles this journey
# clicks): 2 panes survive (the main pane + the bottom panel).
assert_eq "$panes" "2" "desktop-close prunes both sidebars (main + bottom left)"

# --- narrow to mobile (the breakpoint crossing flips isMobile via the
#     matchMedia change listener; NO reload - a reload would re-assert the
#     fixed shape via ensurePinnedEdges and undo the desktop-close) ---
ab set viewport 420 840 >/dev/null 2>&1
sleep 1.2

# the drawer handles are GONE by design (16ac10a, card t_aca53fee):
# one set of menubar edge toggles serves every width - below 768px they
# drive the drawer flips (app.vue routes the same emits on isMobile).
# Pin both halves of the contract: no drawer-btn, toggles visible.
handles=$(jsq 'document.querySelectorAll("button.icon-btn.drawer-btn").length')
assert_eq "$handles" "0" "16ac10a: the mobile drawer-btn handles are gone (one toggle set, every breakpoint)"
visible=$(jsq '(() => {
  const labels = ["Toggle left sidebar", "Toggle bottom panel", "Toggle right sidebar"];
  return labels.every((l) => {
    const b = Array.from(document.querySelectorAll("button.icon-btn.edge-toggle"))
      .find((b) => b.getAttribute("aria-label") === l);
    return b && getComputedStyle(b).display !== "none";
  }) ? 1 : 0;
})()')
assert_eq "$visible" "1" "mobile: the menubar edge toggles render under 768px (the drawer open path)"

# open the sessions drawer via the menubar toggle; REPRO 1 asserts it
# opens WITH content (not a bare scrim) even though the edge pane was
# pruned at desktop
js 'Array.from(document.querySelectorAll("button.icon-btn.edge-toggle")).find(b=>b.getAttribute("aria-label")==="Toggle left sidebar").click()' >/dev/null 2>&1
sleep 0.7
scrim=$(jsq 'document.querySelector(".scrim")?1:0')
open=$(jsq 'document.querySelector(".mobile-drawer.left.open")?1:0')
content=$(jsq 'document.querySelector(".mobile-drawer.left .session-list")?1:0')
assert_eq "$scrim" "1" "REPRO 1: drawer scrim renders"
assert_eq "$open" "1" "REPRO 1: sessions drawer opens (left.open)"
assert_eq "$content" "1" "REPRO 1: drawer opens WITH content after desktop-close (not blank)"
step "ok: REPRO 1 - desktop-close -> mobile -> drawer opens with content"

# --- REPRO 2: the inspector drawer shows its tab bar and tabs switch ---
# the right drawer opens via the menubar inspector toggle (16ac10a)
js 'Array.from(document.querySelectorAll("button.icon-btn.edge-toggle")).find(b=>b.getAttribute("aria-label")==="Toggle right sidebar").click()' >/dev/null 2>&1
sleep 0.7
open=$(jsq 'document.querySelector(".mobile-drawer.right.open")?1:0')
assert_eq "$open" "1" "REPRO 2: inspector drawer opens (right.open)"
tabs=$(jsq 'Array.from(document.querySelectorAll(".mobile-drawer.right .mobile-drawer-tab")).map(t=>t.textContent.trim()).join(",")')
tabs=${tabs#\"}; tabs=${tabs%\"}  # jsq returns JSON-encoded strings; strip the quotes
# the strip is pinned to the DESKTOP inspector strip captured above (the
# registry's right-area panels - agents (t_7e2b7002) and extensions
# (t_14f73742) joined it via the registry, and so will the next one):
# the drawer must EQUAL that strip, never a recipe-copied list
assert_eq "$tabs" "$desktop_strip" "REPRO 2: inspector drawer shows the registry tab strip (== desktop strip)"

# the active tab is Files (the file tree renders, not a module panel)
active=$(jsq 'document.querySelector(".mobile-drawer.right .mobile-drawer-tab.active")?.textContent.trim()')
active=${active#\"}; active=${active%\"}
assert_eq "$active" "Files" "REPRO 2: Files is the default active inspector tab"
files=$(jsq 'document.querySelector(".mobile-drawer.right .inspector-files")?1:0')
assert_eq "$files" "1" "REPRO 2: Files tab renders the file tree"

# switch to Skills -> the content swaps to the skills module panel
js 'Array.from(document.querySelectorAll(".mobile-drawer.right .mobile-drawer-tab")).find(t=>t.textContent.trim()==="Skills").click()' >/dev/null 2>&1
sleep 0.6
active=$(jsq 'document.querySelector(".mobile-drawer.right .mobile-drawer-tab.active")?.textContent.trim()')
active=${active#\"}; active=${active%\"}
assert_eq "$active" "Skills" "REPRO 2: clicking Skills activates it"
skills=$(jsq 'document.querySelector(".mobile-drawer.right .module-panels")?1:0')
assert_eq "$skills" "1" "REPRO 2: Skills tab renders the module panel (content switched)"

# switch to Settings -> the settings panel renders (not a module panel)
js 'Array.from(document.querySelectorAll(".mobile-drawer.right .mobile-drawer-tab")).find(t=>t.textContent.trim()==="Settings").click()' >/dev/null 2>&1
sleep 0.5
settings=$(jsq 'document.querySelector(".mobile-drawer.right .pane.settings")?1:0')
assert_eq "$settings" "1" "REPRO 2: Settings tab renders the settings panel"
step "ok: REPRO 2 - drawer tab bar switches content (Files -> Skills -> Settings)"

# --- desktop regression: the toggles still hide/show sidebars at desktop ---
js 'document.querySelector(".scrim")?.click()' >/dev/null 2>&1
sleep 0.3
ab set viewport 1200 800 >/dev/null 2>&1
sleep 1.0
js 'Array.from(document.querySelectorAll("button.icon-btn.edge-toggle")).find(b=>b.getAttribute("aria-label")==="Toggle left sidebar").click()' >/dev/null 2>&1
sleep 0.4
after_close=$(jsq 'document.querySelector(".dock-pane .sessions")?1:0')
assert_eq "$after_close" "1" "desktop: reopen left sidebar (boot re-asserted edges on the breakpoint cross; toggle closes)"
js 'Array.from(document.querySelectorAll("button.icon-btn.edge-toggle")).find(b=>b.getAttribute("aria-label")==="Toggle left sidebar").click()' >/dev/null 2>&1
sleep 0.4
# closing again prunes; reopening brings it back
js 'Array.from(document.querySelectorAll("button.icon-btn.edge-toggle")).find(b=>b.getAttribute("aria-label")==="Toggle left sidebar").click()' >/dev/null 2>&1
sleep 0.4
final=$(jsq 'document.querySelector(".dock-pane .sessions")?1:0')
assert_eq "$final" "1" "desktop: toggle hide/show round-trips the sidebar"
step "ok: desktop toggle hide/show unchanged"

assert_no_console_errors