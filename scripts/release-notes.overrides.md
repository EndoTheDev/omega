# release notes: per-release hand-written prose

Read by `scripts/release-notes.sh` (or by `NOTES_OVERRIDES=...`). One section
per release; the script prefers this file over the tag annotation for the
post's hook and bullets. The tag annotation stays the fallback, so a release
with no section here still renders (the hook is then generated from the class
counts, and the script says so on stderr).

Format:

    ## <version>          a section heading naming the version exactly
    hook: <one sentence>   plain words, the theme and why it matters
    - <bullet>             user-observable behavior, never the mechanism
    - <bullet>             at most five (the five-highlight law)

Rules for the text:

- Write for two readers at once. A new user should read the hook and want the
  update; an existing user should understand what is new or different.
- A bullet states what a user sees or does, never how it is built. Test each
  one: would someone who never worked on this codebase understand it without
  asking?
- A theme name may appear in the hook, but the hook must also say what changed
  in user-visible terms.
- Plain hyphens only: no em-dashes, no en-dashes (the script normalizes them
  anyway, and the style law is the repo's).
- Do not restate the stats line - the script prints it from the commit log.

## v0.17.3

hook: The make-over: the web ui's biggest visual and interaction refresh yet.

- the command palette now edits the top bar's clock field right where it lives - click, type, done, no trip through settings
- the agents tab now works like sessions do: agent cards with one-line previews, timestamps, and rebind-to-canonical-chat
- under the hood, the build pipeline runs more work in parallel without collisions
- folder references pin like file references, so a whole folder can ride along with a message
- the files tree was rebuilt: hover guides show where a row sits and the tree scans faster
