# ADR 0001: features ship as capsules

Status: accepted

## Context

omega grew features as cross-cutting changes: backend routes, UI surfaces and core wiring landed as separate pieces of many features at once. That shape has two failure modes we hit directly: features could not be disabled or removed without archaeology (deleting one folder left vocabulary and behavior behind in core), and core accumulated knowledge of features it did not own.

## Decision

Every feature is a capsule: one capability - its backend module, routes, and UI surfaces - registered, shipped, and disabled as a single unit. Capsules register on omega's additive rails (routes, commands, tools, panels); core never imports a capsule. The bar we hold: removing a capsule's folder removes the feature from source and binary with zero residue in core.

## Alternatives considered

Baked-in features (feature code as ordinary core code): rejected - it fails the deletability bar and hides coupling.

A multi-repo split (core plus one repo per feature): rejected - one product, one repo; users install one binary, and a split doubles release and dependency management for no gain at this scale.
