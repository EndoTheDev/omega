// FileTree row glyphs (card t_467a0386, iconified card t_502b8e81).
// VS Code-style extension icons. The hand-rolled SVG strings are gone: a
// row glyph is now a semantic NAME in the webui iconset
// (composables/icons.ts), rendered through the Icon wrapper - one iconset,
// no inline SVG builders. The map is a pure table so node --test can pin
// it: known extensions resolve to their glyph name, everything unknown
// falls to "file-generic". The row's dim/hover states still carry the
// color (currentColor only).

export type GlyphKind =
  | "db"
  | "log"
  | "config"
  | "md"
  | "code"
  | "script"
  | "text"
  | "generic";

// extension (lowercased, no dot) -> glyph kind. omega's own home is the
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

// card t_502b8e81: glyph kind -> iconset NAME. One distinct shape per
// kind at one stroke weight (the kind table keeps the distinction
// budget); the names live in composables/icons.ts like every other icon.
const KIND_ICONS: Record<GlyphKind, string> = {
  generic: "file-generic",
  text: "file-text",
  log: "file-text",
  md: "file-text",
  config: "file-config",
  code: "file-code",
  script: "file-script",
  db: "file-db",
};

export function glyphIcon(kind: GlyphKind): string {
  return KIND_ICONS[kind] ?? KIND_ICONS.generic;
}

// folder name: filled when open (the lucide FolderOpen/Folder pair makes
// the state difference, no new color), outlined when collapsed.
export function folderIcon(open: boolean): string {
  return open ? "folder-open" : "folder";
}