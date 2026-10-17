// omega webui: unit tests for markdown rendering (renderMarkdown) and the
// streaming render tick (createRenderScheduler) in composables/omega.ts.
// Run: node --test webui/test/ (node >= 23 runs TS natively)
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  createRenderScheduler,
  renderMarkdown,
  RENDER_TICK_MS,
} from "../app/composables/omega.ts";
import type { RenderTimers } from "../app/composables/omega.ts";

describe("renderMarkdown", () => {
  it("plain text becomes a paragraph", () => {
    assert.equal(renderMarkdown("hello"), "<p>hello</p>\n");
  });

  it("inline formatting: strong, em, code, link", () => {
    const html = renderMarkdown("**b** *i* `c` [l](https://example.com)");
    assert.match(html, /<strong>b<\/strong>/);
    assert.match(html, /<em>i<\/em>/);
    assert.match(html, /<code>c<\/code>/);
    assert.match(html, /<a href="https:\/\/example\.com">l<\/a>/);
  });

  it("fenced code block becomes pre/code (go now highlighted)", () => {
    const html = renderMarkdown("```go\nfmt.Println(1)\n```");
    assert.match(html, /<pre><code class="hljs language-go">fmt\.Println\(<span class="hljs-number">1<\/span>\)/);
  });

  it("a partially-received fence still renders as code", () => {
    const html = renderMarkdown("```py\nprint('un");
    assert.match(html, /<pre><code/);
    assert.match(html, /print\('un/);
  });

  it("fenced go block gets hljs token markup", () => {
    const html = renderMarkdown('```go\npackage main\nfunc main() { fmt.Println("hi") }\n```');
    assert.match(html, /<code class="hljs language-go">/);
    assert.match(html, /<span class="hljs-keyword">package<\/span>/);
    assert.match(html, /<span class="hljs-string">&quot;hi&quot;<\/span>/);
  });

  it("a still-open fence of a KNOWN language renders unstyled (streaming)", () => {
    const html = renderMarkdown("```go\nfunc main() {\n");
    assert.match(html, /<pre><code class="language-go">/);
    assert.doesNotMatch(html, /hljs/);
  });

  it("unknown language falls back to plain code, no hljs spans", () => {
    const html = renderMarkdown("```brainfuck\nx\n```");
    assert.match(html, /<pre><code class="language-brainfuck">x\n<\/code><\/pre>/);
    assert.doesNotMatch(html, /hljs/);
  });

  it("highlighting keeps raw html inside the code escaped", () => {
    const html = renderMarkdown("```go\n// <script>alert(1)</script>\n```");
    assert.doesNotMatch(html, /<script>/);
    assert.match(html, /&lt;script&gt;/);
  });

  it("tables render as table markup", () => {
    const html = renderMarkdown("| a | b |\n|---|---|\n| 1 | 2 |");
    assert.match(html, /<table>/);
    assert.match(html, /<th>a<\/th>/);
    assert.match(html, /<td>2<\/td>/);
  });

  it("raw HTML is escaped, not executed", () => {
    const html = renderMarkdown("<script>alert(1)</script>");
    assert.doesNotMatch(html, /<script>/);
    assert.match(html, /&lt;script&gt;/);
  });

  it("event-handler markup never renders as an element", () => {
    const html = renderMarkdown('<img src="x" onerror="alert(1)">');
    // escaped to text by the default renderer - present as text with
    // &lt;/&quot;, never as a real <img> tag
    assert.doesNotMatch(html, /<img/);
    assert.match(html, /&lt;img/);
    assert.match(html, /&quot;alert\(1\)&quot;/);
  });

  it("javascript: hrefs never render as a link", () => {
    // the default validateLink rejects the scheme - the whole token
    // falls back to plain text, no <a> is emitted
    const html = renderMarkdown("[click](javascript:alert(1))");
    assert.doesNotMatch(html, /<a /);
    assert.doesNotMatch(html, /href=/);
  });
});

// A manual fake clock for the debounce tests - injectable timers make
// this a pure deterministic tick instead of node mock.timers.
function fakeClock() {
  const pending: { fn: () => void; due: number; handle: symbol }[] = [];
  let now = 0;
  return {
    timers: {
      set: (fn: () => void, ms: number) => {
        const handle = Symbol();
        pending.push({ fn, due: now + ms, handle });
        return handle;
      },
      clear: (handle: unknown) => {
        const i = pending.findIndex((p) => p.handle === handle);
        if (i >= 0) pending.splice(i, 1);
      },
    } as RenderTimers,
    tick(ms: number) {
      now += ms;
      for (const p of [...pending]) {
        if (p.due <= now) {
          pending.splice(pending.indexOf(p), 1);
          p.fn();
        }
      }
    },
  };
}

describe("createRenderScheduler (debounced streaming render)", () => {
  it("coalesces chunk bursts into one render per tick, latest source", () => {
    const clock = fakeClock();
    const renders: string[] = [];
    const sched = createRenderScheduler(
      (src) => renders.push(src),
      RENDER_TICK_MS,
      clock.timers,
    );
    sched.schedule("partial");
    sched.schedule("partial code");
    sched.schedule("partial code, table");
    assert.equal(renders.length, 0, "nothing renders inside the window");
    clock.tick(RENDER_TICK_MS);
    assert.deepEqual(renders, ["partial code, table"]);
  });

  it("two ticks apart render twice - long streams keep advancing", () => {
    const clock = fakeClock();
    const renders: string[] = [];
    const sched = createRenderScheduler(
      (src) => renders.push(src),
      RENDER_TICK_MS,
      clock.timers,
    );
    sched.schedule("first");
    clock.tick(RENDER_TICK_MS);
    sched.schedule("second");
    clock.tick(RENDER_TICK_MS);
    assert.deepEqual(renders, ["first", "second"]);
  });

  it("cancel drops the pending render", () => {
    const clock = fakeClock();
    const renders: string[] = [];
    const sched = createRenderScheduler(
      (src) => renders.push(src),
      RENDER_TICK_MS,
      clock.timers,
    );
    sched.schedule("mid-stream");
    sched.cancel();
    clock.tick(RENDER_TICK_MS * 10);
    assert.deepEqual(renders, []);
  });

  it("cancel is idempotent and a schedule after cancel re-arms", () => {
    const clock = fakeClock();
    const renders: string[] = [];
    const sched = createRenderScheduler(
      (src) => renders.push(src),
      RENDER_TICK_MS,
      clock.timers,
    );
    sched.schedule("x");
    sched.cancel();
    sched.cancel();
    clock.tick(RENDER_TICK_MS);
    assert.deepEqual(renders, []);
    sched.schedule("y");
    clock.tick(RENDER_TICK_MS);
    assert.deepEqual(renders, ["y"]);
  });

  it("custom interval is honored", () => {
    const clock = fakeClock();
    const renders: string[] = [];
    const sched = createRenderScheduler(
      (src) => renders.push(src),
      300,
      clock.timers,
    );
    sched.schedule("late");
    clock.tick(299);
    assert.equal(renders.length, 0);
    clock.tick(1);
    assert.deepEqual(renders, ["late"]);
  });
});