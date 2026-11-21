# ADR 0007: the dependency law - agent/ never imports harness/, api/ is the serving shell

Status: accepted

## Context

Capsules register into the shell; the boot seam is one-directional. Without a mechanical check, layering violations land as innocent-looking imports - each one cheap, each one costing a restructure to undo once entrenched.

## Decision

Two rules, enforced mechanically (`scripts/gate-deps.sh` plus a Go gate test): the `agent/` tree never imports `harness/`, and `api/` stays the serving shell that composes capsules rather than a home for business logic. Test files may ride harness fixtures through an explicit, documented exemption: the scan reads regular imports only, never test imports. The exemption is deliberate, not a hole - fixtures are test-only material.

## Why

The gate has stopped real violations, including one that looked like a routine import during a refactor. Removing or relaxing it requires an architecture decision that re-draws the seam - never an import-by-import relaxation.
