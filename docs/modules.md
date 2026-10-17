# Modules

Modules are in-process Go packages that provide tools, commands,
and capability seams to omega. Each module implements the
`agent.Plugin` interface and is compiled into the `omega` binary.
There are no separate processes, no JSON-RPC, and no stdio.

For the full list of modules and how to write new ones, see
[modules/README.md](../modules/README.md).

For the module system architecture (Context, Plugin, MountAll),
see [agent/AGENTS.md](../agent/AGENTS.md).
