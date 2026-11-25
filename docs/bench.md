# The Linux bench

One command proves the linux binary built from this checkout boots on a fresh home, serves
the webui, runs a conversation and keeps a setting across a restart, inside a throwaway
container. On the Pi that is a native arm64 build; on an x86 host, amd64 from the same tree.
It exists to catch container-only and fresh-home-only failures: a missing
SPA bundle, a Host guard rejecting a published port, a settings write that never
reaches `config.yaml`.

It is not a CI gate and not a substitute for the Go suite. The provider is the
deterministic mock, so no receipt ever depends on a live model.

## Prerequisites

- Docker with the Linux engine: Docker Desktop on Windows or macOS, or Docker Engine on a Linux host.
- Host port 18099 free. It is pinned because the image bakes `server.public_hosts`.
- Run from the repository root: the compose build context is the checkout.

## Run it

    bash scripts/bench-run.sh

On the Pi, from the dev machine:

    ssh pibtw 'cd /home/endo/dev/omega && git pull --ff-only origin dev && BENCH_OUT=/home/endo/dev/bench bash scripts/bench-run.sh'

That checkout is the host's own dev clone, owned by endo - not a bench-owned copy. The
pull is --ff-only and names its remote: it fast-forwards the tree and leaves its
branches and stash alone. Naming `origin dev` matters because that clone's `dev` carries
no upstream, so a bare `git pull` stops with "no tracking information for the current
branch".

Receipts land in `${BENCH_OUT:-D:/dev/bench}/<run-id>/`; override the root with
`BENCH_OUT`. Always set BENCH_OUT on a Linux host: the built-in default is a Windows
path, and an unset root writes a stray D: directory into the checkout. The run id is
also the compose project suffix, so a stuck run is easy to reap:
`docker compose -p omega-bench-<run-id> down -v`.

## Where it runs

The canonical host is the Pi's Docker engine (pibtw, aarch64). Containers isolate from
the engine that runs them: a bench run creates only its own compose project - no volume,
no custom network - and removes them at the end, so the Pi's own stack is untouched. The
Pi's containers (its website, its keypool proxy) are separate projects on the same
engine, and production omega runs as a systemd user unit outside it. The published port
binds 127.0.0.1 on the host, so a run opens nothing to the tailnet or the internet.

Read a Pi run's receipts back over ssh:

    scp -r pibtw:/home/endo/dev/bench/<run-id> ~/bench-receipts/

A run is on demand only. Nothing schedules it, and no run starts while a deploy is in
flight: the Pi serves production, and 18099 is also the deploy's copy-boot port, so a
run and a deploy cannot hold that port at once.

Division of labor: the Windows machine proves Windows things (the desktop, the sandbox,
dev work); the Pi proves Linux things (this bench, and native arm64 from the same tree).

## Receipts

| file | what it proves |
| --- | --- |
| `00-meta.txt` | the tree, base image, tool versions and built image digest the run tested |
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
- CLI and pty flows, TUI scripts, update staging and published multi-arch images are out of scope.
