// FileTree row glyphs (card t_467a0386): VS Code-style extension icons.
// Monochrome inline SVG, currentColor ONLY - the row's dim/hover states
// carry the color, no icon-font dependency, no new palette entries.
// The map is a pure table so node --test can pin it: known extensions
// resolve to their glyph class, everything unknown falls to "generic".

export type GlyphKind =
  | "db"
  | "log"
  | "config"
  | "md"
  | "code"
  | "script"
  | "text"
  | "generic";

// extension (lowercased, no dot) -> glyph class. omega's own home is the
// visible case: .go .db .log .yaml .json .md all resolve distinctly.
const EXT_GLYPHS: Record<string, GlyphKind> = {
  db: "db",
  sqlite: "db",
  log: "log",
  yaml: "config",
  yml: "config",
  json: "config",
  toml: "config",
  md: "md",
  markdown: "md",
  go: "code",
  ts: "code",
  tsx: "code",
  js: "code",
  mjs: "code",
  cjs: "code",
  vue: "code",
  py: "code",
  rs: "code",
  sh: "script",
  bash: "script",
  txt: "text",
};

// glyphKindFor: "main.go" -> "code", ".gitignore" -> "generic",
// "Makefile" -> "generic", "UNKNOWN.bin" -> "generic". Case-insensitive
// on the extension; a dot only counts when it NAMES an extension.
export function glyphKindFor(name: string): GlyphKind {
  const dot = name.lastIndexOf(".");
  if (dot <= 0) return "generic";
  return EXT_GLYPHS[name.slice(dot + 1).toLowerCase()] ?? "generic";
}

// 16-box stroke glyphs at row size; stroke=1.5 survives 14px rendering.
// One path set per class - at 14px, monochrome, that is the distinctness
// budget: doc-with-fold, doc-with-rules (text/log/md), braces (config),
// brackets (code), prompt (script), cylinder (db), page (generic).
const P_PATHS: Record<GlyphKind, string[]> = {
  generic: [
    "M4.5 1.5h4.6l3.4 3.4v9.6h-8z",
    "M9.1 1.5v3.4h3.4",
  ],
  text: [
    "M4.5 1.5h4.6l3.4 3.4v9.6h-8z",
    "M6 8.2h4.4",
    "M6 10.8h4.4",
  ],
  log: [
    "M4.5 1.5h4.6l3.4 3.4v9.6h-8z",
    "M6.2 6.2h3.9",
    "M6.2 8.7h3.9",
    "M6.2 11.2h3.9",
  ],
  md: [
    "M4.5 1.5h4.6l3.4 3.4v9.6h-8z",
    "M5.8 7.6h5",
    "M5.8 11.2h5",
  ],
  config: [
    "M6.4 2.6c-1.6 0-1.5 1.6-1.5 2.6 0 1.2-.5 2.3-1.4 2.8.9.5 1.4 1.6 1.4 2.8 0 1-.1 2.6 1.5 2.6",
    "M9.6 2.6c1.6 0 1.5 1.6 1.5 2.6 0 1.2.5 2.3 1.4 2.8-.9.5-1.4 1.6-1.4 2.8 0 1 .1 2.6-1.5 2.6",
  ],
  code: [
    "M6.2 5.2 3.6 8l2.6 2.8",
    "M9.8 5.2 12.4 8l-2.6 2.8",
  ],
  script: [
    "M5 5.5 8.2 8 5 10.5",
    "M9.6 11.5h2.4",
  ],
  db: [
    "M3.2 4.2v7.6c0 1.05 2.15 1.9 4.8 1.9s4.8-.85 4.8-1.9V4.2",
    "M3.2 4.2c0 1.05 2.15 1.9 4.8 1.9s4.8-.85 4.8-1.9-2.15-1.9-4.8-1.9-4.8.85-4.8 1.9z",
  ],
};

// folder outline; the OPEN state fills it - the fill difference is the
// state, not a new color (fill and stroke are both currentColor).
const FOLDER =
  "M2.8 4.6c0-.61.4-1.1 1-1.1h3l1.5 1.9h4.9c.55 0 1 .45 1 1v6.3c0 .61-.45 1.1-1 1.1h-9.4c-.6 0-1-.49-1-1.1z";

function svg(pathDs: string[], fillCurrent: boolean): string {
  const body = pathDs
    .map((d) => `<path d="${d}" stroke-linecap="round" stroke-linejoin="round"/>`)
    .join("");
  const fill = fillCurrent ? "currentColor" : "none";
  return (
    `<svg viewBox="0 0 16 16" aria-hidden="true" fill="${fill}" ` +
    `stroke="currentColor" stroke-width="1.5">${body}</svg>`
  );
}

// glyphSvg: a file row's icon for a glyph class (static markup, no user
// data ever flows into the string - callers hand a GlyphKind and get
// inline SVG, so v-html is safe by construction)
export function glyphSvg(kind: GlyphKind): string {
  return svg(P_PATHS[kind] ?? P_PATHS.generic, false);
}

// folderSvg: filled when open, outlined when collapsed
export function folderSvg(open: boolean): string {
  return svg([FOLDER], open);
}