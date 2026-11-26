// omega webui: THE ICONSET.
import {
  Activity,
  Archive,
  ArchiveRestore,
  ArrowLeft,
  ArrowRight,
  Command,
  Copy,
  Inbox,
  Book,
  Box,
  Braces,
  Brain,
  ChartColumn,
  ChevronDown,
  ChevronRight,
  Clock,
  Database,
  Download,
  File,
  FileArchive,
  FileCode,
  FilePlus,
  FileSpreadsheet,
  FileText,
  FileType,
  Folder,
  FolderOpen,
  FolderPlus,
  FoldVertical,
  GitFork,
  Globe,
  Plus,
  History,
  Kanban,
  MailOpen,
  Minus,
  MessageSquare,
  PanelBottomClose,
  PanelBottomOpen,
  PanelLeftClose,
  PanelLeftOpen,
  PanelRightClose,
  PanelRightOpen,
  Pencil,
  PlugZap,
  RefreshCw,
  ScrollText,
  SlidersHorizontal,
  Square,
  SquareTerminal,
  Star,
  Trash2,
  Users,
  Wrench,
  X,
  Zap,
} from "lucide-vue-next";
import type { LucideIcon } from "lucide-vue-next";

// semantic name -> lucide component.
export const ICONS = {
  // the dock registry (dockables.ts DOCKABLES[].icon). The KEY stays "sidebar": that is
  // the dock registry's pinned-left component id (pure-string contract, icons.ts
  // comment).
  sidebar: History, // the sessions pane (locked left tab)
  chat: MessageSquare, // a chat tab / "new chat"
  files: Folder, // the file browser tab
  cron: Clock, // wake schedules
  logs: ScrollText, // the log strip glyph (the Logs tab carries "activity")
  skills: Book, // the skills library
  tools: Wrench, // the tools inventory - also the tool-call block glyph
  agents: Users, // the agent roster (merged config + continuous surface)
  settings: SlidersHorizontal, // config knobs
  kanban: Kanban, // the board
  insights: ChartColumn, // usage bars
  mcp: PlugZap, // the MCP servers tab (bridge connections + tools)
  terminal: SquareTerminal, // the terminal tab
  activity: Activity,
  browser: Globe, // the general web browser tab
  module: Box,
  "nav-back": ArrowLeft, // the back arrow (history bottom dims)
  "nav-forward": ArrowRight, // the forward arrow (stack top dims)
  "palette": Command, // the command palette button (Ctrl+K)
  "panel-left-open": PanelLeftOpen,
  "panel-left-close": PanelLeftClose,
  "panel-right-open": PanelRightOpen,
  "panel-right-close": PanelRightClose,
  "panel-bottom-open": PanelBottomOpen,
  "panel-bottom-close": PanelBottomClose,
  inbox: Inbox, // the notifications feed tab (the bell died with its chrome)
  mailOpen: MailOpen, // the mark-all-read action (the notifications band tool)
  brain: Brain, // the thinking block
  "chevron-down": ChevronDown, // collapsers (rotated per state)
  "chevron-right": ChevronRight, // row expanders: closed at rest, rotate 90deg when open
  star: Star, // pinned sessions
  download: Download, // session export
  copy: Copy, // the row's copy-session-id action; also the desktop window's restore glyph
  // the desktop window's caption glyphs (WindowControls.vue): the header IS the title bar
  minus: Minus,
  square: Square,
  x: X,
  pencil: Pencil, // inline rename
  plus: Plus, // the cron tab's add-job editor tool
  branch: GitFork, // session fork
  archive: Archive,
  unarchive: ArchiveRestore,
  trash: Trash2, // destructive delete
  folder: Folder,
  "file-generic": File,
  "file-text": FileText,
  "file-code": FileCode,
  "file-config": Braces,
  "file-script": SquareTerminal,
  "file-db": Database,
  // the attachment chips' classes the tree never needed.
  "file-pdf": FileType,
  "file-sheet": FileSpreadsheet,
  "file-archive": FileArchive,
  "folder-open": FolderOpen,
  "file-plus": FilePlus,
  "folder-plus": FolderPlus,
  refresh: RefreshCw,
  unfoldless: FoldVertical, // "collapse folders" - the fold glyph
  zap: Zap,
} satisfies Record<string, LucideIcon>;

export type IconName = keyof typeof ICONS;

/** the vocabulary as data - structural tests pin registry names against it */
export const ICON_NAMES = Object.keys(ICONS) as IconName[];

/**
 * the lookup the Icon wrapper renders through: unknown names THROW here, at the one
 * place - a typo can never silently render nothing.
 */
export function iconOf(name: string): LucideIcon {
  const icon = ICONS[name as IconName];
  if (!icon) throw new Error(`iconset: unknown icon "${name}"`);
  return icon;
}