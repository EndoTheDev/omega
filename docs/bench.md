# The Linux bench

One command proves the shipped linux/amd64 binary boots on a fresh home, serves the
webui, runs a conversation and keeps a setting across a restart, inside a throwaway
container. It exists to catch container-only and fresh-home-only failures: a missing
SPA bundle, a Host guard rejecting a published port, a settings write that never
reaches `config.yaml`.

It is not a CI gate and not a substitute for the Go suite. The provider is the
deterministic mock, so no receipt ever depends on a live model.

## Prerequisites

- Docker with the Linux engine (Docker Desktop on Windows or macOS).
- Host port 18099 free. It is pinned because the image bakes `server.public_hosts`.
- Run from the repository root: the compose build context is the checkout.

## Run it

    bash scripts/bench-run.sh

Receipts land in `${BENCH_OUT:-D:/dev/bench}/<run-id>/`; override the root with
`BENCH_OUT`. The run id is also the compose project suffix, so a stuck run is easy to
reap: `docker compose -p omega-bench-<run-id> down -v`.

## Receipts

| file | what it proves |
| --- | --- |
| `00-meta.txt` | the tree, base image digest and tool versions the run tested |
| `01-build.log` | the linux binary built from this checkout |
| `02-health.json` | `omega serve` came up on a fresh home and reported healthy |
| `03-webui.html` | the committed SPA bundle is served |
| `04-entry.txt` | the entry script the shell loads is fetchable |
| `05-chat.sse` | one mock round trip: session, tool call, answer, stop |
| `06-settings.json` | a settings write was accepted |
| `06-settings-readback.json` | the same knob reads back the new value |
| `07-config.yaml` | the value landed in the container's `config.yaml` |
| `08-settings-after-restart.json` | the value survived a process restart |
| `09-serve.log` | the server log of the run |
| `10-residue.txt` | no container, volume or network survived |
| `11-sessions.json` | the home started fresh: one session, the run's own |

`RESULT.txt` is the verdict: one PASS/FAIL line per assertion plus the receipt it rests
on. The script exits 0 only when every line passes.

## Limits

- Fixed host port 18099 and one run at a time; a second run waits for the port to free.
- CLI and pty flows, TUI scripts, update staging and arm64 images are out of scope.
