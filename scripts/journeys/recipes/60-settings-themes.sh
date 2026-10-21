#!/bin/bash
# 60-settings-themes - the settings surface + the theme hot-pick:
# the dock-tab Settings entry opens the knob panel, the theme picker
# applies live (documentElement data-theme flips without a reload),
# and the local theme-mode knob survives a reload.
. "${JROOT:?run must export JROOT}/lib.sh"

open_app

# settings opens as the pinned inspector dock-tab (not a menubar gear)
ab find text "Settings" click >/dev/null 2>&1 || die "no Settings dock-tab rendered"
ab wait --text "Theme" >/dev/null 2>&1 || true
panel=$(jsq 'document.querySelector(".theme-select") ? 1 : 0' 2>&1)
assert_eq "$panel" "1" "settings panel renders the theme picker"

# hot-pick: flip base theme and assert the live document flips instantly
before=$(jsq '(document.documentElement.dataset.theme || "unknown").slice(-6)' 2>&1)
case "$before" in *dark*) want="light";; *light*) want="dark";; *) want="dark" ;; esac
ab select ".theme-select" "$want" >/dev/null 2>&1 || jsq '(() => { const s = document.querySelector(".theme-select"); const opt = Array.from(s.options).find(o => o.value.includes("'$want'")); s.value = opt ? opt.value : s.options[0].value; s.dispatchEvent(new Event("change", {bubbles: true})); return s.value; })()' >/dev/null 2>&1 || die "theme select does not take $want"
sleep 0.5
after=$(jsq '(document.documentElement.dataset.theme || "unknown").slice(-6)' 2>&1)
case "$after" in *"$want"*) step "ok: theme hot-pick applied live ($before -> $after)" ;; *) die "theme hot-pick did not apply live (got $after, want $want)" ;; esac

# the pick is local state: it survives a reload
open_app
persisted=$(jsq '(document.documentElement.dataset.theme || "unknown").slice(-6)' 2>&1)
case "$persisted" in *"$want"*) step "ok: theme survives reload" ;; *) die "theme did not survive reload (got $persisted)" ;; esac

assert_no_console_errors