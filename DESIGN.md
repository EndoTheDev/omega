---
name: omega
colors:
  primary: "#1A1C1E"
  secondary: "#8A9BA8"
  tertiary: "#E8833A"
  neutral: "#14161A"
  surface: "#1E2126"
  success: "#4EC96B"
  error: "#E06C75"
typography:
  h1:
    fontFamily: Public Sans
    fontSize: 1.5rem
    fontWeight: 600
  body-md:
    fontFamily: Public Sans
    fontSize: 1rem
  code:
    fontFamily: JetBrains Mono
    fontSize: 0.875rem
  label-caps:
    fontFamily: Public Sans
    fontSize: 0.75rem
rounded:
  sm: 4px
  md: 8px
  lg: 12px
spacing:
  sm: 8px
  md: 16px
  lg: 24px
---

## Overview

Terminal-born precision, web-delivered. omega's UI carries the discipline of its
Go core into the browser: a dark, focused workspace where the conversation is
the interface. Nothing decorative survives - every element is either a message,
a control, or chrome that holds them.

The feel is a developer's night console: deep charcoal surfaces, one warm
accent for interaction, monospace for anything the machine produced.

## Colors

- **Primary (#1A1C1E):** headline ink, kept near-white in dark mode via the
  token system; carries hierarchy, not decoration.
- **Secondary (#8A9BA8):** slate for metadata, timestamps, session titles,
  borders. Quiet, never competing with content.
- **Tertiary (#E8833A):** the single accent - same warm orange family as the
  beatmatch trainer and the endothe.dev accent. Interaction only: send button,
  active states, streaming cursor, links.
- **Neutral (#14161A):** page background, darker than surfaces so panels read
  as raised.
- **Surface (#1E2126):** message bubbles, sidebar, settings cards.
- **Success/Error:** streaming-complete and error states; never decorative.

One accent color. Gradients, glassmorphism, and glow are absent unless they
communicate state.

## Typography

Public Sans everywhere - continuity with endothe.dev. JetBrains Mono for code
blocks, tool output, and anything the agent emitted verbatim. Chat text is
1rem/1.6 line-height; headings are modest - the conversation is the hero, the
chrome whispers.

## Layout

- **Chat (default route):** full-height column. Message list left-anchored,
  max-width for readability, input docked at bottom. Streaming tokens render
  with a subtle trailing cursor.
- **Sessions:** left sidebar list (or top dropdown on mobile) - title, relative
  time, message count. One tap loads history via the existing API.
- **Settings:** narrow single column, grouped cards (provider, model,
  generation, system prompt). Forms are honest: labeled fields, explicit save.

## Motion

Two permitted animations: the streaming cursor pulse and a 150ms fade on
message arrival. Nothing else moves.

## Dark-first, light-parity

Dark is the default identity; light mode is full parity via the token system
(the beatmatch-trainer tri-state pattern: auto/light/dark, persisted).