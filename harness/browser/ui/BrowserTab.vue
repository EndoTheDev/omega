<template>
  <div
    ref="rootEl"
    class="browser-tab"
    :class="{ narrow: narrow }">
    <!-- the URL row: back / forward / the address / reload. -->
    <div class="browser-bar">
      <button
        class="band-tool"
        :disabled="!canStep(nav, -1)"
        aria-label="browser back"
        title="back"
        @click="step(-1)">
        <Icon
          name="nav-back"
          :size="13" />
      </button>
      <button
        class="band-tool"
        :disabled="!canStep(nav, +1)"
        aria-label="browser forward"
        title="forward"
        @click="step(+1)">
        <Icon
          name="nav-forward"
          :size="13" />
      </button>
      <input
        v-model="urlInput"
        class="browser-url"
        inputmode="url"
        spellcheck="false"
        aria-label="browser url"
        placeholder="paste a URL - https://…"
        @keydown.enter="go" />
      <button
        class="band-tool"
        :disabled="address === ''"
        aria-label="reload page"
        title="reload"
        @click="reload">
        <Icon
          name="refresh"
          :size="13" />
      </button>
    </div>

    <!-- the empty state (the standard pattern, never a blank pane). -->
    <div
      v-if="address === ''"
      class="browser-blank">
      <p class="browser-blank-text">Browse the web in a pane - any site, any origin.</p>
      <p class="browser-blank-hint">Type an address above and press Enter. Sites that refuse embedding fall back below with an open-in-new-tab path.</p>
    </div>

    <div
      v-if="refused"
      class="browser-refused">
      <p class="refused-title">This page can't be shown inline.</p>
      <p class="refused-hint">
        It refuses embedding (X-Frame-Options) or did not load.
      </p>
      <div class="refused-actions">
        <button
          class="browser-act"
          aria-label="open in a new tab"
          @click="openExternal">
          open in a new tab
        </button>
        <button
          class="browser-act"
          aria-label="copy url"
          @click="copyUrl">
          {{ copied ? "copied" : "copy URL" }}
        </button>
      </div>
    </div>

    <iframe
      v-show="address !== '' && !refused"
      ref="frameEl"
      :key="frameKey"
      class="browser-frame"
      :src="frameSrc"
      title="browser page"
      @load="onFrameLoad" />
  </div>
</template>

