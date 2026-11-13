// omega webui: unit tests for the ONE copy path (composables/clipboard.ts,
// card t_a92450ea). Run: node --test webui/test/ (node 23+ runs TS natively).
// The helper's two paths are exercised against hand-rolled globals:
// navigator.clipboard in secure contexts, document.execCommand("copy") in
// insecure ones - the plain-http lane's shape.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { fileURLToPath, URL } from "node:url";

const root = fileURLToPath(new URL("..", import.meta.url));
const srcFile = (rel: string): string => readFileSync(`${root}/${rel}`, "utf8");

// node 21+ installs navigator as a getter on globalThis - defineProperty
// (configurable) is the only installable shape
function installNavigator(value: any) {
  Object.defineProperty(globalThis, "navigator", {
    value,
    configurable: true,
  });
}
function uninstallGlobals() {
  for (const k of ["navigator", "document"]) {
    try {
      Object.defineProperty(globalThis, k, { value: undefined, configurable: true });
      delete (globalThis as any)[k];
    } catch {
      // the global stays - next stub's defineProperty still wins
    }
  }
}

// stub the DOM bits execCommandCopy needs (append/remove/select/
// setSelectionRange on the fake textarea all no-op - only the verdict
// flow is under test, not real browser copy semantics)
function stubGlobals(opts: {
  clipboardWrite?: (text: string) => Promise<void>;
  clipboardPresent?: boolean;
  execCommand?: (cmd: string) => boolean;
}) {
  const calls: {
    created: number;
    removed: number;
    commands: string[];
    // card t_61cc8762: the value the fallback textarea actually held -
    // the insecure lane's payload receipt
    fallbackText: () => string;
  } = {
    created: 0,
    removed: 0,
    commands: [],
    fallbackText: () => "",
  };
  const ta = {
    value: "",
    select() {},
    setSelectionRange() {},
  };
  installNavigator(opts.clipboardPresent === false
    ? {}
    : {
        clipboard:
          opts.clipboardWrite === undefined
            ? undefined
            : { writeText: opts.clipboardWrite },
      });
  (globalThis as any).document = {
    body: {
      appendChild() {},
      removeChild() {},
    },
    activeElement: null,
    createElement() {
      calls.created++;
      return ta;
    },
    documentElement: {},
    execCommand(cmd: string) {
      calls.commands.push(cmd);
      return opts.execCommand ? opts.execCommand(cmd) : false;
    },
    queryCommandSupported() {
      return true;
    },
  };
  // execCommandCopy reads getAttribute/setAttribute/style on the node
  (ta as any).setAttribute = () => {};
  (ta as any).getAttribute = () => null;
  (ta as any).remove = () => {
    calls.removed++;
  };
  (ta as any).style = {};
  calls.fallbackText = () => ta.value;
  return calls;
}

import { copyText } from "../app/composables/clipboard.ts";

describe("copyText: the secure path", () => {
  it("navigator.clipboard.writeText resolving returns true", async () => {
    let got: string | null = null;
    const calls = stubGlobals({
      clipboardWrite: (t) => {
        got = t;
        return Promise.resolve();
      },
    });
    try {
      assert.equal(await copyText("hello"), true);
      assert.equal(got, "hello");
      assert.equal(calls.created, 0); // no textarea fallback needed
    } finally {
      uninstallGlobals();
    }
  });

  it("writeText rejecting falls through to execCommand", async () => {
    const calls = stubGlobals({
      clipboardWrite: () => Promise.reject(new Error("denied")),
      execCommand: (c) => c === "copy",
    });
    try {
      assert.equal(await copyText("hello"), true);
      assert.equal(calls.created, 1);
      assert.deepEqual(calls.commands, ["copy"]);
    } finally {
      uninstallGlobals();
    }
  });
});

describe("copyText: the insecure path (plain-http lane)", () => {
  it("navigator without clipboard rides execCommand", async () => {
    const calls = stubGlobals({ execCommand: (c) => c === "copy" });
    try {
      assert.equal(await copyText("payload"), true);
      assert.deepEqual(calls.commands, ["copy"]);
      assert.equal(calls.removed, 1);
    } finally {
      uninstallGlobals();
    }
  });

  it("execCommand false returns false - never a fake copied", async () => {
    const calls = stubGlobals({ execCommand: () => false });
    try {
      assert.equal(await copyText("payload"), false);
      assert.deepEqual(calls.commands, ["copy"]);
    } finally {
      uninstallGlobals();
    }
  });

  it("execCommand throwing returns false", async () => {
    const calls = stubGlobals({
      execCommand: () => {
        throw new Error("boom");
      },
    });
    try {
      assert.equal(await copyText("payload"), false);
      assert.deepEqual(calls.commands, ["copy"]);
    } finally {
      uninstallGlobals();
    }
  });

  it("no document at all (SSR shape) is false, not a crash", async () => {
    installNavigator({});
    delete (globalThis as any).document;
    try {
      assert.equal(await copyText("payload"), false);
    } finally {
      uninstallGlobals();
    }
  });
});

// --- card t_61cc8762: the sessions row's copy-id affordance ---------------
// The row action lives in module UI (harness/store/ui/AppSidebar.vue) and
// this suite has no DOM, so the contract is pinned in two halves: the
// helper writes the EXACT id it is handed (behavior, both copy paths), and
// the SFC call site routes through the composable with a verdict-branched
// toast (shape). The browser witness is journey 115-session-copy-id (real
// hover, real click, the live toast stack).

const SESSION_ID = "jrn-13nov213000-copy-id";

