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

- `config.yaml` not found — omega looks for `<home>/config.yaml` or
  the path passed via `--config`. Check that the file exists.
- `model_name` left empty in the example file — the example has
  `model_name:` with no value. You must fill it in.
- **Empty value clobbers the default.** YAML parsers set a key with
  no value to the zero value (empty string). `model_name:` (nothing
  after the colon) produces `""`, which fails validation. Same applies
  to inline comments: `model_name: # my model` also yields `""` because
  the parser treats everything after `#` as a comment. Always put the
  value before the comment: `model_name: llama3 # my model`.

## Extension not found

Extensions are compiled into the `omega` binary. If a tool or command
is missing, rebuild omega:

```bash
./build.sh
```

## SQLITE_BUSY errors

If you see `SQLITE_BUSY` when running subagents, make sure you are
running omega v0.1.2 or later — WAL mode + busy_timeout was added to
handle concurrent access from parent and subagent processes.
