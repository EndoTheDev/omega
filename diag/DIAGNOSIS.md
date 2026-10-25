# t_7ab2bc47 — journey 30 switch-back red: diagnosis state (2026-10-25, run 529)

## What is PROVEN (live, three fresh reds on the push-logged diag lane :18431)

Reds caught: run-25Oct165539, run-25Oct165721 (stuck 2/2 family,
diag/evidence/), run-25Oct174816 (redloop catch, session KEPT — was live
probed post-mortem). All three reds share the identical signature.

1. THE CLICK IS LIVE, NOT DETACHED (suspect 1 RULLED OUT).
   Capture-phase click log (diag/observer.js):
   `vArrow: true, connected: true, disabled: false` at click time; POST
   /sessions/.../variants fires +5ms later. No remount race eats the click.

2. THE SERVER PERSISTS AND SERVES THE SELECT EVERYWHERE (suspect 3 RULLED
   OUT). Checkpoint C (200ms after click) and die-time curls:
   variants API `active: 0`, transcript items `[user, message 197]`,
   canvas chat payload rows `[196 user, 197 assistant]`. Server truth is
   correct on every surface for 10s+ afterwards.

3. THE CLIENT RECEIVES FRESH DATA THREE TIMES AND NEVER REPAINTS.
   Response bodies captured by the in-page fetch observer:
   - POST /variants response body itself: `{"variants":[...,"active":0]}`
   - transcript refetch (chat.ts:716 `load()`), +40ms: `active: 0`
   - canvas snapshot refetch (`canvasRefresh()`), +39ms: rows `[196, 197]`
   All fresh. The switcher DOM stays 2/2 from birth to die (+10s, 100ms
   sampler — zero 1/2 samples, zero canvas repaints).

4. THE APPLY CHAIN WORKS WHEN QUIET — THE RED WINDOW IS THE SETTLE WINDOW.
   Backflip probe (diag/backflip.sh) on a quiet settled pane: curl-POST the
   select server-side, the app's 15s canvas poller picks it up and the
   switcher DOM repaints 1/2 -> 2/2 end-to-end. Same machinery, minutes
   later, paints instantly. The red only happens within ~1-12s of the
   retry run's settle (exit-flush window).

5. POST-MORTEM PROBE OF THE STILL-LIVE RED PANE (run-25Oct174816, minutes
   after die): DOM bubble id = 392 (variant 2) still rendered, elapsed
   "3s"; server = [391] active 0. A NEW click on the v-arrow flips to 1/2
   in <400ms with the identical select+refetch flow. So the pane is not
   stuck server-bound: the render graph re-attaches later.

6. SERVER PUSH TIMELINE (OMEGA_DIAG_PUSH=1, lane proc output — the push
   log does NOT go to omega.log, it is log.Printf on stderr):
   run 174816: 17:48:22 first exchange [user/390 -> assistant/391];
   17:48:23 retry begin; 17:48:25 exit-flush `[user/390/complete,
   assistant/392/complete]` running=false; **17:48:26 a further compose
   push `[user/390/complete, assistant/391/complete]` — the POST-SELECT
   active path, pushed as a delta** — a push AFTER the run ended (the
   update subscriber was still registered). This is the select re-push
   (be0e851) — and it carried the FRESH path too. So suspect 2's
   "re-push paints 2/2 back over 1/2" is WRONG for this red: no stale
   payload ever arrives after the fresh ones. The client store receives
   [391] three more times (17:48:26 delta, +7.46 refetch, +13.01 poller
   snapshot) and the DOM never follows in the window.

## CONCLUSION (honest boundary)

Root cause is THE CLIENT RENDER GRAPH, not the server, not the click, not
refetch ordering. In the post-exit-flush window the pane's canvas-render
stops consuming the canvas store: store-backed panel deltas and 15s
poller snapshots that demonstrably apply (fresh bodies land) never reach
the ChatTab -> ChatMessage repaint, and the same chain re-attaches with a
later interaction. The severance is inside ChatTab.vue:212-223
(panelPayload/panelMatches/canvasItems) and/or the per-pane
createCanvasTranscript cache (chat.ts:822-983, signature
`role:index:blockcount` positional cache). Prod bundle internals (Nuxt,
minified) cannot be inspected; `app._instance` is null and element
back-refs are dev-only, so the exact severed link is NOT YET pinned.

## What the fix card must do next (NOT a recipe change)

1. Rerun the same redloop against a DEV-build lane (vite dev / unminified
   bundle) — with prod dev-only handles live: dump `ChatTab`'s setupState
   (state.variants, state.restored, panelMatches, canvasItems) at die.
   Scripts exist: diag/redloop.sh (pauses 15min on red), diag/vmlist3.sh
   (instance walker needs the SUSPENSE/pendingBranch fix), diag/vmdump.sh.
2. Top seam candidate: `canvasItems` computed (ChatTab.vue:220) not
   re-invalidating on store panel replacement (canvas.ts:116 splice vs
   applyDelta at canvas.ts:123 replace-in-place, different reactive
   shapes), and the positional cache signature (chat.ts:837) which folds
   variant-2 row 392 and variant-1 row 391 onto ONE cached item object —
   id mutation in place means the switcher label rides item.id — and the
   250ms delta cadence during the LIVE run window is exactly when this
   cache is churning.
3. Secondary bug found on the way (own card candidate, 170043 family):
   multi-row replays (thinking/tool/text) render NO switcher at all — the
   branch anchor row is a thinking row (kind "thinking", ChatTab's
   variantOfItem guard line 418 requires kind "message") and the final
   text row's id (304) is not in siblings [301, 302] — so a tool-calling
   retry bricks the switcher; journey 30's recipe then gets `got []`.
   Also: server compose duplicated assistant rows ([391, 391] pushes,
   checkpoint rows) — see lane proc log lines 17:47:48 and 17:48:12.

## Harness (committed on wt/t_7ab2bc47)

- 8fa4edc instrumented journey-30 harness (diag30.sh)
- d29b1ca OMEGA_DIAG_PUSH push logger (strip before any fix)
- 24bbee8 interactive probes (probe30/clickprobe/backflip/vmdump)
- redloop.sh: unique per-iteration run tag + 15min pause on red
  (NOTE: needs AB exported - now done in redloop.sh itself)

## Red-run receipts (suite lines)

- 17:48:39 RED `sibling response stays reachable (switch back renders « 1/2 ») failed: got [2/2] want [1/2]`
  evidence: diag/evidence/run-25Oct174816/ (red.txt, client-obs.txt with
  response bodies + 100ms samples, dom.txt, srv-{variants,transcript,
  canvas}.json, lane-log-tail.txt, checkpoints A/B/C)
- 16:55:39 run-25Oct165539 and 16:57:21 run-25Oct165721: same signature
  and same discriminating facts (fresh bodies, no repaint, die snapshot).
- Loop totals: ~110 harness runs on the private lane, 3 switch-back reds;
  greens confirm the flow (suite lines "ok: sibling response stays
  reachable" / "ok: console silent").