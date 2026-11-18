# ISSUES.md - how to create an issue

How to report a problem or suggest a change for omega.
Anyone can file an issue - no template, no gatekeeping.

## Where

Use the GitHub issue tracker:
https://github.com/EndoTheDev/omega/issues

Open an issue, pick a clear title, describe the problem in plain words.
One issue per problem. If you found two problems, file two.

## What to include (bugs)

The more of this you give, the faster the fix:

- omega version: run `omega --version` and paste the line.
- OS and terminal (for example: Debian 12, zsh inside tmux).
- What you did, step by step.
- What you expected to happen.
- What happened instead - exact text or a screenshot.
- If it works in one place but not another, say both places.

A repro we can run ourselves is the fastest path to a fix.

## Feature requests and questions

No special form. Say what you are trying to do and what is in the way.
A request that fits the roadmap becomes a work item.

## What happens after you file

Every issue gets read in a daily triage pass. It is either:

- answered (missing detail, config question, not a bug), or
- kept open with a label while waiting for more reports, or
- turned into a work item and fixed in a coming release.

You will get a reply on the issue itself. If more detail is needed,
the reply asks for it.

## Good reports so far

The best bug reports name the version, the environment, and the exact
steps. They usually turn into fixes within days. Keep them coming.
