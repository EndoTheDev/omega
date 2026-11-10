# Modules

Modules are in-process Go packages that provide tools, commands,
and capability seams to omega. Each module implements the
`agent.Plugin` interface and is compiled into the `omega` binary.
There are no separate processes, no JSON-RPC, and no stdio.

For the full list of modules and how to write new ones, see
[harness/README.md](../harness/README.md).

For the module system architecture (Context, Plugin, MountAll),
see [architecture.md](architecture.md).

## The smallest capsule grammar

`observability` is the reference capsule for a surface that needs
neither a seam nor a tool: no `Offer`, no `Provides`, no config section,
no command - a session-attributed spine subscription plus two read-only
routes (`GET /observability/events`, `GET /observability/logs`; the wire
is documented in [api.md](api.md), the capsule row in
[harness/README.md](../harness/README.md)). Its whole state is the
bounded ring it owns, and disabling it removes the recording and both
routes together.
