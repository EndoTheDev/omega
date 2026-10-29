// omega webui: THE ICONSET (card t_502b8e81). One name one meaning, one
// import path: every icon in the webui flows through this map into the
// Icon.vue wrapper. lucide-vue-next (thin monochrome line icons - the
// shell's existing stroke style) is imported by NAME only, so the bundle
// stays tree-shaken: no icon font, no hand-rolled SVG, no second library.
// Registry entries (dockables.ts) carry the name string; this file is the
// only place a lucide component is referenced.
import {
  Archive,
  ArchiveRestore,
  Bell,
  Book,
  Box,
  Braces,
  Brain,
  ChartColumn,
  ChevronDown,
  CircleDot,
  Clock,
  Database,
  Download,
  File,
  FileCode,
  FilePlus,
  FileText,
  Folder,
  FolderOpen,
  FolderPlus,
  FoldVertical,
  GitFork,
  History,
  Kanban,
  MessageSquare,
  PanelBottomClose,
  PanelBottomOpen,
  PanelLeftClose,
  PanelLeftOpen,
  PanelRightClose,
  PanelRightOpen,
  Pencil,
  PlugZap,
  Puzzle,
  RefreshCw,
  ScrollText,
  SlidersHorizontal,
  SquareTerminal,
  Star,
  Trash2,
  Users,
  Wrench,
} from "lucide-vue-next";
import type { LucideIcon } from "lucide-vue-next";

// semantic name -> lucide component. The names are the VOCABULARY: a tab,
// a chat block, a menu - everything that shows the same concept shows the
// same icon. Adding an icon: one entry here, then `name` at the call site.
export const ICONS = {
  // the dock registry (dockables.ts DOCKABLES[].icon)
  // card t_902f7ba5: the glyph says SESSIONS, not "panel edge" - the
  // pane is the conversations list, so it carries the history clock.
  // The KEY stays "sidebar": that is the dock registry's pinned-left
  // component id (pure-string contract, icons.ts comment).
  sidebar: History, // the sessions pane (locked left tab)
  chat: MessageSquare, // a chat tab / "new chat"
  files: Folder, // the file browser tab
  cron: Clock, // wake schedules
  logs: ScrollText, // the http_channel log strip
  skills: Book, // the skills library
  tools: Wrench, // the tools inventory - also the tool-call block glyph
  agents: Users, // the agent roster
  continuous: CircleDot, // the wake pulse in a frame (the roster tab)
  settings: SlidersHorizontal, // config knobs
  kanban: Kanban, // the board
  extensions: Puzzle, // the module cockpit tab
  insights: ChartColumn, // usage bars
  mcp: PlugZap, // the MCP servers tab (bridge connections + tools)
  // default glyph for modules that declare no icon of their own
  module: Box,
  // shell chrome
  "panel-left-open": PanelLeftOpen,
  "panel-left-close": PanelLeftClose,
  "panel-right-open": PanelRightOpen,
  "panel-right-close": PanelRightClose,
  "panel-bottom-open": PanelBottomOpen,
  "panel-bottom-close": PanelBottomClose,
  bell: Bell, // the notifications funnel
  brain: Brain, // the thinking block
  "chevron-down": ChevronDown, // collapsers (rotated per state)
  star: Star, // pinned sessions
  download: Download, // session export
  pencil: Pencil, // inline rename
  branch: GitFork, // session fork
  archive: Archive,
  unarchive: ArchiveRestore,
  trash: Trash2, // destructive delete
  // file-tree row glyphs (files/filetree, one distinct shape per class)
  folder: Folder,
  "file-generic": File,
  "file-text": FileText,
  "file-code": FileCode,
  "file-config": Braces,
  "file-script": SquareTerminal,
  "file-db": Database,
  "folder-open": FolderOpen,
  // the Files tab toolbar (card t_a4504a25): create, reload, fold
  "file-plus": FilePlus,
  "folder-plus": FolderPlus,
  refresh: RefreshCw,
  unfoldless: FoldVertical, // "collapse folders" - the fold glyph
} satisfies Record<string, LucideIcon>;

export type IconName = keyof typeof ICONS;

/** the vocabulary as data - structural tests pin registry names against it */
export const ICON_NAMES = Object.keys(ICONS) as IconName[];

/** the lookup the Icon wrapper renders through: unknown names THROW here,
    at the one place - a typo can never silently render nothing. */
export function iconOf(name: string): LucideIcon {
  const icon = ICONS[name as IconName];
  if (!icon) throw new Error(`iconset: unknown icon "${name}"`);
  return icon;
}