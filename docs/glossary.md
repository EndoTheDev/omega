# omega glossary

The domain language, one home. Every term defined once; code, docs and UI text use these words with these meanings and never synonym-swap. A new term enters only through a design discussion, and a proposed name must pass the scenario test before it becomes a table, a field, or a file: write the real usage sentences first, and reject any name that collides with an existing entry.

## Terms

**agent** - a saved assistant configuration: a name with its model, provider and behavior settings. A chat session runs under exactly one agent. The default agent is the one used when no agent is named; it cannot be deleted.

**session** - a conversation tree, persisted, with a title, a context start, and a durable stats row. Auto-created on the first send; survives compaction. Not a process or a connection.

**session file** - one session's append-only JSONL store at `<home>/data/sessions/<id>.jsonl`: a header line, one line per message, and one line per metadata change, each naming its session. The files are the source of truth; `omega.db` is a derived index over them and is rebuilt from them when missing.

**index** - the derived SQLite database beside the session files (`data/omega.db`): folded session metadata, message rows, the FTS search tables, image captions and the pending-calls ledger. Disposable by construction - delete it and the next open rebuilds it, or run `/reindex`.

**turn** - one user message and everything it triggers. The unit of the transcript. A turn may end with tool calls still pending; their results arrive later and start a new turn (see ADR 0004).

**run** - one turn's execution through the agent loop; can span many provider calls. Owns usage accounting and the last-run stats. One run, many provider calls.

**wake** - an out-of-band message delivered into an existing session: a pending result arriving, or a user message while calls are pending. A wake starts the next turn.

**compact** - the context compaction boundary: the active path is compacted, a context start plus summary persist, the session survives. Compaction runs automatically at the token budget or on the /compact command. A compact that costs context is the point; it never costs the run. (The codebase's older internal name "fold" is deprecated; any reference to folding means compacting.)

**context start** - the anchor message id where a session's live context begins; set at compact time. Everything before it is summary plus retained tail.

**tool call** - one invocation of a tool by the agent. A tool call is either synchronous (blocks its turn) or background: it outlives its turn, and while it runs the turn preamble counts it ("2 tool calls running, 1 settled since last turn").

**verdict** - a human judgment record on a run (approve/reject plus note). Not a gate result: gates are deterministic; verdicts are taste.

**capsule** - a self-contained module under `harness/` owning its tools, commands, UI and storage; registers into the shell; the deletability bar: removing the folder removes the feature from source and binary. The words capsule and module are not interchangeable: module is the registry row, capsule is the code unit.

**panel** - a canvas panel descriptor, the payload unit the server pushes (view-model deltas). A panel is data, not a component.

**pane** - one dock cell of the canvas; the layout unit. **seat** - the position a pane occupies (the seat map: each view has a default home it returns to when closed).

**canvas** - the multi-pane layout surface: dock splits, zone gestures, the seat map.

**continuous agent** - an agent that persists across wakes in an existing session; the continuous capsule owns the loop, the cron capsule delivers the wake.

**ephemeral session** - a no-store session; replay turns (retry/fork) are not available in one.

**fork** - a branch taken from a message id; the original path keeps existing. A variant is one branch point's alternative; the path selects among them.

**board** - omega's built-in kanban. **board card** - one task on it; cards move through statuses; archive is the reversible end state.

**cron** - omega's scheduled runner. **cron job** - one scheduled entry: a prompt run on a schedule. Always the compound; the bare word "job" is not used for tool work.

**journey** - a scripted end-to-end check that drives the UI the way a user would. Journeys pin the implemented shape of a feature; they run in CI and gate releases.

**preview artifacts** - HTML omega renders for export (transcripts, reports, snapshots). Preview artifacts render with zero JavaScript; interactive behavior belongs to the live web UI, never to an artifact.
