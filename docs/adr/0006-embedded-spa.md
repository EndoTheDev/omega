# ADR 0006: the web UI ships embedded in the binary

Status: accepted

## Context

omega serves its web UI itself; there is no separate web server deployment and no node runtime in production.

## Decision

The web UI is a built single-page application whose static assets are compiled into the Go executable. The binary serves the UI on its own port with no external asset directory. Consequence that shapes the build: the binary contains whatever the build step found on disk, so the SPA build and the binary build must run in the same release pass - a binary built before an SPA change serves a stale UI while reporting a new version, and the release process rebuilds the bundle from current SPA source.

## Alternatives considered

Serving the SPA from a directory beside the binary: rejected - a missing or stale directory becomes a runtime failure mode the user cannot diagnose, and the artifact stops being a single file.

A template-rendered UI (server-side pages): rejected - the interactive product is a streaming chat surface; the SPA is the honest shape for it.
