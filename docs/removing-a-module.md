# Removing a module (the capsule recipe)

A module is a capsule: everything a card can see it through ships from
one registration point - the plugin's `Mount`. The seam (`ctx.Kanban`),
the tools, the slash commands, the canvas panels and their actions are
all appended in that one function. Delete the module's registration and
every face vanishes by construction - nothing outside the module holds
a reference. Kanban is the worked example below (arc 5, the flagship:
the biggest module - six tools folded into one, two commands, two
panels, the REST surface).

This doc is the whole-module recipe. The pre-canvas TUI/webui recipe
remains in `docs/tui.md` ("Module removal recipe (both ends)") and
still applies at its layer.

## What the law guarantees

- An unregistered plugin ships no store, no seam, no tools, no
  commands, no panels. `TestModuleOmissionDropsTUICommands`
  (`cmd/omega/plugins_test.go`) and the per-module removal proofs in
  `cmd/omega/removal_test.go` gate this.
- Per-module REST routes answer 501 through their seam-nil guards -
  they never outlive the module (the `http_channel` guards:
  `boardUnavailable` for kanban, the notifications 501, and so on).
- Core types stay. `core/kanban.go`, `core/seams.go` and friends are
  the seam vocabulary the layers share; the layer law says layers
  never import modules, so the types reference no module code. A
  removal that deleted them would break the seam signature's home.
  This is intentional residue, not leftover.

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
   grep -rn '"task"\|task\.' modules/*/plugin.go  # tool names + alias shims
   grep -rn '"/board"' cmd modules                # the /board, /boards commands
   grep -rn 'kanban' docs                         # route tables, command docs, layer table
   ```

   Expect hits in: the seam mount (`ctx.Kanban` in the module's
   `Mount`), the REST guards (`http_channel/tasks.go`), the
   tool registry (the canonical `task` tool plus any `task.*` alias
   shims), the command chain (`/board`, `/boards` registered through
   `ctx.Commands`), the canvas panels (`modules/kanban/panel.go`,
   module-qualified actions), and the docs. Then delete the module
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
   PROOF removed: seam nil (REST routes fall through to their 501 guard), canvas carries no kanban panel, no task tools, no /board commands; core/kanban.go types remain - documented as intentional
   ```

   That is: seam nil (routes 501), canvas panels absent, tool
   registry without the domain tool AND its alias shims, commands
   unregistered, the other modules mounted - the TUI and webui stay
   alive, the canvas snapshot simply misses the module. The core
   types compile in the proof on purpose (see the guarantee above).
6. **Run the gates in the worktree**: `go build ./...`, `go vet ./...`,
   `go test ./...`. The journey suite proves the board behavior green
   before removal (`91-canvas-tab` and the module journeys) and green
   after - the canvas host tab renders whatever is mounted, so it
   passes both ways.
7. **Data is not code.** A removed module's databases
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