// omega webui: the FILES tab's re-root ordering. The pane's ONLY change signal is the
// focused session row's workspace, and every root comes from the server (the client
// sends a session id, never a root). So a row write IS a browse read: the door that
// switches workspaces must write the row AFTER the store holds the new value. A flip
// sent before the PATCH answers fires the pane's one read against the old store row -
// the pane renders the old root, and the confirmed list write that follows lands the
// value the row already shows, so no second read ever fires and the stale tree stays
// until a reload.
//
// No DOM, no SFC compile: the pane's loader is mirrored below (root round + the
// latest-wins guard of FilesTab.vue:168-189) and the switch door is the REAL one.
// Run: node --test test/filesworkspaceroot.test.ts
import { describe, it, afterEach } from "node:test";
import assert from "node:assert/strict";
import { computed, ref, watch, type Ref } from "vue";

import { fetchFilesBrowse, fetchSessions, setSessionContext } from "../app/composables/omega.ts";
import { latestWins } from "../app/composables/latestfetch.ts";

const SID = "sess-1";
const A = "/home/endo/theirs";
const B = "/home/endo/mine";
const T = "2026-11-28T00:00:00Z";

interface Row {
  id: string;
  workspace?: string;
  created_at: string;
  updated_at: string;
}

let savedFetch: typeof globalThis.fetch;
afterEach(() => {
  globalThis.fetch = savedFetch;
});

const json = (body: unknown): Response =>
  new Response(JSON.stringify(body), { status: 200 });

interface StoreOpts {
  /** the store's own resolution of a picked path (EvalSymlinks, a trailing slash) */
  normalize?: (picked: string) => string;
  /** the store's rejection (a path it will not root on) */
  refuse?: (picked: string) => boolean;
}

// the server half of this card: /files/browse answers the root the STORE holds when
// the request arrives, so a read's answer is a fact about ordering, not about caching.
function installStore(start: string, opts: StoreOpts = {}) {
  const store = { workspace: start, browsed: [] as string[], patches: 0 };
  let open!: () => void;
  const gate = new Promise<void>((r) => (open = r));
  savedFetch = globalThis.fetch;
  globalThis.fetch = (async (url: RequestInfo | URL, init?: RequestInit) => {
    const u = String(url);
    const method = init?.method ?? "GET";
    if (u.startsWith("/files/browse")) {
      store.browsed.push(store.workspace);
      return json({ entries: [], root: store.workspace, source: "session" });
    }
    if (u.startsWith("/sessions?") && method === "GET") {
      return json([{ id: SID, workspace: store.workspace, created_at: T, updated_at: T }]);
    }
    if (u === `/sessions/${SID}` && method === "PATCH") {
      store.patches++;
      await gate;
      const body = JSON.parse(String(init?.body ?? "{}")) as { workspace?: string };
      const picked = body.workspace ?? "";
      if (opts.refuse?.(picked)) return new Response("not a directory", { status: 400 });
      store.workspace = opts.normalize ? opts.normalize(picked) : picked;
      return json({ id: SID, workspace: store.workspace, created_at: T, updated_at: T });
    }
    throw new Error(`unexpected ${method} ${u}`);
  }) as typeof globalThis.fetch;
  return { store, release: () => open() };
}

interface Pane {
  root: Ref<string>;
  reads: Ref<number>;
  settle: () => Promise<void>;
}

// FilesTab.vue's loader, mirrored: the boot read at setup, then one read per row
// change, with the latest-requested round as the only writer (latestfetch.ts).
function mountPane(sessions: Ref<Row[]>, focused: Ref<string | undefined>): Pane {
  const root = ref("");
  const reads = ref(0);
  const rounds = latestWins();
  const inflight: Promise<void>[] = [];
  async function loadRoot() {
    const admitted = rounds.begin();
    reads.value++;
    const data = await fetchFilesBrowse("", focused.value);
    if (!admitted()) return; // a newer root request owns the state
    root.value = data.root ?? "";
  }
  const rowWorkspace = computed(() => {
    const id = focused.value;
    return id ? (sessions.value.find((s) => s.id === id)?.workspace ?? null) : null;
  });
  watch(
    [focused, rowWorkspace],
    () => {
      inflight.push(loadRoot());
    },
    { flush: "sync" },
  );
  inflight.push(loadRoot()); // the mount read
  return {
    root,
    reads,
    settle: async () => {
      for (let i = 0; i < 10; i++) {
        await Promise.all(inflight);
        await new Promise((r) => setTimeout(r, 0));
      }
    },
  };
}

function mountSession(workspace: string) {
  const sessions = ref<Row[]>([{ id: SID, workspace, created_at: T, updated_at: T }]);
  const focused = ref<string | undefined>(SID);
  return { sessions, focused, pane: mountPane(sessions, focused) };
}

// app.vue's row view: refreshSessions replaces the whole list with the server's answer.
async function refreshSessions(sessions: Ref<Row[]>) {
  sessions.value = await fetchSessions();
}

describe("the pane's one read is answered from the store at request time", () => {
  it("a read issued before the PATCH answers renders the OLD root, and never re-fires", async () => {
    const { store, release } = installStore(A);
    const { sessions, pane } = mountSession(A);
    await pane.settle();
    assert.equal(pane.root.value, A, "boot read");
    const readsBefore = pane.reads.value;

    // the flip-then-PATCH door: the row moves first, the store answers later.
    sessions.value[0]!.workspace = B;
    const patched = setSessionContext(SID, undefined, B);
    release();
    await patched;
    await refreshSessions(sessions);
    await pane.settle();

    assert.equal(store.workspace, B, "the store holds B");
    assert.equal(pane.reads.value - readsBefore, 1, "the flip spent the ONE read");
    assert.equal(pane.root.value, A, "the read was answered from the old row and sticks");
    assert.deepEqual(store.browsed, [A, A], "both reads were served by the store's A");
  });
});

describe("the switch door writes the row after the store", () => {
  it("selecting B re-roots the pane on B in one read", async () => {
    const { store, release } = installStore(A);
    const { sessions, pane } = mountSession(A);
    await pane.settle();
    const readsBefore = pane.reads.value;

    const row = sessions.value[0]!;
    row.workspace = B; // app.vue's optimistic flip, before the write
    const switched = setSessionContext(SID, undefined, B).then(() => refreshSessions(sessions));
    release();
    await switched;
    await pane.settle();

    assert.equal(store.workspace, B);
    assert.equal(pane.reads.value - readsBefore, 1, "one read per switch");
    assert.equal(pane.root.value, B);
    assert.deepEqual(store.browsed, [A, B]);
  });
});
