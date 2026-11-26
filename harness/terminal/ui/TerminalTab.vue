<template>
  <section
    class="terminal-pane"
    @contextmenu.capture="onContextMenu">
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
  import { openContextMenu } from "../../../webui/app/composables/contextMenu";
  import { copyText, readClipboardText } from "../../../webui/app/composables/clipboard";
  import { terminalMenuItems } from "./terminalMenu";
  import { createInputWriter } from "./terminalInputWriter";

  // The shell's spawn workspace follows the focused chat session. The SHELL ONLY reads
  // it at claim time; a later focus change never re-roots a living shell.
  const props = defineProps<{ focusedSession?: string | null }>();

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

  async function claim(): Promise<string | null> {
    // the focused session id rides the claim (never a root!).
    const url = props.focusedSession
      ? `${CLAIM}?session=${encodeURIComponent(props.focusedSession)}`
      : CLAIM;
    const res = await fetch(url, { method: "POST" });
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
      // EventSource auto-retries the same url.
      if (sessionId.value === id && connectSeq === seq && !dead.value) {
        void reconnect();
      }
    };
  }

  async function reconnect() {
    closeStream();
    if (killed.value) return;
    await new Promise((r) => setTimeout(r, 500));
    if (killed.value) return;
    await open();
  }

  let claimFail = "";

  // The claim is the pane's ONE door to a shell, and it fails transiently: the store's
  // session cap frees once a sibling shell exits, a spawn is refused under load, a
  // fetch dies with the lane. The ladder keeps the pane recoverable - the state line
  // reports the live refusal while it runs, and a later answer promotes it to "shell"
  // - instead of stranding the pane until a page reload.
  const CLAIM_RETRY_MS = [500, 1000, 2000, 4000, 8000];

  async function open() {
    killed.value = false;
    claimFail = "";
    let attempt = 0;
    for (;;) {
      let id: string | null = null;
      try {
        id = await claim();
      } catch (e) {
        claimFail = e instanceof Error ? e.message : String(e);
      }
      if (killed.value) return;
      if (id) {
        if (sessionId.value && sessionId.value !== id) writer.reset();
        sessionId.value = id;
        dead.value = false;
        stateLine.value = "connecting...";
        errorLine.value = "";
        stream(id);
        requestAnimationFrame(() => void resize());
        return;
      }
      dead.value = true;
      stateLine.value = "no terminal";
      errorLine.value = claimFail || "the terminal module did not answer the claim";
      if (attempt >= CLAIM_RETRY_MS.length) return;
      await new Promise((r) => setTimeout(r, CLAIM_RETRY_MS[attempt]));
      attempt++;
      if (killed.value) return;
    }
  }

  /** keystrokes ride POST /input as raw frame bodies: control chars are data. */
  async function postInput(bytes: Uint8Array): Promise<unknown> {
    const id = sessionId.value;
    if (!id || dead.value) return;
    return fetch(`/terminal/${encodeURIComponent(id)}/input`, {
      method: "POST",
      headers: { "Content-Type": "application/octet-stream" },
      body: bytes as unknown as BodyInit,
    });
  }

  // The pane's writer owns the shape of typing: printable runs coalesce into one POST,
  // control keys leave at once, and only one POST is ever in flight - so the shell reads
  // the bytes in the order they were typed.
  const writer = createInputWriter({ send: postInput });

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
    writer.reset();
    connectSeq++;
    closeStream();
    await fetch(`/terminal/${encodeURIComponent(id)}`, { method: "DELETE" });
    dead.value = true;
    stateLine.value = "exited";
    sessionId.value = "";
  }

  // THE TERMINAL MENU: an omega surface (chrome by the design line), so right-click is the
  // terminal's own menu, never the browser's. Copy rides the ONE copy path; Paste asks the
  // clipboard and hands the text to xterm's own paste (it never fabricates keystrokes);
  // Clear drops the scrollback and keeps the live line.
  function copySelection() {
    const t = term.value;
    if (!t) return;
    const text = t.getSelection();
    if (text) void copyText(text);
  }

  async function pasteClipboard() {
    const t = term.value;
    if (!t) return;
    const text = await readClipboardText();
    if (text) {
      t.focus();
      t.paste(text);
      return;
    }
    t.focus();
    try {
      document.execCommand("paste");
    } catch {
      // the browser refuses the deprecated paste door; nothing was pasted
    }
  }

  function clearScreen() {
    term.value?.clear();
  }

  function onContextMenu(e: MouseEvent) {
    const t = term.value;
    if (!t) return;
    const rows = terminalMenuItems({
      hasSelection: t.hasSelection(),
      copy: copySelection,
      paste: pasteClipboard,
      clear: clearScreen,
    });
    if (openContextMenu(e.clientX, e.clientY, rows)) e.preventDefault();
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
    t.onData((s) => writer.push(s));
    term.value = t;
    fit.value = f;
    observer = new ResizeObserver(() => void resize());
    observer.observe(host.value);
    t.open(host.value);
    void open();
  });

  onBeforeUnmount(() => {
    // the outlet unmount unbinds the SSE, never the SHELL.
    killed.value = false;
    writer.reset();
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
    min-width: 0;
    overflow: hidden;
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
    min-width: 0;
    overflow: hidden;
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