<script setup lang="ts">
  // The general web browser tab. The server side is harness/browser's GET/PUT
  // /browser/last - the resume URL this tab restores on mount and persists after every
  // successful embed load; a refused frame never persists (the no-load rule above is
  // also the write gate, for free).
  import {
    FALLBACK_AFTER_MS,
    PERSIST_GRACE_MS,
    canStep,
    classifyProbe,
    emptyNav,
    normalizeUrl,
    pushNav,
    stepNav,
    type NavHistory,
  } from "./browserLogic";
  import { isNarrowWidth } from "./narrow";
  // the ONE copy path - insecure-context-proof
  import { copyText } from "~/composables/clipboard";
  import { computed, onBeforeUnmount, onMounted, ref } from "vue";

  const rootEl = ref<HTMLElement | null>(null);
  const narrow = ref(false);
  let resizeObs: ResizeObserver | null = null;
  onMounted(() => {
    if (typeof ResizeObserver === "undefined" || !rootEl.value) return;
    resizeObs = new ResizeObserver((entries) => {
      const pending = entries
        .map((e) => Math.round(e.contentRect.width))
        .filter((w) => w > 0);
      if (pending.length === 0) return;
      requestAnimationFrame(() => {
        if (resizeObs === null) return;
        for (const w of pending) narrow.value = isNarrowWidth(w);
      });
    });
    resizeObs.observe(rootEl.value);
    // mount measures NOW: a tab remount in place produces no further
    // resize entry, so the class must not wait for the next width
    // change (the shell's useNarrow does the same on mount)
    narrow.value = isNarrowWidth(Math.round(rootEl.value.getBoundingClientRect().width));
  });
  onBeforeUnmount(() => {
    resizeObs?.disconnect();
    resizeObs = null;
  });

  const urlInput = ref("");
  const address = ref("");
  const frameSrc = ref("");
  const frameKey = ref(0);
  const nav = ref<NavHistory>(emptyNav());
  const pending = ref(false);
  const refused = ref(false);
  const probeRefused = ref(false);
  const copied = ref(false);
  let fallbackTimer: ReturnType<typeof setTimeout> | null = null;
  let probeGen = 0;


  function go() {
    navigate(normalizeUrl(urlInput.value));
  }

  function navigate(u: string) {
    if (u === "") return;
    urlInput.value = u;
    address.value = u;
    frameSrc.value = u;
    frameKey.value++;
    nav.value = pushNav(nav.value, u);
    beginEmbed(u);
  }

  // step is the history belt: the buttons never lie (canStep's disabled edge).
  function step(delta: number) {
    const next = stepNav(nav.value, delta);
    if (!next || !canStep(nav.value, delta)) return;
    nav.value = next;
    // a history step REPLAYS the frame without growing the stack
    navigateReplay(nav.value.urls[nav.value.idx]);
  }
  // navigateReplay steps the belt's target back THROUGH the commit
  // path (the stack already holds the address - pushNav collapses the
  // duplicate) and re-opens the honest window.
  function navigateReplay(u: string) {
    urlInput.value = u;
    address.value = u;
    frameSrc.value = u;
    frameKey.value++;
    beginEmbed(u);
  }

  function reload() {
    if (address.value === "") return;
    frameSrc.value = address.value;
    frameKey.value++;
    beginEmbed(address.value);
  }

  // beginEmbed opens the honest window. Only a definitive false flips the fallback;
  // cannot-say keeps the frame.
  function beginEmbed(u: string) {
    pending.value = true;
    refused.value = false;
    probeRefused.value = false;
    if (fallbackTimer) clearTimeout(fallbackTimer);
    fallbackTimer = setTimeout(() => {
      if (pending.value) refused.value = true;
      fallbackTimer = null;
    }, FALLBACK_AFTER_MS);
    void runProbe(u, ++probeGen);
  }

  async function runProbe(u: string, gen: number) {
    const verdict = await probeFrameable(u, window.location.origin);
    if (gen !== probeGen || address.value !== u) return; // superseded
    if (verdict === "refused") {
      probeRefused.value = true;
      pending.value = false;
      refused.value = true;
      if (fallbackTimer) {
        clearTimeout(fallbackTimer);
        fallbackTimer = null;
      }
    }
  }

  async function probeFrameable(u: string, origin: string): Promise<"refused" | "ok" | "unknown"> {
    try {
      const r = await fetch(
        `/browser/frameable?url=${encodeURIComponent(u)}&origin=${encodeURIComponent(origin)}`,
      );
      if (!r.ok) return "unknown";
      return classifyProbe(await r.json());
    } catch {
      return "unknown";
    }
  }

  function onFrameLoad() {
    if (probeRefused.value) {
      return;
    }
    pending.value = false;
    refused.value = false;
    if (fallbackTimer) {
      clearTimeout(fallbackTimer);
      fallbackTimer = null;
    }
    // the write gate waits a beat past the load: a refused embed's
    // error page ALSO fires load - if the probe's negative lands inside
    // the grace window the persist never happens (a slow probe persists
    // a harmless resume; the next mount re-probes and refuses)
    setTimeout(() => {
      if (!probeRefused.value && !refused.value) {
        void persistLast(address.value);
      }
    }, PERSIST_GRACE_MS);
  }

  // the capsule route (GET/PUT /browser/last)

  interface LastReply {
    last?: string;
  }

  function isValidForWire(u: unknown): u is string {
    return typeof u === "string" && /^https?:\/\//.test(u);
  }

  async function persistLast(u: string) {
    if (!isValidForWire(u)) return;
    try {
      await fetch("/browser/last", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ url: u }),
      });
    } catch {
    }
  }

  onMounted(async () => {
    try {
      const r = await fetch("/browser/last");
      if (!r.ok) return; // the module's routes are dead: no resume point
      const body = (await r.json()) as LastReply;
      if (isValidForWire(body.last)) {
        navigate(body.last);
      }
    } catch {
    }
  });


  function openExternal() {
    if (address.value === "") return;
    window.open(address.value, "_blank", "noopener");
  }

  async function copyUrl() {
    if (address.value === "") return;
    // the ONE copy path - insecure-context-proof
    const ok = await copyText(address.value);
    if (ok) {
      copied.value = true;
      setTimeout(() => (copied.value = false), 1500);
    } else {
      // honest failure: the URL is still visible in the bar
      copied.value = false;
    }
  }

  // the exposed surface the unit suite pins (tests/browser.test.ts)
  defineExpose({
    address,
    nav,
    refused,
    urlInput,
    go,
    navigate,
    step,
    reload,
    openExternal,
    copyUrl,
  });
</script>

<style scoped>
  .browser-tab {
    display: flex;
    flex-direction: column;
    height: 100%;
    overflow: hidden;
    background: var(--z0);
    color: var(--text);
  }
  .browser-bar {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 6px 10px;
    border-bottom: 1px solid var(--stroke);
  }
  .browser-url {
    flex: 1;
    min-width: 0;
    font-family: var(--mono);
    font-size: 11px;
    padding: 4px 8px;
    border: 1px solid var(--stroke);
    border-radius: var(--r-chip);
    background: var(--z1);
    color: var(--text);
    outline: none;
  }
  .browser-url:focus {
    border-color: var(--accent);
  }
  .browser-url::placeholder {
    color: var(--text-dim);
  }
  .browser-frame {
    flex: 1;
    min-height: 0;
    border: 0;
    width: 100%;
    background: var(--z0);
  }
  .browser-blank {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 6px;
    padding: 0 24px;
    text-align: center;
  }
  .browser-blank-text {
    margin: 0;
    font-size: 12px;
    color: var(--text);
  }
  .browser-blank-hint {
    margin: 0;
    font-size: 11px;
    color: var(--text-dim);
    max-width: 420px;
  }
  .browser-refused {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 8px;
    padding: 0 24px;
    text-align: center;
  }
  .refused-title {
    margin: 0;
    font-size: 12px;
    font-weight: 600;
  }
  .refused-hint {
    margin: 0;
    font-size: 11px;
    color: var(--text-dim);
    font-family: var(--mono);
  }
  .refused-actions {
    display: flex;
    gap: 8px;
  }
  .browser-act {
    font-size: 11px;
    padding: 4px 10px;
    border: 1px solid var(--stroke);
    border-radius: var(--r-chip);
    background: transparent;
    color: var(--text);
    cursor: pointer;
  }
  .browser-act:hover {
    border-color: var(--accent);
    color: var(--accent);
  }
  /* narrow: the module-owned threshold (./narrow.ts, pinned to agree with the shell's copy). */
  .browser-tab.narrow .band-tool[aria-label="browser back"],
  .browser-tab.narrow .band-tool[aria-label="browser forward"] {
    display: none;
  }
</style>