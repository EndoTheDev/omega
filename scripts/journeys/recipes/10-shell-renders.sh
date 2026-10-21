#!/bin/bash
# 10-shell-renders - the poisoned-bundle detector.
#
# Open the served SPA and assert the customer-visible shell: title, the
# dock panes, the composer, the sessions sidebar. A bundle that built
# from a conflicted tree, a runtime setup ReferenceError, or a broken
# mount shows up as a missing element or console noise here.
. "${JROOT:?run must export JROOT}/lib.sh"

open_app
ab wait --load networkidle >/dev/null 2>&1 || true

# the SPA shell: title carries the app name
title=$(ab get title)
case "$title" in *omega*) ;; *) die "SPA title wrong: $title" ;; esac

# the three customer surfaces render: sessions sidebar, composer, dock tabs
# (all eval assertions return bare numbers/booleans - agent-browser
# quotes string results, so string-matching gets escaped into noise)
sb=$(jsq 'document.querySelector(".sessions") ? 1 : 0' 2>&1)
cp=$(jsq 'document.querySelector(".composer textarea") ? 1 : 0' 2>&1)
tabs=$(jsq 'document.querySelectorAll(".dock-tab, .view-tab").length' 2>&1)
panes=$(jsq 'document.querySelectorAll("[data-pane-id]").length' 2>&1)
assert_eq "$sb" "1" "sessions sidebar renders"
assert_eq "$cp" "1" "composer renders"
[ "$tabs" -ge 1 ] 2>/dev/null || die "no dock tabs render ($tabs)"
[ "$panes" -ge 1 ] 2>/dev/null || die "no dock panes render ($panes)"
step "ok: shell renders (sidebar, composer, $tabs tabs, $panes panes)"

# a real lane conversation is reachable (list loads into the sidebar)
rows=$(jsq 'document.querySelectorAll(".session-list li").length')
[ "$rows" -ge 1 ] 2>/dev/null || die "session list empty/unrendered: $rows"
step "ok: session list renders ($rows rows)"

# the shell came up with zero console errors (the poisoned-bundle tripwire)
assert_no_console_errors