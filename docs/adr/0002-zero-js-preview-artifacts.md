# ADR 0002: preview artifacts render with zero JavaScript

Status: accepted

## Context

omega renders HTML artifacts for export and preview: transcripts, reports, snapshots. These artifacts are opened in constrained readers - embedded HTML viewers, sandboxed iframes, file previews - which vary wildly in script support, and they must remain readable for years without maintenance.

## Decision

An artifact omega renders is plain HTML and CSS. Zero JavaScript, no framework runtime, no build requirement. Interactivity belongs to the live web UI only; an artifact is a document.

## Alternatives considered

JavaScript-driven artifacts (chart libraries, SPA exports): rejected - they break in constrained readers, age badly with framework churn, and every exported file inherits the security review of executable content.
