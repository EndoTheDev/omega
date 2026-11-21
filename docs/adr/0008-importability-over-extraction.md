# ADR 0008: importability over extraction - the shell resolves capsules, never reaches into them

Status: accepted

## Context

A serving shell that grows business logic becomes undeletable: the deletability bar (see ADR 0001 - removing a capsule's folder removes the feature from source and binary) dies the moment core starts importing capsule internals. The two folders `api/` and `agent/api/` look redundant to a fresh reader; treating them as duplication and "cleaning one up" would destroy the design.

## Decision

The shell resolves capsules through registered interfaces at composition time; a wiring test proves the seam. The agent's own HTTP surface (`agent/api/`) imports the serving shell (`api/`) zero times. The direction is the whole design: capsules are importable, never extracted-from.

## Why

Importability is the load-bearing property behind the capsule model - without it, "delete the folder, the feature is gone" is a slogan instead of a fact. Changing this requires revisiting ADR 0001 first.
