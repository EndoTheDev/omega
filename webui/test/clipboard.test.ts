// omega webui: unit tests for the ONE copy path (composables/clipboard.ts,
// card t_a92450ea). Run: node --test webui/test/ (node 23+ runs TS natively).
// The helper's two paths are exercised against hand-rolled globals:
// navigator.clipboard in secure contexts, document.execCommand("copy") in
// insecure ones - the plain-http lane's shape.
import { describe, it } from "node:test";
import assert from "node:assert/strict";

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
  const calls: { created: number; removed: number; commands: string[] } = {
    created: 0,
    removed: 0,
    commands: [],
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
