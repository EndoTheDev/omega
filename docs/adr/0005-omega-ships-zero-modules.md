# ADR 0005: omega ships zero modules baked in

Status: accepted

## Context

Feature policy (trust rules, approval policy, provider choices) could live inside core or ship as modules the user configures. Baking policy in couples the product to one opinion and makes every choice a code change.

## Decision

Core owns mechanism; modules own policy, and omega ships with no module's behavior baked in. Modules register on typed seams - core consults them mid-decision and falls back to built-in behavior when the module is absent or disabled. The product is a single repo; users disable modules they do not want. Core code contains no feature-module vocabulary (see ADR 0001's deletability bar).

## Alternatives considered

Core-baked policy: rejected - every opinion becomes a release; users cannot disagree without forking.

Per-user config instead of modules: rejected - the seam consult is the extension point; config files cannot add behavior, only tune existing knobs.
