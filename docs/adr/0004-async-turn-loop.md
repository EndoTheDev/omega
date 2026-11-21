# ADR 0004: the yield-shaped async loop

Status: accepted

## Context

An agent loop must handle tool calls that take minutes: shell commands, delegated work, scheduled results. The naive designs both fail: blocking the conversation on every tool call freezes the chat; parking a goroutine per waiting conversation scales cost with idle conversations.

## Decision

The turn, not the tool, is the unit of waiting. A turn ends when the agent makes a yield-class call (a slow tool call that runs in the background); the process then idles with no per-conversation cost. When a result arrives - or the user sends a message - a wake starts the next turn. The UI renders this honestly: the turn ends with a pending chip, and the continuation streams as a new turn in the same pane.

Every tool call gets exactly one real response; pending siblings render as pending chips in history.

## Alternatives considered

A blocking wait tool the agent calls and sleeps on: rejected - one parked goroutine per waiting conversation, and the agent must remember to poll.

Purely synchronous tools with a size cap: rejected - it forbids the long-running work that is the point of an agent.
