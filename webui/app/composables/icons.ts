// omega webui: THE ICONSET - the icon law (only-lucide-reference,
// one name one meaning, wrapper, throw-on-unknown) lives in
// .agents/DESIGN.md (card t_502b8e81). This file is the vocabulary.
import {
  Activity,
  Archive,
  ArchiveRestore,
  // card t_6215474b: the nav cluster + command palette chrome
  ArrowLeft,
  ArrowRight,
  Command,
  // card t_61cc8762: the sessions row's copy-id action
  Copy,
  Inbox,
  Book,
  Box,
  Braces,
  Brain,
  ChartColumn,
  ChevronDown,
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
  // card t_7f09bd0c: the skill reference pill's glyph
  Zap,
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
  logs: ScrollText, // the log strip glyph (the Logs tab carries "activity")
  skills: Book, // the skills library
  tools: Wrench, // the tools inventory - also the tool-call block glyph
  agents: Users, // the agent roster (merged config + continuous surface)
  settings: SlidersHorizontal, // config knobs
  kanban: Kanban, // the board
  extensions: Puzzle, // the module cockpit tab
  insights: ChartColumn, // usage bars
  mcp: PlugZap, // the MCP servers tab (bridge connections + tools)
  terminal: SquareTerminal, // the terminal tab (card t_55a3fc4b: the
  // integrated shell; the SquareTerminal lucide glyph - "file-script"
  // carries the same shape deliberately: the script-class file glyph
  // IS terminal semantics)
  // card t_e703180e: the observability tab - the live run-event stream.
  // Its own glyph, not the logs scroll: "one name one meaning" - the
  // scroll is the raw file tail (the Logs strip), the pulse is the
  // structured stream of what the process is doing.
  activity: Activity,
  browser: Globe, // the general web browser tab (card t_95c91f9c)
  // default glyph for modules that declare no icon of their own
  module: Box,
  // card t_6215474b: the nav cluster (back / forward) + the command
  // palette trigger - the chrome lives in the menubar's centered cluster
  "nav-back": ArrowLeft, // the back arrow (history bottom dims)
  "nav-forward": ArrowRight, // the forward arrow (stack top dims)
  "palette": Command, // the command palette button (Ctrl+K)
  // shell chrome
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
  star: Star, // pinned sessions
  download: Download, // session export
  copy: Copy, // the row's copy-session-id action (card t_61cc8762)
  pencil: Pencil, // inline rename
  plus: Plus, // the cron tab's add-job editor tool (card t_ecec864e)
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
  // card t_8a569163: the attachment chips' classes the tree never needed
  // - a pdf, a spreadsheet, an archive. The tree's own table
  // (harness/files/ui/rowicon.ts) names the rest.
  "file-pdf": FileType,
  "file-sheet": FileSpreadsheet,
  "file-archive": FileArchive,
  "folder-open": FolderOpen,
  // the Files tab toolbar (card t_a4504a25): create, reload, fold
  "file-plus": FilePlus,
  "folder-plus": FolderPlus,
  refresh: RefreshCw,
  unfoldless: FoldVertical, // "collapse folders" - the fold glyph
  // card t_7f09bd0c: the skill reference pill - the zap reads "invocation"
  zap: Zap,
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