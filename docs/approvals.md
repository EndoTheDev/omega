# The Approval Gate

The approval capsule (`modules/approval/`) gates shell commands with
write, network or system effects: flagged commands pass a reviewer
before they run. Benign traffic pays one detector pass and never a
cloud call or a prompt.

## Modes (`approvals.mode`)

| mode     | behavior |
|----------|----------|
| `off`    | The gate is fully absent: no detection, no calls, no logging. The seam mounts nothing (proved by `TestApprovalRemovalProof`). |
| `manual` | Default (ship dark). Every flagged command asks YOU via a verdict card - approve/deny (+ a typed follow-up answer, t_63204dfd). |
| `smart`  | A decision model reviews flagged commands first; the deterministic ladder decides from its answers. |

Empty config value = `manual`. Reviewer unreachable, timed out, or
malformed = fail-closed: the command escalates, it never auto-runs.

## What reaches the reviewer

The rule-based risky-command detector (`detect.go`) flags parsed
commands carrying: system/process control, file writes, network
egress, secret material, privilege escalation, dynamic execution,
interpreter/script execution, redirect writes, command substitution,
or an unparsable static form. It decides nothing - it only decides
WHO looks next. A false ask costs one click (manual) or one cached
forward pass (smart); a false pass costs whatever the command does.

## The reviewer (smart mode)

One forward pass against the configured provider's `/v1/systemone`
passthrough (`approvals.model`, conventionally `clef:27b`), asking
the six questions verbatim - `verdict`, `policy_allows`,
`blast_radius`, `self_advocating`, `reads_secrets`, `sends_outbound`.
The command is reviewed in its shell-parsed, statically expanded form
(quotes, tildes and known vars resolved; `$()`/backticks visible) -
never the raw string. Answer-schema fragments and credential-shaped
material are stripped before egress.

The model advises, Go decides. The ladder (`ladder.go`), in order:

1. `self_advocating >= 0.6` -> ESCALATE (the text argues for itself)
2. `reads_secrets >= 0.7` AND `sends_outbound >= 0.7` -> **DENY**
   (the one hard deny: credential exfiltration)
3. `reads_secrets >= 0.7` -> ESCALATE
4. `policy_allows >= 0.7` with `smart_policy` present and
   `blast_radius < 2` -> APPROVE
5. model APPROVE with `confidence < 0.55` or `blast_radius >= 1.6`
   -> ESCALATE
6. otherwise the model verdict stands - but a model DENY never
   blocks on its own; it escalates to the human.

## Escalation

- Interactive: an approve/deny/follow-up verdict card (title = the
  command, body = the fired step and why; card t_63204dfd). Approve
  runs it, deny skips it, follow-up prompt denies it AND carries the
  typed guidance to the agent (its tool error reads
  "follow-up guidance: <text>"). A typed answer never runs the
  command and is never cached (situational, not a standing verdict);
  the smart-mode stamp is `escalated -> answered` with the text
  attached. 20-minute expiry is fail-closed - nothing ran.
- Answerable from two surfaces: the chat transcript (full
  three-option card at the escalated tool call row) and the
  notifications tab (approve/deny only; the row click still jumps to
  the session). Both answer the SAME pending card through the SAME
  verdict endpoint; first resolution wins, every other surface
  collapses.
- Cron/delegate: the command is NOT executed; the worker gets a clean
  "escalated" tool error it routes around, and a notification surfaces
  to the operator. Workers never block on a human.

## Cache

Verdicts are cached per (canonical command, resolved-target content
hash, policy hash): an identical repeat costs zero model calls, a
mutated script re-asks, and a `smart_policy` edit invalidates the
whole cache. Only settle answers (approve/deny) cache; escalations
never replay.

## Audit log

Every verdict appends one JSONL row to
`<home>/approval-decisions.jsonl` (mode 0600, rotated at 4 MB):
the parsed command, all six answers, the fired ladder step, rule
hits, policy hash, mode, origin and latency. The log is the eval
data source - the corpus regression (`TestDetectorCorpus`,
ladder table tests, adversarial canaries) pins the expected behavior;
the audit log measures it in production.

## Config keys

```yaml
approvals:
  mode: manual        # off | manual | smart (default manual)
  model: clef:27b     # decision model (smart mode; decision-family dropdown)
  smart_policy: |     # trusted operator policy the reviewer reads
    ...
```

`approvals.model` rides the typed model picker (decision-filtered
dropdown in the settings pane). All knobs apply on restart.

## Removability

Capsule law: removing `modules/approval/` removes the feature whole -
detection, parser, reviewer, ladder, cache, audit file and settings
group. Outside the capsule only the `core.ApprovalProvider` seam, the
loop's consult hook and the host's config/settings wiring remain.
 Removal proof: `TestApprovalRemovalProof` (mounts the capsule, mode
off, and the module removed).