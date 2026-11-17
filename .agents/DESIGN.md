# DESIGN.md

The single source of truth for omega's UI rules. A rule is stated
exactly once - here. docs/webui.md carries the values (colors, sizes,
anatomy); code headers point back. A rule restated anywhere else is a
review finding: the copy dies, this file keeps the rule.

## Colors

- Tokens live in `webui/app/assets/css/main.css`. No component invents
  a color; every color in the UI resolves to a token.
- `--unread` marks unread items only - never a generic highlight.
- Dark is the default identity; light is the same ladder inverted at
  full parity.
- A theme override recolors only: color tokens and the two layer
  shadows. Radii and fonts are never overridable - an override
  recolors, it does not restyle.
- A new token ships with a sane default in main.css.

## Icons

- `webui/app/composables/icons.ts` is the only file a lucide icon may
  be referenced from. Icons come from lucide-vue-next by NAME only -
  no icon font, no hand-rolled SVG, no second library.
- One semantic name, one meaning: the ICONS map is the vocabulary.
  Two glyphs for one concept means one goes.
- Every call site renders the shared `ui/Icon.vue` wrapper.
- An unknown name throws at render. A typo is a build bug, never a
  silent blank box.
- Sizes: 16 for menubar chrome, 13 for row actions and band tools.
  The wrapper default (14, stroke 2) serves glyphs outside both
  bands; a size never rides ad hoc inline attrs. Every band tool in
  the tree passes 13 explicitly (the notifications panel's 14 was the
  last straggler; it now passes 13 too, so the note that said "fix
  pending" is retired).

## Components

One anatomy per family, one shared component, no per-tab variants.
Consumers keep their specifics; a consumer carrying a family-anatomy
block is a review finding.

- Registry row: `webui/app/assets/css/registry-row.css`. Registry
  lists are quiet - hierarchy by typography weight and opacity, one
  hairline between rows, not borders or boxes.
- Toggle pill: `webui/app/components/ui/TogglePill.vue`. The only
  switch in the webui, and it renders as a button - never a checkbox.
  One click is one switch: no arm/confirm second click, no confirm
  label. Consequence text (what an exclusion means, a module's
  dependents) rides the pill's title, where it informs without gating.
- Panel band: `webui/app/components/ui/TabBand.vue` + `.band-tool`.

## Composer

- One composer, no modes: no display-mode setting, no mode classes,
  no centered card.
- One arrangement, stretched pane-wide: plus + input on top, a
  hairline, chips left, the orange send right.
- A floating card: 12px inset at the sides and floor; the top edge
  runs flush with the activity line. Border and rounded corners all
  around - never edge-to-edge.
- Stored display-mode values are ignored orphans - never read again,
  no migration written.

## Accessibility

- An icon-only affordance carries its accessible name on the HOST:
  `title` + `aria-label`, both required (the tooltip is the title);
  the glyph itself is aria-hidden.
- Pointer paths have keyboard parity: focus + Enter toggles the
  chevronless tree row.

## Architecture

- Deletability is the test: removing a module folder removes the
  feature from source AND binary - zero module vocabulary survives
  outside the folder. `scripts/nm-purity.sh` is the machine proof;
  docs/removing-a-module.md is the recipe.
- The bar owns the UI too: a removed capsule drops its manifest
  entry and the shell hosts nothing - no alias keeps the feature
  alive.

## How this file converges

When a doc, code header, or note restates a rule, the rule moves
here and the source gets a one-line pointer. Decision records live
outside this file.
