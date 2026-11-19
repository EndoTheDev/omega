// omega webui: the file tree store. Error law: a failed children request NEVER renders
// backend error text as a tree row - internal vocabulary does not leak into the tree
// body. The failed folder is keyed by ITS OWN path (the path that issued the request,
// never a sibling's - the batched expansion-restore fires one request per folder), a
// later successful children response for a folder clears its mark, and a refresh clears
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

// the failure hint's fixed payload shape.
const FAILURE_TOAST = {
  id: 0,
  kind: "chat",
  source_id: "files-tree",
  title: "folder failed to load",
  verdict: "ok",
  read: true,
} as const;

export function createFileTreeStore(opts?: {
  browse?: typeof fetchFilesBrowse;
  session?: () => string | undefined;
  seed?: string[];
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
  // keyed by the folder path that ISSUED the failed request - the per-folder law a
  // sibling's failure must never cross
  const failed: Record<string, string> = reactive({});

  // ensureLoaded fetches one directory level for an expanded branch.
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

  // persisted expanded set (FILETREE_STATE_KEY). The injected seeds load FIRST and
  // independently of storage health - a blocked localStorage must never eat the
  // caller's own state.
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

  for (const p of [...expanded]) ensureLoaded(p);

  // The FilesTab toolbar's seam: the tab cannot reach this store, it talks through. The
  // STORE owns these (the component never double-applies - createFileTreeStore runs
  // once per tree). Watchers read the bus fields, they never write: the bus stays a
  // one-way seam.
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