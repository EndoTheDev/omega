# ADR 0009: the landing pipeline's standing gates

Status: accepted

## Context

An automated landing pipeline can race itself: pipeline passes overlapping, merges landing against a dev tip that moved underneath them, and landings whose gates were green on a tree that no longer exists. Each of these produced real incidents before the gates existed.

## Decision

The pipeline holds standing gates. A pass claims an exclusive lock; an overlapping pass exits silently instead of racing. A branch merges only after its gates run green on that exact tree - never on a stale tip. A branch is rebased when its review is claimed, so the reviewed tree is the tree that merges. Done work merges before new work dispatches.

## Why

Each rule was bought by an incident class: lock races, double landings, and merges that carried content no reviewer ever saw. Removing one requires the incident class it names to be proven extinct - not frustration with its cost.
