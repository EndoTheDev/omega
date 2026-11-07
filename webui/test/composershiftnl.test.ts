// omega webui: the composer's trailing-newline law (t_eb07b315). Chromium
// hangs a block-final newline in pre-wrap - the draft carries the break
// but the DOM renders no line box, so one Shift+Enter left the caret glued
// to line 1 (the double-press bug). capTail appends ONE sentinel <br>
// after a draft-final newline and removes it otherwise; the sentinel is
// invisible to the draft reader and the caret arithmetic (non-chip
// elements weigh zero). The real function text is extracted from the SFC
// (brace-matched, the composer.test.ts shape) and run against a fake
// contenteditable. Run: node --test webui/test/ (node >= 23 runs TS).
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const source = readFileSync(
  join(here, "../app/components/chat/AppComposer.vue"),
  "utf8",
);

// brace-match a top-level `function <name>(...)` declaration out of the
// SFC's script text - the extracted text IS the behavior under test
function extractFn(name: string): string {
  const at = source.indexOf(`function ${name}(`);
  assert.ok(at > -1, `AppComposer carries function ${name}`);
  let depth = 0;
  const start = source.indexOf("{", at);
  for (let i = start; i < source.length; i++) {
    if (source[i] === "{") depth++;
    else if (source[i] === "}") {
      depth--;
      if (depth === 0) return source.slice(at, i + 1);
    }
  }
  assert.fail(`unbalanced braces around ${name}`);
}

// --- the fake contenteditable --------------------------------------------
// text node: plain object with nodeType/nodeValue; element: class with a
// dataset and childNodes; our BR sentinel: instantiates the harness's
// brClass - capTail sees it through the injected HTMLBRElement binding.

const TEXT = 3;
class FakeElement {
  nodeType = 1;
  tagName: string;
  dataset: Record<string, string | undefined> = {};
  childNodes: unknown[] = [];
  constructor(tagName: string) {
    this.tagName = tagName;
  }
  get lastChild(): unknown {
    return this.childNodes[this.childNodes.length - 1] ?? null;
  }
  appendChild(n: unknown): void {
    this.childNodes.push(n);
  }
  removeChild(n: unknown): void {
    const i = this.childNodes.indexOf(n);
    assert.ok(i > -1, "removeChild of an absent child");
    this.childNodes.splice(i, 1);
  }
  get previousSibling(): unknown {
    return null; // not needed by capTail; previousSibling walking
    // happens on the brs themselves (set below)
  }
}

class FakeBR extends FakeElement {
  prev: unknown = null;
  constructor() {
    super("BR");
  }
  override get previousSibling(): unknown {
    return this.prev;
  }
}

// wire the sibling chain the way a real DOM does (both directions)
function appendAll(el: FakeElement, ...kids: unknown[]): void {
  for (let i = 0; i < kids.length; i++) {
    const n = kids[i] as FakeBR & { prev?: unknown };
    if (i > 0) n.prev = kids[i - 1];
    el.appendChild(n);
  }
}

const tnode = (value: string) => ({ nodeType: TEXT, nodeValue: value });

// evaluate the real draftTextOf + capTail against the fake DOM world
// (the composer.test.ts shape: strip the TS annotations, eval the raw text)
const draftTextOf = new Function(
  "Node",
  "Element",
  "return " +
    extractFn("draftTextOf").replace(
      /function draftTextOf\(el: HTMLElement\): string/,
      "function draftTextOf(el)",
    ).replace(
      /const walk = \(n: Node\) =>/,
      "const walk = (n) =>",
    ),
)(
  { TEXT_NODE: TEXT },
  FakeElement,
) as (el: HTMLElement) => string;

const capTail = new Function(
  "Node",
  "Element",
  "HTMLBRElement",
  "draftTextOf",
  "document",
  "return " +
    extractFn("capTail").replace(
      /function capTail\(el: HTMLElement, text: string\): void/,
      "function capTail(el, text)",
    ).replace(
      /const brs: Node\[\] = \[\];/,
      "const brs = [];",
    ),
)(
  { TEXT_NODE: TEXT },
  FakeElement,
  FakeBR,
  draftTextOf,
  { createElement: () => new FakeBR() },
) as (el: HTMLElement, text: string) => void;

const kidsOf = (el: FakeElement): string[] =>
  el.childNodes.map((n) =>
    n instanceof FakeBR ? "BR" : JSON.stringify((n as { nodeValue: string }).nodeValue),
  );

describe("composer trailing-newline sentinel (capTail)", () => {
  it("draft 'hello\\n' gains exactly one trailing <br>", () => {
    const el = new FakeElement("DIV");
    appendAll(el, tnode("hello\n"));
    capTail(el as unknown as HTMLElement, "hello\n");
    assert.deepEqual(kidsOf(el), ['"hello\\n"', "BR"]);
  });

  it("the cap is idempotent: a repeat call never doubles the sentinel", () => {
    const el = new FakeElement("DIV");
    appendAll(el, tnode("hello\n"));
    capTail(el as unknown as HTMLElement, "hello\n");
    capTail(el as unknown as HTMLElement, "hello\n");
    assert.deepEqual(kidsOf(el), ['"hello\\n"', "BR"]);
  });

  it("a surplus sentinel run (browser artifacts) collapses back to one", () => {
    const el = new FakeElement("DIV");
    const b1 = new FakeBR();
    const b2 = new FakeBR();
    appendAll(el, tnode("hi\n"), b1, b2);
    capTail(el as unknown as HTMLElement, "hi\n");
    assert.deepEqual(kidsOf(el), ['"hi\\n"', "BR"]);
  });

  it("a draft without a final newline sheds the sentinel entirely", () => {
    const el = new FakeElement("DIV");
    appendAll(el, tnode("hello"));
    capTail(el as unknown as HTMLElement, "hello");
    assert.deepEqual(kidsOf(el), ['"hello"']);
    const el2 = new FakeElement("DIV");
    appendAll(el2, tnode("hello\n"), new FakeBR());
    capTail(el2 as unknown as HTMLElement, "hello");
    assert.deepEqual(kidsOf(el2), ['"hello\\n"']);
  });

  it("an empty editor never keeps a sentinel (:empty placeholder)", () => {
    const el = new FakeElement("DIV");
    appendAll(el, new FakeBR());
    capTail(el as unknown as HTMLElement, "");
    assert.deepEqual(kidsOf(el), []);
  });

  it("a mid-editor newline does not cap", () => {
    const el = new FakeElement("DIV");
    appendAll(el, tnode("a\nb"));
    capTail(el as unknown as HTMLElement, "a\nb");
    assert.deepEqual(kidsOf(el), ['"a\\nb"']);
  });

  it("the sentinel weighs zero in the draft read (draft invariance)", () => {
    const el = new FakeElement("DIV");
    el.childNodes.push(tnode("hello\n"), new FakeBR());
    const withCap = draftTextOf(el as unknown as HTMLElement);
    assert.equal(withCap, "hello\n");
  });

  it("the composer wires capTail into both DOM write paths", () => {
    assert.match(source, /capTail\(el, text\)/, "absorbWrite caps its own read");
    assert.match(
      source,
      /capTail\(el, domText\)/,
      "syncEditor's parity path caps",
    );
    assert.match(
      source,
      /capTail\(el, props\.draft\)/,
      "syncEditor's rebuild path caps",
    );
  });
});
