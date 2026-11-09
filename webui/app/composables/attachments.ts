// omega webui: non-image attachment helpers (card t_8a569163) - the
// pure side of the composer strip and the sent bubble. A reference is
// core.Attachment{kind,name,size,path}: the server wrote the bytes into
// the session's upload dir, the client only ever holds the record. So
// the strip shows a TYPE GLYPH + name + size and never a thumbnail -
// there are no bytes to paint.
//
// The icon table lives in the SHELL, not in harness/files/ui/rowicon.ts:
// the composer is shell surface and must survive the files capsule's
// removal. For every extension BOTH tables know the answer is the same
// glyph (a .md row in the tree and a .md attachment chip are one
// meaning); attachments.test.ts pins that agreement so the two tables
// cannot drift apart silently.
//
// kind is the server's own label (server_attachments.go attachmentKind):
// the lowercased extension without its dot, "file" when the name carries
// none. It is a label, never a sniff - nothing here inspects content.
import { bytesLabel } from "./omega.ts";

type IconName = string; // an iconset vocabulary name (composables/icons.ts)

// extension -> iconset name. Groups follow the files tree's glyph kinds
// (db / log / config / md / code / script / text / generic) plus the
// attachment-only classes the tree never needed (pdf, spreadsheet,
// archive). Unknown kinds fall to the generic file glyph.
const KIND_ICONS: Record<string, IconName> = {
  pdf: "file-pdf",
  csv: "file-sheet",
  tsv: "file-sheet",
  xls: "file-sheet",
  xlsx: "file-sheet",
  ods: "file-sheet",
  zip: "file-archive",
  tar: "file-archive",
  gz: "file-archive",
  tgz: "file-archive",
  "7z": "file-archive",
  rar: "file-archive",
  md: "file-text",
  markdown: "file-text",
  txt: "file-text",
  text: "file-text",
  log: "file-text",
  json: "file-config",
  jsonl: "file-config",
  yaml: "file-config",
  yml: "file-config",
  toml: "file-config",
  ini: "file-config",
  conf: "file-config",
  config: "file-config",
  env: "file-config",
  go: "file-code",
  ts: "file-code",
  tsx: "file-code",
  js: "file-code",
  jsx: "file-code",
  mjs: "file-code",
  cjs: "file-code",
  vue: "file-code",
  py: "file-code",
  rs: "file-code",
  c: "file-code",
  h: "file-code",
  cpp: "file-code",
  hpp: "file-code",
  java: "file-code",
  kt: "file-code",
  rb: "file-code",
  php: "file-code",
  css: "file-code",
  scss: "file-code",
  html: "file-code",
  sh: "file-script",
  bash: "file-script",
  zsh: "file-script",
  bat: "file-script",
  ps1: "file-script",
  cmd: "file-script",
  db: "file-db",
  sqlite: "file-db",
  sqlite3: "file-db",
  sql: "file-db",
};

/** attachmentIcon maps a reference's kind to its iconset glyph name. */
export function attachmentIcon(kind: string): IconName {
  return KIND_ICONS[(kind || "").toLowerCase()] ?? "file-generic";
}

/** attachmentSizeLabel is the chip's size text (512 B / 2 KiB / 1.5
    MiB) - the ONE byte formatter (omega.ts bytesLabel), never a second
    rounding rule. */
export function attachmentSizeLabel(size: number | undefined): string {
  return bytesLabel(size);
}

// the extensions whose files are IMAGES (the base64 lane): the browser
// types most of them, but a pasted blob or an odd mime still belongs to
// the image lane when the NAME says so. Magic sniffing stays the final
// word on that lane (mediaTypeFromBase64) - this only routes the pick.
const IMAGE_EXTS = new Set(["png", "jpg", "jpeg", "gif", "webp", "bmp"]);

/** isImageFile routes a picked File: an image/* mime or an image
    extension rides the base64 image lane, everything else the upload
    reference lane. */
export function isImageFile(file: { name: string; type: string }): boolean {
  if ((file.type || "").startsWith("image/")) return true;
  const dot = file.name.lastIndexOf(".");
  if (dot < 0) return false;
  return IMAGE_EXTS.has(file.name.slice(dot + 1).toLowerCase());
}
