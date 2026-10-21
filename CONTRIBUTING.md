# Contributing to omega

omega is a Go port of the Pi/Tau event-stream agent architecture: one
binary, three layers with one job each, an in-process module system.
External contributions are welcome. This page is the complete inbound
process — what we take, how PRs are checked, and where the security
line sits.

## Before you open a PR

Read `docs/roadmap.md` and the open issues first. If nothing there
matches what you want to build, start an issue or ask on the
[Discord](https://discord.gg/vQvHrHWkbx) before writing anything.
Large or architectural changes especially: direction is agreed before
code, never during review.

For repo layout, architecture, and the coding contracts our own work
follows, read `AGENTS.md`, `DESIGN.md`, and the docs under `docs/`.

## The gate

Every PR passes the same admission rules, in plain words:

1. **Direction before correctness.** An out-of-scope PR is closed
   politely even if the code is perfect. Fit with the roadmap or an
   accepted issue is decided before anything else.
2. **Same gates as our own cards.** `go build ./...`, `go vet ./...`,
   `go test ./...`, `-race`, and — when `modules/ui` changes — an SPA
   regen through the embed lane (`modules/ui/scripts/build.sh`, then
   commit the synced `modules/http_channel/static/`). These gates run
   in **GitHub CI, never on any private machine**. That is explicit
   policy, not a convenience. (Today's CI job runs vet + test + the
   shell scratch-repo suite, and fails dev pushes on the drift gate;
   the remaining checks - full build + validation journeys - land with
   the nightly channel.)
3. **Review is read-only.** A maintainer will read your diff anywhere
   they like, but nothing from a PR is executed outside GitHub CI
   runners. We do not build or run external code locally, at all, for
   any reason. If review needs an exception, the answer is to extend
   CI, not to run things on maintainer machines.
4. **Security-touch surfaces get a named security review.** Changes
   touching `modules/trust/`, the attach/fetch endpoints, config
   handling, the shell tool, or any server-side URL fetch are
   reviewed and approved for security specifically, by name, before
   merge. The bar is the `internalHostGuard` pattern in
   `modules/http_channel/server_images.go` - detail in the security
   section below.
5. **No new dependencies without stated justification.** Anything
   added to `go.mod` or `package.json` needs a paragraph in the PR
   saying what it does and why the standard library doesn't cover
   it. Unjustified or unnecessary dependency bumps get rejected.
6. **Privacy.** Nothing personal: no personal files, no seeded user
   data, no embedded profiles of any real person or real
   installation. Examples and fixtures are synthetic, by construction.
7. **Secrets are an auto-reject.** A PR that adds or reads `.env`,
   credential patterns, or key files is rejected outright, no
   discussion. `.env` is gitignored for a reason; secrets never
   enter the tree, in any format, redacted or not.
8. **Tests for new behavior** — the same standard as our own cards.
   New code paths get tests; changed behavior updates the tests that
   describe it. A PR that skips this is incomplete even when it works.
9. **Authorship is stated.** The work is yours; AI-assisted work is
   fine as long as it's disclosed. Undisclosed AI-dump PRs get
   skeptical review, not rubber-stamps. We read diffs — an honest
   note in the description buys trust, omission buys scrutiny.

The checklist version of these rules is the PR template you'll be
asked to fill when opening the PR: `.github/PULL_REQUEST_TEMPLATE.md`.

## Why the boundary is this sharp

omega's shell tool executes LLM-generated commands with no sandboxing,
and the machines that build omega are machines with real credentials:
the maintainer's shell, GitHub tokens, signing keys, a home server
running omega itself. Code that looks fine can be a probe of exactly
those. The security boundary makes that a non-issue by construction:
review diffs are read, CI runners execute, and nothing in between is
trusted. Rule 3 isn't distrust of you — it's refusal to make the
honesty of review dependent on what any diff says about itself.

The concrete bar for rule 4 is the `internalHostGuard` pattern in
`modules/http_channel/server_images.go`: the server-side URL fetch
refuses IP literals inside the box (loopback, link-local incl. cloud
metadata, RFC1918, CGNAT) and the guard is a seam so tests can stub
it. Any new server-side fetch or trust decision must show the same
level of care: named guard, refuse-by-default toward the private
rings, tests that prove it.

## Release channel

Stable releases are tagged and built by GitHub Actions (5 platforms,
sha256 checksums). A nightly prerelease channel is planned: once it
ships you can grab a nightly to test against the latest without
building from source. Until then, the freshest code is the `dev`
branch via `./build.sh`.