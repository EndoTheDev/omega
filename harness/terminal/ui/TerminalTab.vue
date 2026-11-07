<template>
  <!-- card t_55a3fc4b: the webui Terminal tab - the integrated shell,
       VS Code-style with zero restrictions (the owner's ruling; no
       gate, no enable step). card t_617c2ad0: THE TERMINAL CAPSULE'S
       OWN view (harness/terminal/ui/) - the shell's dock outlet binds
       it from module-tabs.generated.ts; the registry only opens the
       tab. One terminal per pane: the pane's session is
       claimed on mount (the terminal module's POST /terminal/sessions
       reattaches to the pane's last shell when one idles - the dock
       unmounts this outlet on sibling activation and the shell keeps
       running server-side). The output is SSE from
       /terminal/{id}/stream (the replay tail first), the keystrokes
       POST /terminal/{id}/input, the pane size POST /terminal/{id}/resize. -->
  <section class="terminal-pane">
    <div class="terminal-toolbar">
      <span class="terminal-state" :class="{ dead }">{{ stateLine }}</span>
      <span class="terminal-tools">
        <button
          v-if="dead"
          class="terminal-btn"
          aria-label="restart terminal"
          title="New shell"
          @click="open">
          <Icon name="refresh" />
        </button>
        <button
          v-else
          class="terminal-btn"
          aria-label="kill terminal"
          title="Kill shell"
          @click="kill">
          <Icon name="trash" />
        </button>
      </span>
    </div>
    <div ref="host" class="terminal-host"></div>
    <p v-if="errorLine" class="terminal-error">{{ errorLine }}</p>
  </section>
</template>

