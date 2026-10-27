# Troubleshooting

## `config: provider.model_name is required`

This error means omega loaded your `config.yaml` but the
`provider.model_name` field is empty. The most common cause is
**YAML indentation**.

### Wrong (keys at root level)

```yaml
provider:
type: ollama
model_name: llama3
```

YAML treats `type` and `model_name` as root-level keys, not nested
under `provider`. omega sees an empty provider.

### Right (keys indented 2 spaces)

```yaml
provider:
  type: ollama
  model_name: llama3
```

### Other causes

- `config.yaml` not found — omega looks for `<home>/config.yaml` first,
  then `<home>/config/config.yaml`, or the path passed via `--config`.
  Check that the file exists in one of those locations.
- `model_name` left empty in the example file — the example has
  `model_name:` with no value. You must fill it in.
- **Empty value clobbers the default.** YAML parsers set a key with
  no value to the zero value (empty string). `model_name:` (nothing
  after the colon) produces `""`, which fails validation. Same applies
  to inline comments: `model_name: # my model` also yields `""` because
  the parser treats everything after `#` as a comment. Always put the
  value before the comment: `model_name: llama3 # my model`.

## Module not found

Modules are compiled into the `omega` binary. If a tool or command
is missing, rebuild omega:

```bash
./build.sh
```

## SQLITE_BUSY errors

If you see `SQLITE_BUSY` when running subagents, make sure you are
running omega v0.1.2 or later — WAL mode + busy_timeout was added to
handle concurrent access from parent and subagent processes.

## Where did my database go? (data/ layout, v0.8+)

The databases live in `<home>/data/` (`omega.db`, `kanban.db`, plus
their SQLite `-wal`/`-shm` sidecars). On the first boot of the version
that introduced the layout, a legacy home-root database is MOVED (renamed,
never copied) into `data/` automatically — nothing to do by hand, and the
old path never resurrects. The three yaml files moved the same way into
`<home>/config/` earlier; resolution checks the home root first, so legacy
installs kept working unchanged.

## A chat reply came back empty (`finish_reason: "load"`)

An empty-but-successful turn usually means the run sent no `role` on its
message(s) — a content-only payload decodes to zero messages, and Ollama
ends the empty turn with `done_reason: "load"` which surfaces as
`finish_reason: "load"` on the stream. Send `{role: "user", content: ...}`
entries via `/chat` (see [api.md](api.md)); for one-shot checks prefer
`omega run "..."`.

## Conflict markers show up in the served web UI

The SPA bundle has been built from conflicted/stash-poisoned source.
Regenerate, don't hand-fix: `cd webui && bash scripts/build.sh`,
then rebuild `go build ./cmd/omega` and verify the served CSS/JS came
out clean (grep served files, never the `.vue` sources). See
"poisoned-bundle diagnosis" in [webui.md](webui.md).
