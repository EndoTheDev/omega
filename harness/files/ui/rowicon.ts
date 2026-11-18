// FileTree row glyphs.

export type GlyphKind =
  | "db"
  | "log"
  | "config"
  | "md"
  | "code"
  | "script"
  | "text"
  | "generic";

// extension (lowercased, no dot) -> glyph kind.
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

// glyphKindFor: "main.go" -> "code", ".gitignore" -> "generic", "Makefile" ->.
export function glyphKindFor(name: string): GlyphKind {
  const dot = name.lastIndexOf(".");
  if (dot <= 0) return "generic";
  return EXT_GLYPHS[name.slice(dot + 1).toLowerCase()] ?? "generic";
}

// glyph kind -> iconset NAME.
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

// folder name: filled when open.
export function folderIcon(open: boolean): string {
  return open ? "folder-open" : "folder";
}