<script setup lang="ts">
  import { onBeforeUnmount, onMounted, ref, shallowRef } from "vue";
  import "@xterm/xterm/css/xterm.css";
  import { Terminal } from "@xterm/xterm";
  import { FitAddon } from "@xterm/addon-fit";
  import type { ITheme } from "@xterm/xterm";

  // the module routes (harness/terminal): claim, stream, input, resize,
  // kill. The ids are server-minted hex - encodeURIComponent is the
  // honest form even for them.
  const CLAIM = "/terminal/sessions";

  const host = ref<HTMLElement | null>(null);
  const term = shallowRef<Terminal | null>(null);
  const fit = shallowRef<FitAddon | null>(null);
  const sessionId = ref("");
  const dead = ref(true);
  const errorLine = ref("");
  const stateLine = ref("connecting...");
  const killed = ref(false);

  interface WireFrame {
    t: "o" | "x" | "m";
    d?: string;
    c?: number;
  }

  let source: EventSource | null = null;
  let observer: ResizeObserver | null = null;
  let connectSeq = 0; // the stale-claim guard (the auto-name gen law)

  function atobBytes(b64: string | undefined): Uint8Array {
    const raw = atob(b64 ?? "");
    const out = new Uint8Array(raw.length);
    for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
    return out;
  }

  function strBytes(s: string): Uint8Array {
    const out = new Uint8Array(s.length);
    for (let i = 0; i < s.length; i++) out[i] = s.charCodeAt(i) & 0xff;
    return out;
  }

  /** the pane's xterm palette reads the shell's own tokens - the
      theme tokens (main.css) ARE the terminal theme. */
  function termTheme(): ITheme {
    const cs = getComputedStyle(document.documentElement);
    const tok = (n: string, fallback: string) =>
      cs.getPropertyValue(n).trim() || fallback;
    return {
      background: tok("--z1", "#1a1a1c"),
      foreground: tok("--text", "#e6e6e9"),
      cursor: tok("--accent", "#E8833A"),
      selectionBackground: tok("--z2", "#26262a"),
    };
  }

  function closeStream() {
    if (source) {
      source.close();
      source = null;
    }
  }

  /** claim the pane's session (reattach-else-spawn server-side).
      A failure's body is the module's own error text - a platform gap
      (no pty on this build, a 409 cap, a dead module) names itself,
      and the pane repeats it verbatim instead of the generic line. */
  async function claim(): Promise<string | null> {
    const res = await fetch(CLAIM, { method: "POST" });
    if (!res.ok) {
      const raw = (await res.text()).trim();
      let msg = raw || `HTTP ${res.status}`;
      try {
        const parsed = JSON.parse(raw) as { error?: string };
        if (parsed.error) msg = parsed.error;
      } catch {
        // a non-JSON refusal rides the raw body
      }
      claimFail = msg;
      return null;
    }
    const body = (await res.json()) as { id?: string };
    return body.id ?? null;
  }

  /** the SSE pipe: replay tail, live chunks, exit, missed reconnects. */
  function stream(id: string) {
    const seq = ++connectSeq;
    closeStream();
    source = new EventSource(`/terminal/${encodeURIComponent(id)}/stream`);
    source.onopen = () => {
      if (sessionId.value === id) {
        stateLine.value = "shell";
        errorLine.value = "";
      }
    };
    source.onmessage = (ev: MessageEvent<string>) => {
      if (sessionId.value !== id || connectSeq !== seq) return;
      let frame: WireFrame;
      try {
        frame = JSON.parse(ev.data) as WireFrame;
      } catch {
        return; // a shell chunk is never raw in a JSON frame
      }
      if (frame.t === "o" && frame.d) term.value?.write(atobBytes(frame.d));
      else if (frame.t === "m") void reconnect();
      else if (frame.t === "x") {
        dead.value = true;
        stateLine.value = "exited";
        const code = frame.c ?? -1;
        term.value?.write(`\r\n\x1b[90m[shell exited (${code})]\x1b[0m\r\n`);
        closeStream();
      }
    };
    source.onerror = () => {
      // EventSource auto-retries the same url; a vanished session
      // (kill, prune, restart) 404s into the missed path below
      if (sessionId.value === id && connectSeq === seq && !dead.value) {
        void reconnect();
      }
    };
  }

  /** a dropped stream: one quiet beat, then re-claim + re-stream. */
  async function reconnect() {
    closeStream();
    if (killed.value) return;
    await new Promise((r) => setTimeout(r, 500));
    if (killed.value) return;
    await open();
  }

  /** the claim's failure text (the module's error body, see claim()). */
  let claimFail = "";

  async function open() {
    killed.value = false;
    claimFail = "";
    const id = await claim();
    if (!id) {
      stateLine.value = "no terminal";
      errorLine.value = claimFail || "the terminal module did not answer the claim";
      return;
    }
    sessionId.value = id;
    dead.value = false;
    stream(id);
    // the pane's size on (re)attach - after the layout settles
    requestAnimationFrame(() => void resize());
  }

  /** keystrokes ride POST .../input as raw frame bodies (control
     chars are data; JSON would quote them into ambiguity). */
  async function input(s: string) {
    const id = sessionId.value;
    if (!id || dead.value) return;
    await fetch(`/terminal/${encodeURIComponent(id)}/input`, {
      method: "POST",
      headers: { "Content-Type": "application/octet-stream" },
      body: strBytes(s) as unknown as BodyInit,
    });
  }

  async function resize() {
    const t = term.value;
    const f = fit.value;
    if (!t || !f || !sessionId.value || !host.value) return;
    try {
      f.fit();
    } catch {
      return; // a zero-px pane fits nothing
    }
    const dims = t as unknown as { rows: number; cols: number };
    if (dims.rows > 0 && dims.cols > 0) {
      await fetch(`/terminal/${encodeURIComponent(sessionId.value)}/resize`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ rows: dims.rows, cols: dims.cols }),
      });
    }
  }

  /** the kill affordance: DELETE the session, mark the pane dead. */
  async function kill() {
    const id = sessionId.value;
    if (!id) return;
    killed.value = true;
    connectSeq++;
    closeStream();
    await fetch(`/terminal/${encodeURIComponent(id)}`, { method: "DELETE" });
    dead.value = true;
    stateLine.value = "exited";
    sessionId.value = "";
  }

  onMounted(() => {
    if (!host.value) return;
    const t = new Terminal({
      theme: termTheme(),
      fontFamily: "'JetBrains Mono', ui-monospace, monospace",
      fontSize: 13,
      convertEol: false,
      cursorBlink: true,
    });
    const f = new FitAddon();
    t.loadAddon(f);
    t.onData((s) => void input(s));
    term.value = t;
    fit.value = f;
    observer = new ResizeObserver(() => void resize());
    observer.observe(host.value);
    t.open(host.value);
    void open();
  });

  onBeforeUnmount(() => {
    // the outlet unmount unbinds the SSE, never the SHELL - the pty
    // idles server-side and the remount reattaches with its replay
    killed.value = false;
    connectSeq++;
    observer?.disconnect();
    observer = null;
    closeStream();
  });
</script>

<style scoped>
  .terminal-pane {
    position: relative;
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 120px;
    background: var(--z1, #1a1a1c);
  }
  .terminal-toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 2px 8px;
    font-size: 11px;
    color: var(--text-dim, #8a8a92);
    border-bottom: 1px solid var(--stroke, #2e2e31);
    flex: none;
  }
  .terminal-tools {
    display: flex;
    gap: 4px;
  }
  .terminal-btn {
    background: none;
    border: none;
    cursor: pointer;
    color: var(--text-dim, #8a8a92);
    padding: 2px;
    display: inline-flex;
  }
  .terminal-btn:hover {
    color: var(--text, #e6e6e9);
  }
  .terminal-host {
    flex: 1 1 auto;
    min-height: 0;
    padding: 4px 6px;
  }
  .terminal-host :deep(.xterm) {
    height: 100%;
  }
  .terminal-error {
    margin: 0;
    padding: 4px 8px;
    font-size: 11px;
    color: var(--err, #e05252);
    flex: none;
  }
</style>