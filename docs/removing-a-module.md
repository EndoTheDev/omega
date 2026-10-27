# Removing a module (the capsule recipe)

A module is a capsule: everything a card can see it through ships from
one registration point - the plugin's mount. The seam (`ctx.Kanban`),
the tools, the slash commands, the canvas panels and their actions are
all registered from that one function. Delete the module's
registration and every face vanishes by construction - nothing outside
the module holds a reference. Kanban is the worked example below (arc
5, the flagship: the biggest module - six tools folded into one, two
commands, two panels, the REST surface).

This doc is the whole-module recipe. The pre-canvas TUI/webui recipe
remains in `docs/tui.md` ("Module removal recipe (both ends)") and
still applies at its layer. The mount-style migration recipe (v0.12
phase C, moving a capsule onto the universal `Register*` surface) is
its sibling: `docs/migrating-a-module.md`.

## What the law guarantees

- An unregistered plugin ships no store, no seam, no tools, no
  commands, no panels. `TestModuleOmissionDropsTUICommands`
  (`cmd/omega/plugins_test.go`) and the per-module removal proofs in
  `cmd/omega/removal_test.go` gate this.
- Module REST routes are registered by the capsule itself
  (`api.RegisterRoute`, the phase-D vocabulary) and served by
  `http_channel/module_routes.go` from the live Context. An absent
  module registered no pattern, so its REST answers 404, not a
  seam-nil guard - no fallback surface survives the removal.
- Core stays a pure witness. Since the v0.12 phase-F core-purity
  move, core carries ZERO module nouns (the machine check is
  `scripts/nm-purity.sh`): the module-shaped types live in the capsule
  (`modules/kanban`), so removing a module takes its types with it.
  Core retains only the shared vocabulary (`core/seams.go`,
  `core/routes.go`) that is a layer contract, not a module shape.

## The recipe

1. **Check dependents first.** `Requires()` edges are declared data,
   not prose: `grep -rn '"kanban"' modules/*/plugin.go` - a hit inside
   another module's `Requires()` means removing kanban takes that
   emitter with it (the notifications removal proved this: kanban,
   cron and delegate all name `notifications`). Remove or re-route
   dependents in the same change.
2. **Take the plugin out of `buildPlugins()`** in `cmd/omega/main.go`.
   One line. That is the whole unregistration - there is no second
   switch, no manifest entry, no route table to prune.
3. **Grep the registration points** (they are all one-hop from the
   module name):

   ```bash
   grep -rn '"kanban"' cmd modules core docs --include='*.go'
   grep -rn 'Module: "kanban"' modules            # module-qualified action ids
   grep -rn '"kanban.task"\|kanban\.' modules/kanban  # tool names (namespaced, Q10)
   grep -rn '"/board"' cmd modules                # the /board, /boards commands
   grep -rn 'kanban' docs                         # route tables, command docs, layer table
   ```

   Expect hits in: the seam claim (`RegisterKanban` in the module's
   mount), the route table (`modules/kanban/rest.go` registering the
   patterns through `api.RegisterRoute`), the tool registry
   (the namespaced `kanban.task` tool - the legacy `task`/`task.*`
   names retired with the v0.12 mount migration), the command chain
   (`/board`, `/boards` registered through `api.RegisterCommand`), the
   canvas panels (`modules/kanban/panel.go`,
   namespaced action ids), and the docs. Then delete the module
   directory (`modules/kanban/`) - tests die with it.
4. **Write the removal proof against the mounts, not the directory.**
   The pattern is `TestKanbanRemovalProof` (`cmd/omega/removal_test.go`
   - `TestNotificationsRemovalProof` started it): build the full
   plugin set once, mount, assert every face is present and working;
   mount again without the module, assert every face is gone. The
   proof runs against the mounted context, so it survives later
   refactors of the module directory layout.
5. **The unmount proof must show all of it:**

   ```
   PROOF mount=full: seam mounted, canvas carries the task panel + the board switcher, module-qualified
   PROOF tool wiring: task create pushed the task panel through the live canvas
   PROOF action-pipe: complete_task closed t_d259b564, switch_board answered the switcher
   PROOF removed: seam absent (the capsule's route patterns left the ledger, the module mux is empty), canvas carries no kanban panel, no task tools, no /board commands
   ```

   That is: seam absent (routes 404), canvas panels absent, tool
   registry without the domain tool AND its alias shims, commands
   unregistered, the other modules mounted - the TUI and webui stay
   alive, the canvas snapshot simply misses the module.

6. **Run the removal through the nm machine.** The final proof is
   mechanical, not assertive: `scripts/nm-purity.sh --module kanban`
   scrubs every wire to the module out of a copy of the tree, builds
   the binary, and asserts zero module-noun symbols in `go tool nm`
   output. A capsule that cannot pass it has a wire the removal recipe
   does not model. Capsules with a known dependency (recorded with the
   compile evidence) live in `scripts/nm-purity.modules`; removing a
   capsule from that manifest is a claim the rotation proves or
   refuses on the next run.
7. **Run the gates in the worktree**: `go build ./...`, `go vet ./...`,
   `go test ./...`. The journey suite proves the board behavior green
   before removal (`91-canvas-tab` and the module journeys) and green
   after - the canvas host tab renders whatever is mounted, so it
   passes both ways.
8. **Data is not code.** A removed module's databases
   (`<home>/data/kanban.db`, `<home>/data/boards/`) stay on disk.
   Say so in the removal note; never ship a migration that deletes a
   user's boards.

## What breaks the capsule (do not do these)

- A fallback surface outside the module - a renderer that guesses the
  panel shape, a route that reimplements the store's grammar. Both
  frontends render the canvas generically; that is the whole point.
- Deleting the core seam types while any seam signature references
  them (`core.KanbanProvider` is `ctx.Kanban`'s type and
  `modules/kanban` implements it).
- A `Requires()` edge left pointing at a removed module - the mount
  fails with the missing seam at startup, which is the correct
  behavior: fix the edge, never widen a guard.