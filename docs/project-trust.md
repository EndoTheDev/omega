# Project Trust

omega loads `AGENTS.md` files from the working directory up to the
filesystem root and injects them into the system prompt. Because those
files can contain instructions, omega gates them behind a trust check.

The trust unit is the nearest directory (walking up from cwd) containing
an `AGENTS.md`. Trust decisions are stored in `<home>/trust.yaml`:

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
