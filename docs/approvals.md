# The Approval Gate

The approval capsule (`harness/approval/`) gates shell commands with
write, network or system effects: flagged commands pass a reviewer
before they run. Benign traffic pays one detector pass and never a
cloud call or a prompt.

## Modes (`approvals.mode`)

| mode     | behavior                                                                                                                        |
| -------- | ------------------------------------------------------------------------------------------------------------------------------- |
| `off`    | The gate is fully absent: no detection, no calls, no logging. The seam mounts nothing (proved by `TestApprovalRemovalProof`).   |
| `manual` | Default (ship dark). Every flagged command asks YOU via a verdict card - approve/deny (+ a typed follow-up answer). |
| `smart`  | A decision model reviews flagged commands first; the deterministic ladder decides from its answers.                             |

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

Every hit is also a structured FINDING with a severity (`low` /
`medium` / `high`). On this legacy lane the severity is a label - the
routing above is unchanged - and it rides the audit row for the eval
reader.

## The request-only detector set (safeagents)

`detectcall.go` adds a second, request-only set that runs over EVERY
tool call (not just `shell.run`) and costs no network call and no
model call. It asks two questions only a request can answer:

- **Provenance.** Does the host this call reaches appear in the
  HUMAN's own words? The model's assistant turns are excluded by
  ruling - a host the model wrote earlier is no more sourced than one
  it writes now, so self-citation is not provenance. An empty
  conversation (a one-shot run, a gate consulted without the loop's
  handoff) sources nothing.
- **Reach.** A host only counts in a call that can reach it: the tool
  name matches `fetch|browse|browser|web|http|url|download`, or a
  command line carries `curl`, `wget`, `nc`, `ssh`, `git clone`,
  `pip install`, `/dev/tcp/`, `requests.`, `httpx.` (plus the clients
  the legacy detector already knows). URL hosts count anywhere in a
  reaching call - the code names the URL a line before opening it -
  while a bare IP counts only on a reaching line: a line carrying a
  network client, or any argument line of a call whose TOOL NAME is
  itself the reach surface (`web.fetch`'s arguments are what it
  fetches, so a bare `host` value reaches on its own).

The allowlist is fixed: `github.com`, `pypi.org`, `pythonhosted.org`,
`huggingface.co`, `hf.co`, `npmjs.org`, `crates.io`, `localhost`,
`127.0.0.1`, and the RFC 2606 / RFC 6761 reserved names
(`example.com/.net/.org`, `.example`, `.test`, `.invalid`,
`.localhost`) which no one can register.

Two precision rulings keep the corpus quiet:

- digits after `=` are a version, not an address (`pkg==0.5.1.4`), and
  every octet must fit a byte;
- argument values are DECODED before matching (a JSON payload carried
  as a string is decoded once, and a decoded newline is a line
  boundary) - matching raw wire text would let an escaped `\n` in
  front of `cat` read as `ncat`.

The Cohere-report behaviours the set names: `/proc/*/environ` reads,
`curl --noproxy`, the Kubernetes service-account token path,
`unshare`/`nsenter` namespace escape, `.netrc` reads, and reads of the
agent harness's own credential surface.

**Severity decides the route.** A `high` finding blocks on its own
(the cheap regex catches the obvious: no model, no human). A `medium`
finding marks the call SUSPICIOUS and only those escalate - judge
first, then the human. Nothing is cached on this lane: the block path
costs no model call, and a judge-augmented escalation is situational.

**The rollout is log-only.** `approvals.monitor` defaults to
`log-only`: every finding is recorded in the audit log as a
WOULD-BLOCK and the call proceeds exactly as it would have. That is
the soak every new detector ships through; `enforce` flips it once a
real-run soak shows no false-positive pile.

## The evidence-only judge

When a suspicious call escalates, the judge reads EXACTLY three
things: the human's task text, the pending tool call, and the
deterministic findings. It never sees the worker agent's reasoning -
an agent that wants a call to run would argue its case there. The
prompt declares the task and the call as DATA, not instructions, and
tells the judge that prose addressing it is evidence against the
call.

The ratchet is Go's: the judge answers one probability (`block`) and
the threshold decides. A block ADDS a finding; no answer ever lifts a
deterministic one. A dead judge fails OPEN by default - it adds
nothing and the human still decides - and fails CLOSED when
`approvals.judge_fail_closed` is on.

The judge is a configured reviewer, so the task text and the call
cross the SAME `sanitize()` egress boundary the reviewer lane rides
(`redact.go`): answer-schema fragments (the judge's own `block`
vocabulary included) and credential-shaped material are stripped
before the body leaves the machine. The log-only soak's WOULD-BLOCK
row is sanitized the same way, so the soak log never holds a cleartext
credential.

## The reviewer (smart mode)

One forward pass against the configured provider's `/v1/systemone`
passthrough using the decision model (the `approvals_model` slot in
provider settings - `instance:model` routes the consult through a
named provider instance; conventionally `clef:27b`), asking the six
questions verbatim - `verdict`, `policy_allows`,
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
  command, body = the fired step and why). Approve
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
hits, policy hash, mode, origin and latency. Every row also names the
run's session: the http channel and the CLI loop wrap the session id
on the run context, and a cron fire or `run --session` turn that only
persists through its turn sink inherits it from the agent's
attribution default - a background origin's decision is evidence
about a session, never anonymous. The log is the eval
data source - the corpus regression (`TestDetectorCorpus`,
ladder table tests, adversarial canaries) pins the expected behavior;
the audit log measures it in production.

The request-only set's rows carry three extra fields: `findings` (one
record per detector hit: name, severity, tiered, detail), `monitor`
(true on a log-only dry run - the row is a WOULD-BLOCK measurement and
nothing about the call changed), and `judge` / `judge_block` (the
evidence-only judge's state: `blocked`, `cleared`, `unavailable` or
`not-consulted`, with its probability). A monitor row's `decided_by`
reads `monitor`; an enforced block reads `detector`.

## Config keys

```yaml
approvals:
  mode: manual # off | manual | smart (default manual)
  smart_policy: | # trusted operator policy the reviewer reads
    ...
  monitor: log-only # log-only (default) | enforce - the request-only
                    # detector set's rollout: soak first, then enforce
  judge_fail_closed: false # the evidence-only judge unreachable:
                           # false = fail open, the human still decides
```

The decision model moved to provider settings: the
`approvals_model` slot (`provider.approvals_model` in the settings
pane) rides the typed model picker, decision-filtered, and the picker
also lists the named provider instances' models as `instance:model`
values - the consult routes through that instance. Empty = fail-closed
(every flagged command escalates). The gate re-reads its config on
every decision: the mode, the policy and the model
slot apply LIVE - a settings-pane save (or a hand-edited config.yaml)
reaches the running gate without a restart (the host's hot-apply path
pushes the fresh section; the reviewer request is rebuilt per ask
from the live values). The one restart line that remains: `off` AT
BOOT keeps the gate unmounted entirely - re-mounting is a restart.

## Removability

Capsule law: removing `harness/approval/` removes the feature whole -
detection, parser, reviewer, ladder, cache, audit file and settings
group. Outside the capsule only the `core.ApprovalProvider` seam, the
loop's consult hook and the host's config/settings wiring remain.
Removal proof: `TestApprovalRemovalProof` (mounts the capsule, mode
off, and the module removed).
