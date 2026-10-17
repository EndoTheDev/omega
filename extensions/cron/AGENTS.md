# cron

## Purpose

The cron extension schedules omega prompts on cron expressions. Jobs
live in `crons.yaml` in OMEGA_HOME; each fire is one full agent turn in
a session labeled `cron:<name>`. Mounted via the `cron` seam
(`agent.CronProvider`; started by the serve host with `CronDeps`,
stopped on shutdown).

## Ownership

- `cron.go` - config surface: `Job` (name, schedule, prompt, deliver,
  enabled), `Config`, `ParseConfig`/`LoadConfig` with line-aware
  rejections (invalid schedule/name/deliver, duplicates naming both
  lines, unknown fields). Schedule grammar is fixed at load:
  robfig/cron/v3 standard 5-field parser, no seconds, no descriptors,
  evaluated in server local time.
- `runner.go` - the scheduler (`Runner`, implements
  `agent.CronProvider`): per-fire agent turn in the `cron:<name>`
  session, catch-up for missed fires within the last 24h
  (`cronstate.json` records last covered scheduled time),
  single-instance-per-job guard, deliver targets (journal | memory |
  none), and an mtime-poll hot-reload of `crons.yaml` that logs the
  diff (added/enabled/disabled/removed) and reschedules without
  restart.
- `plugin.go` - Plugin adapter (provides the `cron` seam); Mount loads
  `crons.yaml` from OMEGA_HOME, a load failure is a mount error.
- `*_test.go` - schema, catch-up, guard, deliver, hot-reload tests.

## Local Contracts

- **Trust gate: omega never writes crons.yaml.** Only human edits
  change the job file. omega may propose jobs in conversation; the
  only automatic write to the home directory is the bootstrap seed of
  `crons.yaml.example` (cmd/omega/home.go) - never a live crons.yaml.
- A fire arriving while the same job is still running is skipped
  (logged, never queued). Different jobs run in parallel; user chat
  sessions are never blocked.
- State entries for removed jobs may linger in cronstate.json; unknown
  entries are ignored on catch-up.
- A failed run still persists its session; deliver targets apply only
  to a non-empty final assistant message.

## Verification

```bash
go test ./extensions/cron    # schema, catch-up, guard, deliver, reload
go test -race ./extensions/cron
```