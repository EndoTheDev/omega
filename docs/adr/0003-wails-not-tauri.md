# ADR 0003: the desktop shell is Wails, not Tauri

Status: accepted (the shell itself is planned; the choice is settled)

## Context

omega's next step after the web UI is a native desktop shell wrapping the existing UI. The candidates were Wails and Tauri.

## Decision

Wails. The deciding mechanism: omega is a Go binary. Wails embeds the OS webview inside a Go application - one toolchain, one build, the same language as the entire backend. Tauri wraps the webview in a Rust application shell, which would add a second compiled toolchain, a second dependency ecosystem, and a second security-audit surface to a project whose runtime is otherwise Go plus a web UI.

## Alternatives considered

Tauri: rejected on toolchain weight - see above.

Electron: rejected on binary size and memory; it ships its own Chromium where the OS already provides a webview.
