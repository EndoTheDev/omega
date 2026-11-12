# DESIGN.md

The design LAWS home (Endo's word, 2026-11-07). Each law is stated
exactly once - here. docs/webui.md is the map (values, anatomy,
mechanics); code headers point. A law restated anywhere else is a
review finding: the copy dies, this file keeps the law. Laws only -
tutorials and per-component prose here are this file becoming a
second map, the death of its 2026-10-30 predecessor.

## Iconset (card t_502b8e81)

- `webui/app/composables/icons.ts` is the ONLY file a lucide
  component may be referenced from: lucide-vue-next by NAME only -
  no icon font, no hand-rolled SVG, no second library.
- One semantic name = one meaning: two glyphs for one concept means
  one goes; the ICONS map is the vocabulary.
- Every call site renders the shared `ui/Icon.vue` wrapper.
- An unknown name THROWS at render (`iconOf`) - a typo is a build
  bug, never a silent blank box.

## Icon sizes (card t_502b8e81; the open nit ruled by t_59de3bbe)

- 16 = menubar chrome. 13 = row actions and band tools (the
  FilesTab precedent, card t_a4504a25, wins: band tools are 13 -
  the notifications panel's default-14 tools are drift, fix
  pending). The wrapper default (14, stroke 2) serves glyphs outside
  both bands; a size never rides ad hoc inline attrs.

## Theme tokens (values live in docs/webui.md)

- Tokens live in `webui/app/assets/css/main.css`; no component
  invents a color.
- `--unread` is an unread-ONLY semantic color, never a generic
  highlight.
- Dark-first, light-parity: dark is the default identity, light is
  the same ladder inverted at full parity.
- Theme overrides recolor only: color tokens + the two layer
  shadows. Radii and fonts are never overridable - an override
  recolors, it does not restyle.
- A new token ships a sane default in main.css.

## Composer (card t_1750a9e3, Endo ruling 2026-11-07)

- ONE composer, no modes: no display-mode setting, no mode classes,
  no centered card. The mode system is dead and stays dead.
- The C5 row arrangement is the single shape, stretched pane-wide:
  plus + input on top, a hairline, chips left + the orange send
  right.
- A floating card: 12px inset at the sides and floor; the top edge
  runs flush with the activity line. Border and rounded corners all
  around - never edge-to-edge.
- Stored display-mode values are ignored orphans - never read again,
  no migration written.

## UI families (cards t_561dc4e0, t_d93864e7, t_9ae4dea5)

- One anatomy per family, one shared component, no per-tab
  variants: the registry row (`assets/css/registry-row.css`), the
  toggle pill (`components/ui/TogglePill.vue`), the panel band
  (`components/ui/TabBand.vue` + `.band-tool`). Consumers keep
  their specifics only - a consumer carrying a family-anatomy block
  is a review finding.
- The toggle pill is the only switch in the webui, and it renders as
  a button - never a checkbox. ONE click is one switch (card
  t_5051d523): no arm/confirm second click, no confirm label. The
  consequence text (what an exclusion means, a module's dependents)
  rides the pill's title, where it informs without gating.
- Registry lists are quiet: hierarchy by typography weight and
  opacity, one hairline between rows - not borders or boxes.

## Accessibility (card t_502b8e81)

- An icon-only affordance carries its accessible name on the HOST:
  `title` + `aria-label`, both required (the tooltip is the title);
  the glyph itself is aria-hidden.
- Pointer paths have keyboard parity (focus + Enter toggles the
  chevronless tree row, card t_63582d1f).

## The architecture bar (Endo ruling, 2026-10-25)

- Deletability is the test: removing a module folder removes the
  feature from source AND binary - zero module vocabulary survives
  outside the folder. `scripts/nm-purity.sh` is the machine proof;
  docs/removing-a-module.md is the recipe.
- The bar owns the UI too: a removed capsule drops its manifest
  entry and the shell hosts nothing - no alias keeps the feature
  alive.

## Convergence

- This file converges design law; docs converge facts and
  mechanics. When a doc, code header or vault note restates a law,
  the law moves here and the site gets a one-line pointer. Vault
  files keep their decision prose (ADRs, grills) under a
  superseded-by pointer.
