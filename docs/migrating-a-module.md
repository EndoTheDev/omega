# Migrating a module to the universal mount (the capsule recipe)

The v0.12 universal mount (design settled 2026-10-26, phase A) gives
every capsule one registration surface: `agent.APIPlugin`'s
`MountWithAPI(ctx, api)`. Instead of assigning `Context` slots by hand,
the capsule calls `api.Register*(...)` - exclusivity moves from "which
slot" to one-name-one-tenant, and the registry ledger (claims, module
routes, refusals) records it. Phase B shipped the adapters; this phase
migrates the capsules one card at a time.

Kanban (t_a62ffa95, phase C1) is the proven migration: the biggest
capsule - an exclusive seam, a folded tool, two commands, a live
canvas capsule, and the notifications capture. Every remaining
capsule follows this pattern in its own card. The removal-side recipe
stays in `docs/removing-a-module.md`; the two recipes compose.

## The law (what the API enforces)

- Machine-facing names - tool names, panel action ids - are the
  `module.verb` namespace pair (`kanban.task`), prefix enforced at
  registration. User-facing names - slash commands, route patterns -
  keep their natural shape and are owner-recorded.
- One name, one tenant. A cross-tenant claim fails the boot (loud).
  Same-tenant re-registration replaces (the clean-idempotent remount).
- Registrations land on the exact `Context` fields the classic mount
  assigned, so the migrated capsule is behaviorally identical.
- A mount reads nothing it did not register. A dependency consumed at
  mount time (a seam capture) either stays a `Requires()` edge, or
  moves to a registration that removes the need for the edge.

## The recipe, in order

1. **Implement `agent.APIPlugin`** on the plugin. Keep the classic
   `Mount` as a fail-loud stub - it is never dispatched (APIPlugin
   wins, the strangler rule), but its body is dead and any accidental
   arrival there must scream. When the last phase-F card deletes the
   classic surface, the stubs go with it.

2. **Map every classic assignment to its registration.** The census
   (`agent/extensionapi.go` header) is the table; the short list:

   | Classic assignment                    | Registration                |
   | ------------------------------------- | --------------------------- |
   | Exclusive seam (one per slot)         | the slot's `Register*`      |
   | `ctx.ToolProviders` append            | `RegisterTool` per tool     |
   | `ctx.Commands` + handler wrapper      | `RegisterCommand` per name  |
   | `ctx.PanelProviders` append (static)  | `RegisterPanel` + `RegisterAction` |
   | `ctx.PanelProviders` append (live)    | `RegisterPanelProvider`     |
   | notifications capture (`ctx.Notifications`, nil-guarded) | `RegisterEmitter` |

   `RegisterCommand` builds the chain-on-miss wrapper for you: each
   handler answers only its own command name and returns an error for
   anything else - the wrapper falls through to the previous handler
   exactly like the hand-built `prev := ctx.CommandHandler` capture.
   One registration per command keeps the table order stable.

3. **Rename the machine-facing identities.** Existing tool names gain
   the module prefix (`task` became `kanban.task`); panel action ids
   become namespace pairs (`complete_task` became
   `kanban.complete_task`). This is not a rename of convenience - the
   registration API refuses anything else. Alias shims that cannot
   carry the namespace (the dotted `task.*` set) die instead: no
   backwards compatibility is the standing law, and the retirement is
   part of the migration's proof. Command names (`/board`) do not move.

4. **Drop the capture `Requires()` edge when the capture moves.** A
   mount-time seam capture (`ctx.Notifications` read inside Mount)
   needs the `Requires()` edge to guarantee ordering. The emitter
   registration receives the mounted seam AFTER every plugin mounts -
   nil-safe, module absent means nil - so the edge is then a lie and
   dies. Dependencies still read at mount time keep their edge.

5. **Register routes only if the engine serves nothing for you today.**
   `RegisterRoute` records module REST for phase-D consumption; a
   capsule served through a Context seam (kanban's `/tasks` rides
   `ctx.Kanban` through `http_channel`) registers nothing in phase C.

6. **Home the CLI subcommand (v0.12 phase D).** `RegisterSubcommand`
   registers the capsule's argv subcommand: a thin name+handler prefix.
   The binary routes `omega <name> ...` to the handler with the
   untouched argv after the name - the module parses its own flags
   (core never grows a flag framework) and prints its own help for
   `omega <name> --help`. The global `omega --help` lists module
   subcommands by registry introspection, so deleting the module makes
   the subcommand vanish from the help and from dispatch; a
   non-matching argument falls back to the project-path behavior. The
   module-CLI mount runs on config defaults when the home's config.yaml
   is missing or invalid: a module subcommand keeps the pre-migration
   CLI contract of never requiring a valid global config.
   Kanban (`modules/kanban/cli.go`) is the reference.

## How the proof works

The migration's proof is that the old way is gone and every face
survived:

- **The module's own tests** mount through the API - the helpers build
  a `Context` and call `agent.MountAllAPI([]agent.Plugin{p}, ctx, nil)`
  (all dispatch flows through the same mount loop, so a one-plugin
  mount is the real mount, not a shortcut). A shape test pins the
  result: seam identity, the registry's tool names, the command table
  and its fall-through, the panel provider's ids, and the classic
  stub failing loud.
- **A registration can't drift silently.** Every faces test already
  written against the registry (tool lookups, command chains, canvas
  snapshots) keeps passing - names and ids changed exactly once, in
  step 3, visible as those tests' updates.
- **The removal proof stays green.** `TestKanbanRemovalProof`
  (`cmd/omega/removal_test.go`) mounts the full set, drives every face
  (tool mutation pushes a panel delta, the action pipe completes a
  task through the renamespaced id), then mounts without the module
  and asserts nothing survived. It is untouched by the mount-style
  change except for the renamed identities.
- **The parity law still holds at the framework level.** The generic
  dual-mount parity test (`agent/extensionapi_test.go`) keeps proving
  that a registration and the classic assignment it adapted produce
  identical Contexts - for the remaining (still classic) capsules and
  for any migrated capsule through its new registrations.
- **Journeys green** on a private lane in the worktree; the mount
  style change must not move one customer-level assertion.

## What NOT to do

- Do not keep a working classic `Mount` beside `MountWithAPI` "for
  safety" - APIPlugin wins at dispatch, the body is unreachable, and
  unreachable seam assignment is the drift the migration kills.
- Do not widen the API to express one capsule's shape. `RegisterPanelProvider`
  exists because live view-models are a real, shared need (stateful
  capsules push deltas); ad-hoc escape hatches are not.
- Do not register a route you do not serve, and do not leave the
  capsule's argv in cmd/omega: the CLI subcommand homes in the capsule
  (step 6, phase D) - a wiring file in cmd/omega/<module>.go is the
  seam assignment the migration deletes.
