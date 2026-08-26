# Extensions

Extensions are in-process Go packages that provide tools, commands,
and capability seams to omega. Each extension implements the
`agent.Plugin` interface and is compiled into the `omega` binary.
There are no separate processes, no JSON-RPC, and no stdio.

For the full list of extensions and how to write new ones, see
[extensions/README.md](../extensions/README.md).

For the extension system architecture (Context, Plugin, MountAll),
see [agent/AGENTS.md](../agent/AGENTS.md).
