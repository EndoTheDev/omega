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
         the SAME raw tail the pane holds - no server-side filter. -->
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
        live · {{ shown.length }} of {{ lines.length }} lines
      </p>
    </template>
  </section>
</template>

<script setup lang="ts">
  import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from "vue";
  import { isNarrowWidth } from "./narrow";
  import { LOG_STREAM_EVENT, LOG_SUBSCRIBE_EVENT, MAX_LINES, SNAPSHOT_OVERLAP, mergeTail, type LogStreamDetail } from "./logstream";
  import {
    filterLogLines,
    LOG_SEVERITIES,
    type LogSeverity,
  } from "./severity";

  // Transport-owned UI (card t_db430fea): the tail of the omega log the
  // channel's server itself writes. The FIRST read is REST -
  // GET /logs?lines=200 - and everything after it arrives on the push
  // channel's `logs` topic (card t_0e7bc0bb: the 2.5s poll this pane used
  // to run is gone; the socket carries the DELTA only, never a snapshot).
  // READ-ONLY: the endpoint is a tail read and the socket only listens.
  interface LogPayload {
    path: string;
    lines: string[];
  }
  const LINES = 200;

  const lines = ref<string[]>([]);
  const loaded = ref(false);
  const error = ref("");
  const bodyEl = ref<HTMLElement | null>(null);

  // pending buffers pushed lines that land before the snapshot does: the
  // pane subscribes first (so no appended line is lost in the gap), then
  // folds the buffer on with mergeTail - the ONE merge whose overlap bound
  // is open (the buffered run may repeat the snapshot's tail). Every later
  // push is a transport delta and appends at the default bound of zero.
  let pending: string[] = [];

  // onStream is the shell's half of the pane's window seam: a pushed
  // delta appends, a reset (the reconnect signal - nothing is replayed
  // server-side) re-reads the snapshot.
  function onStream(e: Event) {
    const detail = (e as CustomEvent<LogStreamDetail>).detail ?? {};
    if (detail.reset) {
      void load();
      return;
    }
    const incoming = detail.lines ?? [];
    if (!incoming.length) return;
    if (!loaded.value) {
      pending.push(...incoming);
      return;
    }
    lines.value = mergeTail(lines.value, incoming);
  }

  function subscribe(on: boolean) {
    if (typeof window === "undefined") return;
    window.dispatchEvent(new CustomEvent(LOG_SUBSCRIBE_EVENT, { detail: { on } }));
  }

  // --- the severity filter (card t_221dc498): toggle chips classify
  // the raw tail in-band (the "ERROR: "/"WARN: " markers after the
  // timestamp - severity.ts, pinned with the node suite). All three on
  // by default; the filter is view-only over the kept tail, so a chip
  // toggle is instant (no refetch round trip).
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

  async function load() {
    let nextError = "";
    let snapshot: string[] = [];
    try {
      const res = await fetch(`/logs?lines=${LINES}`);
      if (!res.ok) throw new Error(`GET /logs: ${res.status}`);
      const data: LogPayload = await res.json();
      snapshot = data.lines ?? [];
    } catch (e) {
      nextError = e instanceof Error ? e.message : String(e);
    }
    // a transient failure never wipes the view - the error state
    // renders alongside the last good tail
    if (!nextError || lines.value.length === 0) {
      error.value = nextError;
    }
    if (snapshot.length) {
      // the snapshot is the truth up to the moment it was read; the
      // buffer carries what the socket pushed during the read, so this is
      // the merge whose overlap bound is positional-open
      lines.value = mergeTail(snapshot, pending.splice(0), MAX_LINES, SNAPSHOT_OVERLAP);
    } else if (!nextError) {
      // an empty log is a real answer: keep the pushed lines that beat
      // the read, which are all this pane will show
      lines.value = mergeTail(lines.value, pending.splice(0), MAX_LINES, SNAPSHOT_OVERLAP);
    }
    // on a failure the buffer stays: the next successful read folds it
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

  // mount order matters: subscribe FIRST (the shell adds the `logs` topic
  // to its one connection and starts forwarding deltas - anything written
  // during the snapshot read lands in `pending`), then read the snapshot.
  onMounted(() => {
    subscribe(true);
    window.addEventListener(LOG_STREAM_EVENT, onStream);
    void load();
  });

  onBeforeUnmount(() => {
    window.removeEventListener(LOG_STREAM_EVENT, onStream);
    // the last mounted pane drops the topic: no pane, no tail on the wire
    subscribe(false);
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