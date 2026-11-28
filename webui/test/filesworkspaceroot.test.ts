// omega webui: the FILES tab's re-root ordering. The pane's only change signal is the
// focused session row's workspace, and its root always comes from the server (it sends a
// session id, never a root). So a row write IS a browse read: the switch door must write
// the row only after the store holds the new value, or the pane's one read is answered
// from the old row and the stale tree sticks for the page's life.
//
// No SFC compile: the pane's loader is mirrored below (root round + the latest-wins
// guard) and the switch door is the real composable path app.vue calls.
// Run: node --test test/filesworkspaceroot.test.ts
import { describe, it, afterEach } from "node:test";
import assert from "node:assert/strict";
import { computed, ref, watch, type Ref } from "vue";

import { switchWorkspace } from "../app/composables/chat.ts";
import { fetchFilesBrowse, fetchSessions, setSessionContext } from "../app/composables/omega.ts";
import { latestWins } from "../app/composables/latestfetch.ts";

const SID = "sess-1";
const A = "/home/endo/theirs";
const B = "/home/endo/mine";
const FALLBACK = "/home/endo/fallback";
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

// the server half: /files/browse answers the root the STORE holds when the request
// arrives, so a read's answer is a fact about ordering, not about caching.
function installStore(start: string, opts: StoreOpts = {}) {
  const store = {
    workspace: start,
    browsed: [] as string[],
    picked: [] as string[],
    patches: 0,
  };
  let open!: () => void;
  const gate = new Promise<void>((r) => (open = r));
  savedFetch = globalThis.fetch;
  globalThis.fetch = (async (url: RequestInfo | URL, init?: RequestInit) => {
    const u = String(url);
    const method = init?.method ?? "GET";
    if (u.startsWith("/files/browse")) {
      const root = store.workspace || FALLBACK; // an unpinned row resolves the default
      store.browsed.push(root);
      return json({ entries: [], root, source: store.workspace ? "session" : "default" });
    }
    if (u.startsWith("/sessions?") && method === "GET") {
      return json([{ id: SID, workspace: store.workspace, created_at: T, updated_at: T }]);
    }
    if (u === `/sessions/${SID}` && method === "PATCH") {
      store.patches++;
      await gate;
      const body = JSON.parse(String(init?.body ?? "{}")) as { workspace?: string };
      const picked = body.workspace ?? "";
      store.picked.push(picked);
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
  sessions.value = (await fetchSessions()) as Row[];
}

const tick = () => new Promise((r) => setTimeout(r, 0));

// app.vue's one switch door: the composable path, the store's PATCH, the list read.
function switchDoor(sessions: Ref<Row[]>, workspace: string) {
  return switchWorkspace(sessions.value[0]!, workspace, {
    patch: (id, ws) => setSessionContext(id, undefined, ws),
    refresh: () => refreshSessions(sessions),
  });
}

describe("selecting a workspace re-roots the pane", () => {
  it("switching to B re-roots the pane on B in one read", async () => {
    const { store, release } = installStore(A);
    const { sessions, pane } = mountSession(A);
    await pane.settle();
    assert.equal(pane.root.value, A, "boot read");
    const readsBefore = pane.reads.value;

    const switched = switchDoor(sessions, B);
    await tick();
    // the store owns the row until the PATCH answers: no read, no premature root
    assert.equal(pane.root.value, A, "the pane keeps the store's root mid-flight");
    assert.equal(pane.reads.value, readsBefore, "no read before the store answers");

    release();
    assert.equal(await switched, true);
    await pane.settle();

    assert.equal(store.workspace, B);
    assert.equal(pane.reads.value - readsBefore, 1, "one read per switch");
    assert.equal(pane.root.value, B, "the pane roots on the confirmed workspace");
    assert.deepEqual(store.browsed, [A, B], "the read lands after the store moved");
  });

  it("the flip-then-PATCH ordering spends the one read on the old root", async () => {
    // the pre-fix door, kept as the reproduction: the row moves first, so the watch's
    // read is answered from the old store row and the confirmed list write that lands
    // later equals what the row already shows - the watch never fires again.
    const { store, release } = installStore(A);
    const { sessions, pane } = mountSession(A);
    await pane.settle();
    const readsBefore = pane.reads.value;

    sessions.value[0]!.workspace = B;
    const patched = setSessionContext(SID, undefined, B).then(() => refreshSessions(sessions));
    release();
    await patched;
    await pane.settle();

    assert.equal(store.workspace, B, "the store holds B");
    assert.equal(pane.reads.value - readsBefore, 1, "the flip spent the ONE read");
    assert.equal(pane.root.value, A, "the read was answered from the old row and sticks");
    assert.deepEqual(store.browsed, [A, A], "both reads were served by the store's A");
  });

  it("a refused switch leaves the pane on the store's root and spends no read", async () => {
    const { store, release } = installStore(A, { refuse: (p) => p === B });
    const { sessions, pane } = mountSession(A);
    await pane.settle();
    const readsBefore = pane.reads.value;

    const switched = switchDoor(sessions, B);
    release();
    assert.equal(await switched, false, "the store refused B");
    await pane.settle();

    assert.equal(store.workspace, A);
    assert.equal(sessions.value[0]!.workspace, A, "the row keeps the server's value");
    assert.equal(pane.root.value, A);
    assert.equal(pane.reads.value, readsBefore, "a refused switch re-roots nothing");
  });

  it("the pane roots on the workspace the store confirmed, not the picked string", async () => {
    const picked = `${B}/`;
    const { store, release } = installStore(A, {
      normalize: (p) => p.replace(/\/+$/, ""),
    });
    const { sessions, pane } = mountSession(A);
    await pane.settle();
    const readsBefore = pane.reads.value;

    const switched = switchDoor(sessions, picked);
    release();
    assert.equal(await switched, true);
    await pane.settle();

    assert.deepEqual(store.picked, [picked], "the client sent its pick as-is");
    assert.equal(sessions.value[0]!.workspace, B, "the row carries the store's answer");
    assert.equal(pane.root.value, B);
    assert.equal(pane.reads.value - readsBefore, 1);
  });

  it("picking the workspace the session already holds writes nothing", async () => {
    const { store, release } = installStore(A);
    const { sessions, pane } = mountSession(A);
    await pane.settle();
    const readsBefore = pane.reads.value;

    assert.equal(await switchDoor(sessions, A), false);
    release();
    await pane.settle();

    assert.equal(store.patches, 0, "no PATCH for a no-op pick");
    assert.equal(pane.reads.value, readsBefore);
    assert.equal(pane.root.value, A);
  });

  it("clearing the pick re-roots the pane on the resolved fallback in one read", async () => {
    const { store, release } = installStore(A);
    const { sessions, pane } = mountSession(A);
    await pane.settle();
    const readsBefore = pane.reads.value;

    const switched = switchDoor(sessions, "");
    release();
    assert.equal(await switched, true, "the empty target is a real PATCH");
    await pane.settle();

    assert.deepEqual(store.picked, [""], "an empty workspace clears the pin");
    assert.equal(sessions.value[0]!.workspace, "", "the row follows the store");
    assert.equal(pane.root.value, FALLBACK);
    assert.equal(pane.reads.value - readsBefore, 1, "one read per switch");
  });
});
