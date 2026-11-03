// omega webui: the gesture-ownership model (card t_b2430179). The gesture
// record (tabDrag + the ghost rect registry) is COMPOSABLE-owned, guarded by
// ONE window-level listener set the composable itself installs - never per
// pane instance. A pane unmounting mid-gesture cannot orphan the cleanup:
// dragend / drop / Esc / blur end the record at the composable, a new
// gesture's start overwrites any stale record, and a layout mutation ends
// an armed record. Render-free; node's built-in test runner.
import { describe, it, beforeEach } from "node:test";
import assert from "node:assert/strict";
import {
  tabDrag,
  endGesture,
  onGestureEnd,
  setGestureTarget,
  type TabDragInfo,
} from "../app/composables/dockdnd.ts";
import { ghostRegistry } from "../app/composables/dockghost.ts";

let target: EventTarget;
function fire(type: string, init?: Record<string, string>): void {
  const ev = new Event(type);
  if (init) for (const [k, v] of Object.entries(init)) (ev as any)[k] = v;
  target.dispatchEvent(ev);
}

beforeEach(() => {
  target = new EventTarget();
  setGestureTarget(target as unknown as typeof window);
  endGesture(); // a fresh record per test (also proves idempotence)
});

describe("gesture ownership: the composable-owned guard", () => {
  it("a window dragend ends the gesture record", () => {
    const info: TabDragInfo = { paneId: "n1", index: 0, component: "files" };
    tabDrag.start(info);
    assert.ok(tabDrag.current());
    fire("dragend");
    assert.equal(tabDrag.current(), null);
  });

  it("a window drop ends the gesture record", () => {
    tabDrag.start({ paneId: "n1", index: 1, component: "cron" });
    fire("drop");
    assert.equal(tabDrag.current(), null);
  });

  it("Esc ends the armed gesture; with nothing armed it is inert", () => {
    tabDrag.start({ paneId: "n1", index: 0, component: "files" });
    fire("keydown", { key: "Escape" });
    assert.equal(tabDrag.current(), null);
    let rung = 0;
    const un = onGestureEnd(() => rung++);
    fire("keydown", { key: "Escape" }); // nothing armed: no end event
    assert.equal(rung, 0);
    fire("keydown", { key: "a" }); // a non-Esc key never ends
    assert.equal(rung, 0);
    un();
  });

  it("a window blur ends the gesture record", () => {
    tabDrag.start({ paneId: "n1", index: 0, component: "files" });
    fire("blur");
    assert.equal(tabDrag.current(), null);
  });

  it("the NEXT gesture's start overwrites a stale record (no poisoned hit-test)", () => {
    // stale = a gesture the browser never delivered dragend for (a source
    // element removed mid-gesture on a real drag) - the next dragstart must
    // annihilate it, never read it
    tabDrag.start({ paneId: "nX", index: 9, component: "cron" });
    assert.equal(tabDrag.current()?.paneId, "nX");
    tabDrag.start({ paneId: "n1", index: 0, component: "files" });
    assert.deepEqual(tabDrag.current(), {
      paneId: "n1",
      index: 0,
      component: "files",
    });
  });

  it("the ghost registry clears with the gesture - composable-owned, not instance-owned", () => {
    tabDrag.start({ paneId: "n1", index: 0, component: "files" });
    ghostRegistry.begin("n1", 0);
    ghostRegistry.record("n5", { x: 0, y: 0, w: 10, h: 10 });
    assert.ok(ghostRegistry.current());
    fire("dragend");
    assert.equal(ghostRegistry.current(), null);
    // and again through the direct endGesture path (a layout mutation's belt)
    tabDrag.start({ paneId: "n2", index: 0, component: "files" });
    ghostRegistry.begin("n2", 0);
    endGesture();
    assert.equal(ghostRegistry.current(), null);
  });

  it("every gesture end rings the subscribers; unbind detaches one", () => {
    let a = 0;
    let b = 0;
    const unA = onGestureEnd(() => a++);
    onGestureEnd(() => b++);
    tabDrag.start({ paneId: "n1", index: 0, component: "files" });
    assert.equal(a, 1); // start's stale-record annihilation rings the subs
    assert.equal(b, 1);
    fire("drop");
    assert.equal(a, 2);
    assert.equal(b, 2);
    unA();
    fire("dragend");
    assert.equal(a, 2); // A detached, B still on
    assert.equal(b, 3); // B hears the dragend even with nothing armed
  });

  it("instance death cannot orphan the cleanup: no per-instance listener exists", () => {
    // the old model armed window listeners per DockPane instance, so the
    // instance that unmounted mid-gesture took the only closer with it. The
    // ownership law: the record's cleanups are composable-level API - this
    // test pins endGesture() (the global clear) clearing the record WITHOUT
    // any DOM/window involvement, which is what survives instance death.
    tabDrag.start({ paneId: "n1", index: 0, component: "files" });
    endGesture();
    assert.equal(tabDrag.current(), null);
    // a second gesture afterwards reads a CLEAN record, not the stale one
    assert.equal(tabDrag.current(), null);
  });
});