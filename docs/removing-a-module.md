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
  `engine/http_channel/module_routes.go` from the live Context. An absent
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
   grep -rn '"kanban"' modules/kanban             # the tool registry (the bare domain tool, the one-domain law)
   grep -rn '"/board"' cmd modules                # the /board, /boards commands
   grep -rn 'kanban' docs                         # route tables, command docs, layer table
   ```

   Expect hits in: the seam claim (`RegisterKanban` in the module's
   mount), the route table (`modules/kanban/rest.go` registering the
   patterns through `api.RegisterRoute`), the tool registry
   (the bare domain `kanban` tool - the legacy `kanban.task`
   namespaced name retired by the one-domain tool law, t_e06b38bc),
   the command chain
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
   the binary, and asserts two things on `go tool nm` output: zero
   `modules/<name>` paths, and zero module-noun words in OMEGA-OWNED
   symbols - paths under `github.com/EndoTheDev/omega/`, main-package
   symbols, and type descriptors naming a main-scope type. Third-party
   symbols are out of scope: a library's own names (robfig/cron,
   modernc.org/memory, the crypto stdlib) ride in the binary through
   other capsules and are not the module's vocabulary. A capsule that
   cannot pass it has a wire the removal recipe
   does not model. Capsules with a known dependency (recorded with the
   compile evidence) live in `scripts/nm-purity.modules`; removing a
   capsule from that manifest is a claim the rotation proves or
   refuses on the next run.
7. **Take the host config struct with it** (`cmd/omega/config.go`
   plus its yaml key, env-var entries and settings rows). A module's
   config struct is module-shaped vocabulary wherever it sits
   (AGENTS.md: module-shaped names appear in modules/ only), and its
   type-equality descriptor is exactly how the noun used to survive a
   clean removal. The scrubber models the class: a `type <Noun>Config`
   host struct and the aggregate Config field naming it (or the
   module-alias type, `Continuous continuous.Config`) die
   mechanically, and access sites via the field (`cfg.Memory.File`,
   `case "memory":`) cascade with them. A planted fixture proved it:
   `main.CronConfig` + a `Cron CronConfig` field scrubbed byte-clean
   off config.go (card t_6209ed01). Host rules keep the cascade
   mechanical: helpers read module-config fields as arguments, never
   whole Config (webBaseURL - a helper reading whole Config strands
   its last return when the access sites die), and a host type named
   after the module noun is always module vocabulary.

   More shapes the v0.13/v0.14 wiring taught (the nightly paid four
   runs for them):

   - The capsule's own type never crosses a host function boundary.
     A module-typed return (`newAgent` handing back the plugin
     instance) makes the scrub kill the whole function and strands
     every caller of a positional multi-value return. Capability
     threads as a host shape: a `func([]string)` hook, never
     `*continuous.Plugin`. The registry stores the hook, callers
     check a nil, and the removal leaves a dead nil-hook field no
     noun survives in.
   - A module-namespace operand in a boolean expression early-returns
     instead (`DisabledNamespaces.empty`). A line-granular scrub never
     rewrites an expression; it deletes whole statements.
   - Locals the module's removal strands get non-reserved names
     (`hasSkills`, `listErr` in the skills validator). The scrub's
     anchors never deaden `err`/`ok` - that sentinel is what keeps
     unrelated guards alive - so a name that must die with its
     declaration cannot be one of them.
   - The scrubber itself now models two strands: a return whose values
     are all dead identifiers or go literals (at least one dead) dies,
     and a short declaration nobody reads anymore blanks its dead
     names in place (unread plain declarations die; a short form with
     no new name degrades to a plain assignment). Fixtures pin both in
     `scripts/nm-purity_test.sh`.
8. **Run the gates in the worktree**: `go build ./...`, `go vet ./...`,
   `go test ./...`. The journey suite proves the board behavior green
   before removal (`91-canvas-tab` and the module journeys) and green
   after - the canvas host tab renders whatever is mounted, so it
   passes both ways.
9. **Data is not code.** A removed module's databases
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