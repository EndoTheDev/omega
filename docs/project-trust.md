# Project Trust

omega loads `AGENTS.md` files from the working directory up to the
filesystem root and injects them into the system prompt. Because those
files can contain instructions, omega gates them behind a trust check.

The trust unit is the nearest directory (walking up from cwd) containing
an `AGENTS.md`. In the omega repo itself, the root file is the single
worker map - one map, not a hierarchy: read it and follow its pointers
into `docs/`. Trust decisions are stored in `<home>/config/trust.yaml`:

A legacy `<home>/trust.yaml` is migrated to `config/` on boot when the
target does not exist; while a root file exists it stays authoritative
(root-first resolution), so existing installs keep working unchanged.

```yaml
trusted:
  - path: /home/user/Code
    level: parent # trust this directory and everything under it
  - path: /home/user/Code/specific-repo
    level: exact # trust this directory only
```

## Behavior

- **TUI** - an untrusted project prompts `Trust files in <dir>? [y/N]`.
- **`run`/`serve`** - an untrusted project skips context with a warning.
- **`--approve`** - trust the current project (records an `exact` entry).
- **`--no-approve`** - skip the current project's context. Wins over
  `--approve`.
