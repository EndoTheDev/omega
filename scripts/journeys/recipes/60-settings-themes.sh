#!/bin/bash
# 60-settings-themes - the settings surface + the split theme knobs:
# the dock-tab Settings entry opens the knob panel, the COLOR MODE
# select (auto/dark/light) flips the built-in palette live, the THEME
# select lists "default" plus the themes-folder files (defaults in,
# then a seeded custom file round-trips and persists). Local knobs
# survive a reload.
. "${JROOT:?run must export JROOT}/lib.sh"

# the custom theme file is seeded into the serving lane's themes
# folder (the registry-toggles pattern): OMEGA_HOME wins, else derived
# from OMEGA_BIN - no machine-specific path. Both unset dies loudly.
if [ -z "${OMEGA_HOME:-}" ]; then
  [ -n "${OMEGA_BIN:-}" ] || die "set OMEGA_HOME or OMEGA_BIN to the serving lane home"
  OMEGA_HOME="$(dirname "$OMEGA_BIN")"
fi
THEME_FILE="$OMEGA_HOME/themes/jrn-theme-$(date +%d%b%H%M%S | tr 'A-Z' 'a-z').yaml"
THEME_LABEL="Journey Palm"
ACCENT="#0099aa"

cleanup() {
  rm -f "$THEME_FILE" 2>/dev/null || true
}
trap cleanup EXIT

open_app

# settings opens as the pinned inspector dock-tab (not a menubar gear).
# card t_502b8e81: the right edge renders ICON-ONLY chips - find the
# chip by its aria-label (there is no "Settings" text node to find).
# card t_31aa2542: nothing after open_app is synchronous on a cold
# runner - poll rather than the old immediate zero-wait query.
if ! poll_js '(()=>{const t=[...document.querySelectorAll(".dock-pane.pinned .dock-tab")].find(b=>((b.getAttribute("aria-label")||"")||b.title).trim()==="Settings");if(!t) return false;t.click();return true})()' 10; then
  die "Settings dock-tab chip never rendered"
fi
step "ok: Settings dock-tab opens by its chip name"

# the SPLIT knobs (task t_a94b1cda): two selects in the chat group,
# one per chip aria-label: color mode (auto/dark/light) and theme
# (default + themes-folder files). The old merged select retired.
if ! poll_js '(()=>{const c=[...document.querySelectorAll(".setting-select .model-chip")];
  return c.some(b=>b.getAttribute("aria-label")==="color mode") && c.some(b=>b.getAttribute("aria-label")==="theme")})()' 10; then
  die "settings panel never rendered the color mode + theme knobs"
fi
step "ok: both knobs render (color mode + theme, split)"

# the theme select lists the built-in default
hdr=$(jsq '(()=>{const c=[...document.querySelectorAll(".setting-select .model-chip")].find(b=>b.getAttribute("aria-label")==="theme"); if(!c) return 0; c.click(); return 1})()' 2>&1)
assert_eq "$hdr" "1" "theme chip clicked (pop opens)"
if ! wait_js '(()=>{return [...document.querySelectorAll(".setting-select .model-list li")].some(l=>(l.textContent||"").trim()==="default")})()' 10; then
  die "theme options never rendered (no default entry)"
fi
tlist=$(jsq '(()=>{return [...document.querySelectorAll(".setting-select .model-list li")].map(l=>(l.textContent||"").trim()).join(",")})()' 2>&1)
step "ok: theme select lists [$tlist]"
case ",$tlist," in
  *,default,*) step "ok: theme select lists default" ;;
  *) die "theme select missing the default entry" ;;
esac

# pick default (a no-op value pick is fine - the built-in stays): the
# colors stay - data-theme remains dark/light and NO inline token ride
# along (the default theme is the built-in palettes only)
pick=$(jsq '(()=>{const o=[...document.querySelectorAll(".setting-select .model-list li")].find(l=>(l.textContent||"").trim()==="default"); if(!o) return 0; o.click(); return 1})()' 2>&1)
assert_eq "$pick" "1" "theme option pick to default"
after=$(jsq '(document.documentElement.dataset.theme || "unknown")' 2>&1)
case "$after" in dark|light) step "ok: default pick keeps the built-in palette ($after)" ;; *) die "defaultpick lost the palette (got $after)" ;; esac
inline=$(jsq 'document.documentElement.style.getPropertyValue("--accent").trim()' 2>&1)
assert_eq "$inline" "" "default theme carries no token overrides"

# close the pop, then the color-mode hot-pick: flip the built-in
# palette and assert the live document flips instantly (auto follows
# the OS, dark/light pin one - semantics unchanged)
jsq 'document.body.click()' >/dev/null 2>&1 || true
before=$(jsq '(document.documentElement.dataset.theme || "unknown").slice(-6)' 2>&1)
case "$before" in *dark*) want="light";; *light*) want="dark";; *) want="dark" ;; esac
hdr=$(jsq '(()=>{const c=[...document.querySelectorAll(".setting-select .model-chip")].find(b=>b.getAttribute("aria-label")==="color mode"); if(!c) return 0; c.click(); return 1})()' 2>&1)
assert_eq "$hdr" "1" "color mode chip clicked (pop opens)"
if ! wait_js '(()=>{return [...document.querySelectorAll(".setting-select .model-list li")].some(l=>(l.textContent||"").trim()=="'"$want"'")})()' 10; then
  die "color mode options never rendered"
