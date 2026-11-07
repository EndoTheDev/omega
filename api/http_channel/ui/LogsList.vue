<template>
  <section
    ref="rootEl"
    class="logs-list"
    :class="{ narrow: narrow }">
    <!-- card t_308e6d9b: the UNIVERSAL band. Card t_221dc498: the Logs
         tab's first band actions - the severity filter (info/warning/
         error toggle chips, the card's scope). One chip per severity,
         aria-pressed carries the on/off state, the click toggles it;
         all-on is the default (the full stream). The chips classify
         the SAME raw tail the poll fetches - no server-side filter. -->
    <TabBand title="Logs">
      <button
        v-for="sev in LOG_SEVERITIES"
        :key="sev"
        class="band-tool logs-sev-chip"
        :class="{ off: !sevEnabled[sev] }"
        type="button"
        :aria-pressed="sevEnabled[sev]"
        :aria-label="`toggle ${sev} lines`"
        @click="toggleSev(sev)">
        {{ sev }}
      </button>
    </TabBand>
    <p
      v-if="error"
      class="panel-status">
      {{ error }}
    </p>
    <p
      v-else-if="!loaded"
      class="panel-status">
      loading
    </p>
    <template v-else>
      <div
        ref="bodyEl"
        class="logs-body">
        <template v-if="shown.length">
          <p
            v-for="(entry, i) in shown"
            :key="i"
            class="logs-line">
            {{ entry.text }}
          </p>
        </template>
        <p
          v-else-if="lines.length"
          class="panel-status">
          no lines match the severity filter
        </p>
        <p
          v-else
          class="panel-status">
          log file is empty
        </p>
      </div>
      <p
        class="logs-meta">
        polling every {{ POLL_MS / 1000 }}s · {{ shown.length }} of {{ lines.length }} lines
      </p>
    </template>
  </section>
</template>

<script setup lang="ts">
  import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from "vue";
  import { isNarrowWidth } from "./narrow";
  import {
    filterLogLines,
    LOG_SEVERITIES,
    type LogSeverity,
  } from "./severity";

  // Transport-owned UI (card t_db430fea): the tail of the omega log
  // the channel's server itself writes, from GET /logs?lines=200, and
  // LIVE: a 2.5s poll re-reads the tail; no websocket infrastructure
  // for this (settled v1 decision). READ-ONLY: the endpoint is a tail
  // read - the poll never mutates anything.
  interface LogPayload {
    path: string;
    lines: string[];
  }
  const POLL_MS = 2500;
  const LINES = 200;

  const lines = ref<string[]>([]);
  const loaded = ref(false);
  const error = ref("");
  const bodyEl = ref<HTMLElement | null>(null);

  // --- the severity filter (card t_221dc498): toggle chips classify
  // the raw tail in-band (the "ERROR: "/"WARN: " markers after the
  // timestamp - severity.ts, pinned with the node suite). All three on
  // by default; the filter is view-only, the poll always fetches the
  // full tail so a chip toggle is instant (no refetch round trip).
  const sevEnabled = reactive<Record<LogSeverity, boolean>>({
    info: true,
    warning: true,
    error: true,
  });
  function toggleSev(sev: LogSeverity) {
    sevEnabled[sev] = !sevEnabled[sev];
  }
  const shown = computed(() => filterLogLines(lines.value, sevEnabled));

  // --- narrow adaptation, the module-UI recipe (card t_a2fdc630): this
  // root measures its OWN rendered container and flips the class; the
  // scoped stylesheet below condenses (log lines carry long mono
  // paths - at ~300px the padding is dead weight). Same mechanism as
  // the shell's useNarrow - kept in-module because module UI imports
  // no shell code (Board precedent); threshold unit-covered in
  // ./narrow.ts.
  const rootEl = ref<HTMLElement | null>(null);
  const narrow = ref(false);
  let resizeObs: ResizeObserver | null = null;
  onMounted(() => {
    if (typeof ResizeObserver === "undefined" || !rootEl.value) return;
    // (same rAF deferral as the shell's useNarrow: the flip changes the
    // observed layout itself, so the callback must not mutate this frame)
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

  let timer: ReturnType<typeof setInterval> | null = null;

  async function poll() {
    let nextError = "";
    let next: string[] = [];
    try {
      const res = await fetch(`/logs?lines=${LINES}`);
      if (!res.ok) throw new Error(`GET /logs: ${res.status}`);
      const data: LogPayload = await res.json();
      next = data.lines ?? [];
    } catch (e) {
      nextError = e instanceof Error ? e.message : String(e);
    }
    // a transient failure never wipes the view - the error state
    // renders alongside the last good tail
    if (!nextError || lines.value.length === 0) {
      error.value = nextError;
    }
    if (next.length) lines.value = next;
    loaded.value = true;
  }

  // logs follow: the tail sticks to the bottom while the reader is at
  // the bottom; scrolled up (reading history), the update does not
  // yank the viewport. Watching SHOWN (not lines) so a filter toggle
  // re-runs the same law - a shrink while parked at the bottom keeps
  // the bottom parked.
  async function follow() {
    const el = bodyEl.value;
    if (!el) return;
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 20;
    await nextTick();
    if (atBottom) el.scrollTop = el.scrollHeight;
  }

  watch(shown, follow, { flush: "post" });

  onMounted(() => {
    void poll();
    timer = setInterval(poll, POLL_MS);
  });

  onBeforeUnmount(() => {
    if (timer !== null) clearInterval(timer);
  });
</script>

<style scoped>
  .logs-list {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-height: 0;
    padding: 0;
  }
  .panel-status {
    margin: 0;
    padding: 10px 12px;
    font-size: 11px;
    opacity: 0.6;
  }
  .logs-body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 10px 12px;
  }
  .logs-line {
    margin: 0;
    font-family: var(--mono);
    font-size: 10.5px;
    line-height: 1.5;
    white-space: break-spaces;
    word-break: break-all;
    opacity: 0.75;
  }
  .logs-meta {
    margin: 0;
    padding: 4px 12px 8px;
    font-size: 10px;
    opacity: 0.45;
  }
  /* the severity chips (card t_221dc498): the band-tool look plus the
     toggle state. aria-pressed carries it for the a11y tree; the eye
     needs the dimming - an ON chip reads full text, an OFF chip reads
     dimmed like a disabled band tool. */
  .logs-list .logs-sev-chip[aria-pressed="true"] {
    color: var(--text);
  }
  .logs-list .logs-sev-chip.off {
    opacity: 0.45;
  }
  /* narrow: the module-UI recipe (card t_a2fdc630) - the tail's mono
     lines are widest at the top of the file; recovering the horizontal
     padding gives every long line less wrap, not more. */
  .logs-list.narrow .panel-status,
  .logs-list.narrow .logs-body {
    padding: 6px 8px;
  }
  .logs-list.narrow .logs-line {
    font-size: 10px;
  }
  .logs-list.narrow .logs-meta {
    padding: 3px 8px 6px;
  }
</style>