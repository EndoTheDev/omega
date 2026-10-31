#!/bin/bash
# 60-settings-themes - the settings surface + the theme hot-pick:
# the dock-tab Settings entry opens the knob panel, the theme picker
# applies live (documentElement data-theme flips without a reload),
# and the local theme-mode knob survives a reload.
. "${JROOT:?run must export JROOT}/lib.sh"

open_app

# settings opens as the pinned inspector dock-tab (not a menubar gear).
# card t_502b8e81: the right edge renders ICON-ONLY chips - click the
# chip by its aria-label (there is no "Settings" text node to find).
open=$(jsq '(()=>{const t=[...document.querySelectorAll(".dock-pane.pinned .dock-tab")].find(b=>((b.getAttribute("aria-label")||"")||b.title).trim()==="Settings");if(!t) return 0;t.click();return 1})()' 2>&1)
assert_eq "$open" "1" "Settings dock-tab opens by its chip name"

# the theme knob: card t_acad02ec replaced the native select with the
# BandSelect listbox (.setting-select, aria-label = the knob label, the
# current value in .chip-name). assert the chip renders, not a select.
panel=$(jsq '(()=>{const c=[...document.querySelectorAll(".setting-select .model-chip")].find(b=>b.getAttribute("aria-label")==="theme");return c?1:0})()' 2>&1)
assert_eq "$panel" "1" "settings panel renders the theme picker"

# hot-pick: flip base theme and assert the live document flips instantly.
# drive the BandSelect the way 79/81 do: click the chip, pick the
# option from the rendered .model-list (auto/light/dark are literally
# the knob's option labels - SettingsTab.vue themeOptions).
before=$(jsq '(document.documentElement.dataset.theme || "unknown").slice(-6)' 2>&1)
case "$before" in *dark*) want="light";; *light*) want="dark";; *) want="dark" ;; esac
cur=$(jsq '(()=>{const c=[...document.querySelectorAll(".setting-select .model-chip")].find(b=>b.getAttribute("aria-label")==="theme");return c?c.querySelector(".chip-name")?.textContent?.trim()||"":"none"})()' 2>&1)
step "ok: theme knob chip carries the current value [$cur] (want $want -> picking)"
hdr=$(jsq '(()=>{const c=[...document.querySelectorAll(".setting-select .model-chip")].find(b=>b.getAttribute("aria-label")==="theme"); if(!c) return 0; c.click(); return 1})()' 2>&1)
assert_eq "$hdr" "1" "theme chip clicked (pop opens)"
if ! wait_js '(()=>{return [...document.querySelectorAll(".setting-select .model-list li")].some(l=>(l.textContent||"").trim()==="'"$want"'")})()' 10; then
  die "theme options never rendered"
fi
pick=$(jsq '(()=>{const o=[...document.querySelectorAll(".setting-select .model-list li")].find(l=>(l.textContent||"").trim()==="'"$want"'"); if(!o) return 0; o.click(); return 1})()' 2>&1)
assert_eq "$pick" "1" "theme option pick to $want"
ok=0; i=0; after=""
while [ "$i" -lt 20 ]; do
  after=$(jsq '(document.documentElement.dataset.theme || "unknown").slice(-6)' 2>&1)
  case "$after" in *"$want"*) ok=1; break;; esac
  sleep 0.5; i=$((i + 1))
done
[ "$ok" = "1" ] && step "ok: theme hot-pick applied live ($before -> $after)" \
  || die "theme hot-pick did not apply live (got $after, want $want)"

# the pick is local state: it survives a reload
open_app
persisted=$(jsq '(document.documentElement.dataset.theme || "unknown").slice(-6)' 2>&1)
case "$persisted" in *"$want"*) step "ok: theme survives reload" ;; *) die "theme did not survive reload (got $persisted)" ;; esac

assert_no_console_errors