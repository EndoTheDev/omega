---
name: omega
colors:
  z0: "#121212"          # canvas (dark default)
  z1: "#1a1a1c"          # main surfaces: bars, panes
  z1_raised: "#212124"   # cards/bubbles inside a layer-1 pane
  z2: "#26262a"          # overlays
  stroke: "#2e2e31"
  text: "#e6e6e9"
  text_dim: "#8a8a92"
  accent: "#E8833A"
  ok: "#3ecf6e"
  err: "#e05252"
  # light stack: z0 #ececec, z1 #f7f7f8, z1-raised #ededf0, z2 #ffffff,
  # stroke #d5d5da, text #1a1a1d, text-dim #6a6a72
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
  chip: 4px
  card: 6px
  pane: 8px
shadows:
  layer1: "0 1px 4px rgba(0,0,0,0.40)"    # tight blur - fits the pane-area breathing room
  layer2: "0 8px 30px rgba(0,0,0,0.50)"
  # light mode: layer1 rgba(0,0,0,0.10), layer2 rgba(0,0,0,0.16)
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

Elevation by distance from the light - every surface states its layer:

- **z0 (#121212):** the canvas, darkest. Bars sit flat on it.
- **z1 (#1a1a1c):** main surfaces - panes and bars, one step lighter, floating
  (border + radius + shadow).
- **z1-raised (#212124):** cards and bubbles inside a layer-1 pane.
- **z2 (#26262a):** overlays - dropdowns, popovers - deeper shadow.
- **Accent (#E8833A):** the single accent - same warm orange family as the
  beatmatch trainer and the endothe.dev accent. Interaction only: send
  button, active states, streaming cursor, links.
- **Stroke (#2e2e31):** hairline borders; text-dim (#8a8a92) for metadata,
  timestamps, session titles. Quiet, never competing with content.
- **ok (#3ecf6e) / err (#e05252):** status colors; never decorative.

Light mode is the same ladder inverted: #ececec canvas, #f7f7f8 surfaces,
#ffffff overlays.

One accent color. Gradients, glassmorphism, and glow are absent unless they
communicate state.

## Typography

Public Sans everywhere - continuity with endothe.dev. JetBrains Mono for code
blocks, tool output, and anything the agent emitted verbatim. Chat text is
1rem/1.6 line-height; headings are modest - the conversation is the hero, the
chrome whispers.

## Layout

VS Code-style shell: top menu bar (35px, flat chrome) - pane area - status
bar (29px). Panes are rounded floats on the canvas: sessions sidebar left,
chat center, inspector/settings sharing the right slot (never both). All
three panes resizable via dividers; the chat never shrinks below its 320px
floor and the sidebars yield first. Below 768px the sidebars become
slide-in drawers over a scrim.

- **Chat:** full-height column. Message list left-anchored, input docked
  at bottom. Streaming tokens render with a subtle trailing cursor.
- **Sessions:** left pane - title, relative time. One tap loads history.
- **Settings:** the right slot's content when the gear is toggled -
  reasoning, theme, and future groups. The gear is the toggle: no title
  row, no close button.

## Motion

Two permitted animations: the streaming cursor pulse and a 150ms fade on
message arrival. Nothing else moves.

## Dark-first, light-parity

Dark is the default identity; light mode is full parity via the token system
(the beatmatch-trainer tri-state pattern: auto/light/dark, persisted).