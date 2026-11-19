# Security policy

## Reporting a vulnerability

Email **endothedev+omega@gmail.com** with the subject `omega security`.
Do not open a public issue for security reports - email keeps the
window closed until a fix ships.

Include what you can: the affected surface (file or endpoint), a
minimal repro, the version line from `omega --version`, and what an
attacker would gain.

## What you can expect

omega is a solo project. Reports are read within a few days. A
confirmed fix ships in the next nightly and in the next stable
release; critical fixes get a stable release as soon as the fix is
proven. You get a reply either way, and credit in the release notes
if you want it.

## Scope

In scope: the omega code in this repository - the HTTP API, the
agent loop and tools, the harness capsules, the embedded webui, the
build and release scripts.

Design decisions that are documented, not bugs:

- The shell tool executes LLM-generated commands **with no
  sandboxing**. That is stated in the README and is the operator's
  trust decision. What IS in scope: concrete chains where omega
  executes something the operator would not have approved - remote
  content that reaches shell execution, a fetch surface that reads
  private networks, path escapes in file tools.
- Self-hosted instances misconfigured by their operator (open
  bind, weak tokens) are operator error, unless omega's defaults
  caused the exposure.

## The bar for fetch and trust surfaces

Server-side URL fetches refuse private-address targets
(loopback, link-local including cloud metadata, RFC1918, CGNAT) -
the `internalHostGuard` seam in `api/server_images.go`. Any new
server-side fetch or trust surface must meet the same bar and carry
a test that proves it. If you find a fetch surface without the
guard, that is a report we want.

## Supported versions

The latest stable release and the current nightlies receive fixes.
Older stable lines do not.
