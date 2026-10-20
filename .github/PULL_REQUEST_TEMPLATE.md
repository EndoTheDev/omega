<!--
Thanks for contributing to omega. This checklist mirrors the 9-rule
gate in CONTRIBUTING.md. Keep the checks honest - review is read-only
and CI is the only place this code runs.
-->

## What

<!-- What this PR changes, in a sentence or three. -->

## Why (roadmap fit)

<!-- Link the issue or the roadmap item this serves. Out-of-scope PRs
     are closed even when the code is perfect - direction is decided
     before review. -->

## Checklist (the gate)

- [ ] Fits the roadmap or an open issue (direction before correctness)
- [ ] Gates pass in CI: build, vet, test, -race - plus an SPA re-sync
      via modules/ui/scripts/build.sh if modules/ui changed
- [ ] I understand review is read-only: my code runs only on GitHub CI runners
- [ ] Security-touch surfaces named: modules/trust/, attach/fetch
      endpoints, config, shell tool, server-side URL fetch - or "none touched"
- [ ] New dependencies in go.mod / package.json are justified in the
      PR description, or none were added
- [ ] Nothing personal: no personal files, no seeded user data, no
      embedded profiles
- [ ] No secrets: no .env, no credential patterns, no key files
      (auto-reject if any of these are touched)
- [ ] Tests cover the new behavior (same standard as our internal cards)
- [ ] The work is mine; AI assistance is disclosed

## Notes for reviewers

<!-- Security-sensitive areas, design trade-offs, anything that needs
     a second opinion. -->