fi
pick=$(jsq '(()=>{const o=[...document.querySelectorAll(".setting-select .model-list li")].find(l=>(l.textContent||"").trim()=="'"$want"'"); if(!o) return 0; o.click(); return 1})()' 2>&1)
assert_eq "$pick" "1" "color mode pick to $want"
ok=0; i=0; after=""
while [ "$i" -lt 20 ]; do
  after=$(jsq '(document.documentElement.dataset.theme || "unknown").slice(-6)' 2>&1)
  case "$after" in *"$want"*) ok=1; break;; esac
  sleep 0.5; i=$((i + 1))
done
[ "$ok" = "1" ] && step "ok: color mode hot-pick applied live ($before -> $after)" \
  || die "color mode hot-pick did not apply live (got $after, want $want)"
mode_before=$(jsq 'localStorage.getItem("omega.theme-mode") || "auto"' 2>&1)

# --- seed a custom theme file into the lane home, pick it, verify
# --- its base + tokens land, persist through a reload, then undo.
mkdir -p "$OMEGA_HOME/themes" || die "cannot mkdir $OMEGA_HOME/themes (OMEGA_HOME not writable?)"
printf -- '# journey seed - theme file contract\nname: %s\nbase: light\ntokens:\n  accent: "%s"\n  text-dim: "#5c6a79"\n' \
  "$THEME_LABEL" "$ACCENT" > "$THEME_FILE" || die "theme seed write failed"
step "seeded: $THEME_FILE"

# the watcher refreshes WITHOUT a restart: poll /themes until the id
tid="$THEME_FILE"; tid="${tid##*/}"; tid="${tid%.yaml}"
i=0
while [ "$i" -lt 20 ]; do
  curl -s "$JOURNEY_BASE_URL/themes" | grep -q "$tid" && break
  sleep 0.5; i=$((i + 1))
done
curl -s "$JOURNEY_BASE_URL/themes" | grep -q "$tid" || die "watcher never listed the seeded theme ($tid)"
step "ok: /themes lists the seeded file ($tid) without a restart"

# reopen settings (the open-time refetch re-reads the list) and pick
# the seeded file's label
open_app
if ! poll_js '(()=>{const t=[...document.querySelectorAll(".dock-pane.pinned .dock-tab")].find(b=>((b.getAttribute("aria-label")||"")||b.title).trim()==="Settings");if(!t) return false;t.click();return true})()' 10; then
  die "Settings dock-tab never re-rendered"
fi
if ! poll_js '(()=>{const c=[...document.querySelectorAll(".setting-select .model-chip")].find(b=>b.getAttribute("aria-label")==="theme"); if(!c) return false; c.click(); return true})()' 10; then
  die "theme chip never rendered on the reload pass"
fi
if ! wait_js '(()=>{return [...document.querySelectorAll(".setting-select .model-list li")].some(l=>(l.textContent||"").trim()=="'"$THEME_LABEL"'")})()' 10; then
  die "the seeded theme name never made the theme option list"
fi
pick=$(jsq '(()=>{const o=[...document.querySelectorAll(".setting-select .model-list li")].find(l=>(l.textContent||"").trim()=="'"$THEME_LABEL"'"); if(!o) return 0; o.click(); return 1})()' 2>&1)
assert_eq "$pick" "1" "custom theme pick to $THEME_LABEL"

# its base + tokens apply: file base light wins over the color mode,
# the inline --accent override rides the file
ok=0; i=0; after=""
while [ "$i" -lt 20 ]; do
  after=$(jsq '(document.documentElement.dataset.theme || "unknown")' 2>&1)
  [ "$after" = "light" ] && { ok=1; break; }
  sleep 0.5; i=$((i + 1))
done
[ "$ok" = "1" ] || die "custom theme base never applied (got $after, want light)"
inline=$(jsq 'document.documentElement.style.getPropertyValue("--accent").trim()' 2>&1)
assert_eq "$inline" "$ACCENT" "custom theme token override applied"
mode_after=$(jsq 'localStorage.getItem("omega.theme-mode") || "auto"' 2>&1)
assert_eq "$mode_after" "$mode_before" "theme pick never touched the color mode"

# the pick is local state: it survives a reload
open_app
persisted=$(jsq 'localStorage.getItem("omega.theme-id") || ""' 2>&1)
assert_eq "$persisted" "$tid" "custom theme pick persists (theme-id key)"
persisted2=$(jsq '(document.documentElement.dataset.theme || "unknown")' 2>&1)
case "$persisted2" in light) step "ok: custom theme re-applies after reload" ;; *) die "custom theme did not re-apply after reload (got $persisted2)" ;; esac

# restore: back to the built-in default so the cleanup can delete the
# file without leaving a dead id persisted
jsq '(()=>{const c=[...document.querySelectorAll(".setting-select .model-chip")].find(b=>b.getAttribute("aria-label")==="theme"); if(c) c.click(); return !!c})()' >/dev/null 2>&1 || true
if ! wait_js '(()=>{return !![...document.querySelectorAll(".setting-select .model-list li")].find(l=>(l.textContent||"").trim()==="default")})()' 10 2>/dev/null; then
  step "note: theme pop did not reopen for the restore pick (state reverts via the fallback anyway)"
else
  jsq '(()=>{const o=[...document.querySelectorAll(".setting-select .model-list li")].find(l=>(l.textContent||"").trim()==="default"); if(o) o.click(); return !!o})()' >/dev/null 2>&1 || true
fi

assert_no_console_errors