describe("copyText: a session id is the payload, verbatim", () => {
  it("secure path writes the id the row hands over", async () => {
    let got: string | null = null;
    const calls = stubGlobals({
      clipboardWrite: (t) => {
        got = t;
        return Promise.resolve();
      },
    });
    try {
      assert.equal(await copyText(SESSION_ID), true);
      assert.equal(got, SESSION_ID);
      assert.equal(calls.created, 0, "no textarea fallback on the secure lane");
    } finally {
      uninstallGlobals();
    }
  });

  it("insecure path writes the same id through the textarea fallback", async () => {
    const calls = stubGlobals({ execCommand: (c) => c === "copy" });
    try {
      assert.equal(await copyText(SESSION_ID), true);
      assert.equal(calls.fallbackText(), SESSION_ID, "the selection IS the id");
      assert.deepEqual(calls.commands, ["copy"]);
    } finally {
      uninstallGlobals();
    }
  });
});

describe("the sessions row's copy-id action (card t_61cc8762)", () => {
  const sidebar = srcFile("../harness/store/ui/AppSidebar.vue");

  it("routes the click through the ONE copy path", () => {
    assert.match(sidebar, /import \{ copyText \} from "~\/composables\/clipboard"/);
    assert.match(sidebar, /const ok = await copyText\(id\)/, "the verdict is awaited, never voided");
    assert.doesNotMatch(sidebar, /navigator\.clipboard/, "no direct clipboard call outside the composable");
    assert.doesNotMatch(sidebar, /document\.execCommand/, "no second fallback implementation");
  });

  it("renders the button as a hover-revealed row action that never opens the row", () => {
    assert.match(sidebar, /class="icon-btn row-act row-copy-id"/);
    assert.match(sidebar, /aria-label="Copy session id"/);
    assert.match(sidebar, /title="Copy session id"/);
    assert.match(sidebar, /@click\.stop="copySessionId\(s\.id\)"/, ".stop: a copy never opens the session");
    // the cluster is what hover reveals (main.css: .session-list li:hover
    // .row-actions) and what reserves the time slot - a button outside it
    // would be always-visible chrome the row layout never carried
    const clusterAt = sidebar.indexOf('class="row-actions"');
    const copyAt = sidebar.indexOf('class="icon-btn row-act row-copy-id"');
    const clusterEnd = sidebar.indexOf("</span>", clusterAt);
    assert.ok(clusterAt !== -1 && copyAt !== -1, "both landmarks exist");
    assert.ok(clusterAt < copyAt && copyAt < clusterEnd, "the copy button sits inside the hover cluster");
    const css = srcFile("app/assets/css/main.css");
    assert.match(
      css,
      /\.session-list li:hover \.row-actions,\s*\n\.row-actions:focus-within \{ opacity: 1; pointer-events: auto; \}/,
      "the cluster stays hover/focus revealed only",
    );
  });

  it("confirms on the shell's local toast funnel - no second toast stack", () => {
    assert.match(sidebar, /import \{ emitLocalToast \} from "~\/composables\/canvas"/);
    assert.equal((sidebar.match(/emitLocalToast\(/g) ?? []).length, 1, "one emit, the shared funnel");
    // the payload is the record shape the funnel's consumer decodes
    // (notificationFeed decodeToast: numeric id, kind task|cron|chat) -
    // kind "chat" with a NON-session source_id, so a toast click opens
    // nothing (app.vue's openNotification matches source_id against the
    // session list and no-ops on a miss)
    assert.match(sidebar, /kind: "chat"/);
    assert.match(sidebar, /source_id: "session-copy"/);
    assert.match(sidebar, /read: true/, "a copy receipt never counts as unread");
    // distinct fingerprints: the funnel dedupes identical payloads, so a
    // repeated copy of the SAME id must carry a fresh timestamp or the
    // second toast never fires
    assert.match(sidebar, /created_at: new Date\(\)\.toISOString\(\)/);
    // the confirmation follows the awaited verdict - a failed copy never
    // reads as "copied"
    assert.match(sidebar, /verdict: ok \? "ok" : "failed"/);
    assert.match(sidebar, /title: ok \? `copied session id \$\{id\}` : "copy failed/);
    assert.doesNotMatch(sidebar, /toasts?\s*=\s*ref/, "no private toast stack");
  });
});

// the copy-path law in one scan: the composable is the ONLY source naming
// the clipboard API (card t_a92450ea's "no source outside that file" rule).
// Comment text counts too - a copy-pasted snippet fails here before it can
// lie in an insecure context.
describe("the copy-path law: one file names the clipboard API", () => {
  function walk(dir: string): string[] {
    const out: string[] = [];
    for (const name of readdirSync(dir)) {
      const p = `${dir}/${name}`;
      if (statSync(p).isDirectory()) out.push(...walk(p));
      else if (p.endsWith(".ts") || p.endsWith(".vue")) out.push(p);
    }
    return out;
  }

  it("no SPA source outside app/composables/clipboard.ts names it", () => {
    const files = [...walk(`${root}/app`), ...walk(`${root}/../harness`)];
    // assembled so this scan's own needle is never a literal here
    const needle = ["navigator", "clipboard"].join(".");
    const offenders = files
      .filter((f) => readFileSync(f, "utf8").includes(needle))
      .filter((f) => !f.endsWith("app/composables/clipboard.ts"))
      .map((f) => f.slice(root.length - 1));
    assert.ok(files.length > 100, "the walk reached the SPA source");
    assert.deepEqual(offenders, [], "the ONE copy path is the only place");
  });
});
