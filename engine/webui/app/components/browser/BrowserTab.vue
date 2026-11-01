<template>
  <div
    ref="rootEl"
    class="browser-tab"
    :class="{ narrow }">
    <!-- the URL row: back / forward / the address / reload. The address
         is the pane's OWN record - cross-origin frame history is
         unreadable, so the stack is the typed-commit log here. -->
    <div class="browser-bar">
      <button
        class="band-tool"
        :disabled="!canStep(nav, -1)"
        aria-label="browser back"
        title="back"
        @click="step(-1)">
        ←
      </button>
      <button
        class="band-tool"
        :disabled="!canStep(nav, +1)"
        aria-label="browser forward"
        title="forward"
        @click="step(+1)">
        →
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

    <!-- the empty state (the standard pattern, never a blank pane): what
         the pane is, and the honest ceiling while it loads. -->
    <div
      v-if="address === ''"
      class="browser-blank">
      <p class="browser-blank-text">Browse the web in a pane - any site, any origin.</p>
      <p class="browser-blank-hint">Type an address above and press Enter. Sites that refuse embedding fall back below with an open-in-new-tab path.</p>
    </div>

    <!-- the refused/failed embed fallback (the known ceiling, built
         honestly): flips in after FALLBACK_AFTER_MS without a load
         event, flips OUT if the frame loads late. The frame stays
         mounted underneath - v-show, so the late-load recovery and the
         reload button keep working from this state. -->
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

    <!-- the frame. No sandbox, no wrapper origin games: this is a
         browser - normal embed rules, the site's own frame headers
         decide (the settled design, no middleman in v1). -->
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
  // The general web browser tab (card t_95c91f9c, Endo ruling
  // 2026-11-01: "the browser should be like a normal browser which also
  // allows me to visit other websites"). A URL bar + an iframe embed,
  // any origin, no allowlist, no origin gate. The server side is
  // modules/browser's GET/PUT /browser/last - the resume URL this tab
  // restores on mount and persists after every successful embed load;
  // a refused frame never persists (the no-load rule above is also the
  // write gate, for free).
  import { computed, onMounted, ref } from "vue";
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
  import { useNarrow } from "~/composables/narrow";
  import Icon from "../ui/Icon.vue";

  const rootEl = ref<HTMLElement | null>(null);
  const narrow = useNarrow(rootEl);

  // the bar's draft text (type freely, commit on Enter)
  const urlInput = ref("");
  // the committed address the frame carries
  const address = ref("");
  const frameSrc = ref("");
  const frameKey = ref(0);
  const nav = ref<NavHistory>(emptyNav());
  // the embed-load state machine: pending -> frame navigating,
  // refused -> the fallback (the probe's negative OR the 4s
  // no-decision window), probeRefused -> a load event may NOT heal the
  // refusal (Chromium fires load for refused embeds too) nor persist
  const pending = ref(false);
  const refused = ref(false);
  const probeRefused = ref(false);
  const copied = ref(false);
  let fallbackTimer: ReturnType<typeof setTimeout> | null = null;
  // the probe run's generation: a commit while a probe is in flight
  // supersedes its answer (answers arrive out of order)
  let probeGen = 0;

  // --- navigation ---

  function go() {
    navigate(normalizeUrl(urlInput.value));
  }

  // navigate commits an address: the stack grows, the frame loads,
  // the probe runs, the fallback window opens. A refused address
  // re-navigated re-probes honestly (the site may have stopped
  // refusing; no cache).
  function navigate(u: string) {
    if (u === "") return;
    urlInput.value = u;
    address.value = u;
    frameSrc.value = u;
    frameKey.value++;
    nav.value = pushNav(nav.value, u);
    beginEmbed(u);
  }

  // step is the history belt: the buttons never lie (canStep's disabled
  // edge), the belt re-checks on miss.
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
    // a :key bump remounts the frame - the only local force-reload an
    // iframe src of an uncontrolled cross-origin page honors
    frameSrc.value = address.value;
    frameKey.value++;
    beginEmbed(address.value);
  }

  // beginEmbed opens the honest window: the 4s no-decision fallback
  // timer, plus the frameability probe (the fast signal). Only a
  // definitive false flips the fallback; cannot-say keeps the frame.
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
      // a load event fires for a refused embed in Chromium too: the
      // probe's negative stands, the fallback stays, nothing persists
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

  // --- the capsule route (GET/PUT /browser/last) ---

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
      // persistence is the cheapest loss in the home - browsing continues
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
      // same answer: browse from the empty state
    }
  });

  // --- the fallback affordances ---

  function openExternal() {
    if (address.value === "") return;
    window.open(address.value, "_blank", "noopener");
  }

  async function copyUrl() {
    if (address.value === "") return;
    try {
      await navigator.clipboard.writeText(address.value);
      copied.value = true;
      setTimeout(() => (copied.value = false), 1500);
    } catch {
      // clipboard denied: the URL is still visible in the bar
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
  /* narrow recipe: the shell's own component, the shared threshold
     (composables/narrow.ts) - below 360px the bar drops the arrow
     buttons and the url input takes the full row width */
  .browser-tab.narrow .band-tool[aria-label="browser back"],
  .browser-tab.narrow .band-tool[aria-label="browser forward"] {
    display: none;
  }
</style>