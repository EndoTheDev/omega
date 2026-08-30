# gateway

## Purpose

Runtime config: YAML loading, environment overrides, defaults,
hot-reload. Sub-config structs for all extensions. No implementation
lives here — the HTTP server moved to `extensions/http_channel/` and
the SQLite store moved to `extensions/store/`.

## Ownership

- `config.go` - `Config` and sub-configs (ProviderConfig, ServerConfig,
  StoreConfig, SkillsConfig, MemoryConfig, LoggingConfig, CompactionConfig),
  `LoadConfig` (YAML + env + defaults), `DefaultConfig`, `applyEnv`,
  `Validate`, `WatchConfig` (fsnotify hot-reload)
- `config_test.go` - config loading and env override tests

## Local Contracts

- **Config layering is YAML, then env, then defaults.** `LoadConfig`
  starts from `DefaultConfig`, overlays YAML, applies `OMEGA_*` env
  overrides, then validates. `provider.model_name` and `server.port` are
  required after layering.
- **No re-exports.** Types from `agent` and `ai` are
  imported directly, not re-exported from this package.

## Work Guidance

- New sub-config structs go here. Extensions type-assert `ctx.Config`
  to `gateway.Config` to read their section.
- `WatchConfig` uses fsnotify — keep the debounce logic intact.
- Prefer stdlib. The only non-stdlib dependency is `gopkg.in/yaml.v3`.

## Verification

```bash
go test ./gateway/            # config tests
go build ./...                # everything compiles
go vet ./...                  # no suspicious constructs
```

## Child DOX Index

No sub-packages.