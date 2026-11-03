// omega webui: the file tree store - the expansion/children state half
// behind FileTree.vue, extracted from the component (card t_ccd4a000)
// so the node suite drives expansion, failures and restores without a
// DOM. One store instance per tree: the root FileTree creates it, the
// recursion shares it via provide/inject.
//
// Error law (card t_ccd4a000): a failed children request NEVER renders
// backend error text as a tree row - internal vocabulary does not leak
// into the tree body. A failed expansion leaves the folder CLOSED and
// surfaces one fixed-phrase local toast ("folder failed to load") - the
// app's established no-op + one-line-hint pattern (card t_8bcbe72e's
// reveal refusal). The failed folder is keyed by ITS OWN path
// (the path that issued the request, never a sibling's - the batched
// expansion-restore fires one request per folder), a later successful
// children response for a folder clears its mark, and a refresh clears
// every mark so failures get a fresh chance.

import { reactive, watch } from "vue";
import {
  FILETREE_STATE_KEY,
  fetchFilesBrowse,
  fileTreeBus,
  parseExpandedPaths,
  revealAncestors,
  sortFileEntries,
  serializeExpandedPaths,
} from "./omega.ts";
import type { FileEntry } from "./omega.ts";
import { emitLocalToast } from "./canvas.ts";

export interface FileTreeStore {
  isOpen(path: string): boolean;
  isLoading(path: string): boolean;
  // the failed mark of THIS folder's last children request (per-folder
  // keyed; internal state - the tree never renders it as a row)
  isFailed(path: string): boolean;
  childrenOf(path: string): FileEntry[];
  toggle(path: string): void;
  collapseAll(): void;
  refreshAll(): void;
  revealUnder(path: string): void;
}

// the failure hint's fixed payload shape: constant fields so
// emitLocalToast's fingerprint dedupe collapses a batched restore's
// identical failures into ONE toast, and so no backend error string and
// no timestamp churn ever rides it (the toast stack defaults the rest).
const FAILURE_TOAST = {
  id: 0,
  kind: "chat",
  source_id: "files-tree",
  title: "folder failed to load",
  verdict: "ok",
  read: true,
} as const;

export function createFileTreeStore(opts?: {
  // the wire - injectable so the node suite drives the store directly;
  // default is the real /files/browse fetch
  browse?: typeof fetchFilesBrowse;
  // the focused session's id, read live at fetch time (t_a5f9d185)
  session?: () => string | undefined;
  // extra paths treated as restored-expanded before the persisted set
  // loads (test convenience - the restore path of record stays
  // localStorage)
  seed?: string[];
  // the failure surface - injectable for assertions; default is the
  // fixed-phrase local toast above
  onError?: (path: string, message: string) => void;
}): FileTreeStore {
  const browse = opts?.browse ?? fetchFilesBrowse;
  const session = opts?.session ?? (() => undefined);
  const onError =
    opts?.onError ??
    (() => {
      emitLocalToast(FAILURE_TOAST);
    });

  const expanded = reactive(new Set<string>());
  const loading = reactive(new Set<string>());
  const kids: Record<string, FileEntry[]> = reactive({});
  // keyed by the folder path that ISSUED the failed request - the
  // per-folder law a sibling's failure must never cross (t_ccd4a000)
  const failed: Record<string, string> = reactive({});

  // ensureLoaded fetches one directory level for an expanded branch;
  // in-flight and already-loaded reads are no-ops. A failed read
  // collapses the branch (the folder stays CLOSED), marks THAT folder,
  // and hints once; a successful read replaces the children and clears
  // the mark.
  function ensureLoaded(path: string) {
    if (kids[path] || loading.has(path)) return;
    kids[path] ??= [];
    loading.add(path);
    browse(path, session())
      .then((data) => {
        kids[path] = sortFileEntries(data.entries ?? []);
        delete failed[path];
      })
      .catch((e) => {
        delete kids[path];
        failed[path] = e instanceof Error ? e.message : String(e);
        expanded.delete(path);
        onError(path, failed[path]);
      })
      .finally(() => loading.delete(path));
  }

  // persisted expanded set (FILETREE_STATE_KEY): malformed JSON parses
  // empty, invalid paths are dropped by parseExpandedPaths. Hidden
  // folders (.git, .agents) restore since the guard narrowed. The
  // injected seeds load FIRST and independently of storage health - a
  // blocked localStorage must never eat the caller's own state.
  for (const p of opts?.seed ?? []) expanded.add(p);
  try {
    for (const p of parseExpandedPaths(
      localStorage.getItem(FILETREE_STATE_KEY) ?? "",
    ))
      expanded.add(p);
  } catch {
    // no localStorage (or blocked): state is per-session, fine
  }
  watch(
    expanded,
    () => {
      try {
        localStorage.setItem(
          FILETREE_STATE_KEY,
          serializeExpandedPaths(expanded),
        );
      } catch {
        // persistence is a courtesy, never a failure
      }
    },
  );

  const store: FileTreeStore = {
    isOpen: (p) => expanded.has(p),
    isLoading: (p) => loading.has(p),
    isFailed: (p) => !!failed[p],
    childrenOf: (p) => kids[p] ?? [],
    toggle(path: string) {
      if (expanded.has(path)) {
        expanded.delete(path);
        return;
      }
      expanded.add(path);
      ensureLoaded(path);
    },
    collapseAll() {
      expanded.clear();
    },
    refreshAll() {
      // drop every loaded branch's cache and every failed mark, then
      // reload the open set - a create inside an expanded folder must
      // turn up here, and a previously failed branch re-arms
      for (const p of Object.keys(kids)) delete kids[p];
      for (const p of Object.keys(failed)) delete failed[p];
      for (const p of [...expanded]) ensureLoaded(p);
    },
    revealUnder(path: string) {
      for (const p of revealAncestors(path)) {
        expanded.add(p);
        ensureLoaded(p);
      }
    },
  };

  // Restored branches have no toggle to fire their lazy load - fetch
  // every persisted level up front so a reloaded tree renders whole.
  // One request per folder: every failure lands on its own key.
  for (const p of [...expanded]) ensureLoaded(p);

  // The FilesTab toolbar's seam (card t_a4504a25): the tab cannot reach
  // this store, it talks through fileTreeBus. The STORE owns these (the
  // component never double-applies - createFileTreeStore runs once per
  // tree). Watchers read the bus fields, they never write: the bus
  // stays a one-way seam.
  watch(
    () => fileTreeBus.collapse,
    () => expanded.clear(),
  );
  watch(
    () => fileTreeBus.refresh,
    () => store.refreshAll(),
  );
  watch(
    () => fileTreeBus.reveal,
    (path) => store.revealUnder(path),
  );

  return store;
}