<template>
  <section
    ref="rootEl"
    class="obs-pane"
    :class="{ narrow }">
    <TabBand title="Logs">
      <!-- the SESSION scope: the open session by default. -->
      <button
        class="band-tool obs-chip obs-scope"
        type="button"
        :aria-pressed="allScope"
        :aria-label="scopeAria"
        :title="scopeTitle"
        :disabled="!session"
        @click="toggleScopeAll">
        {{ scopeLabel }}
      </button>
      <!-- the errors-only chip: a view filter over the fetched page, never a server. -->
      <button
        class="band-tool obs-chip obs-errors"
        :class="{ off: !errorsOnly }"
        type="button"
        :aria-pressed="errorsOnly"
        aria-label="toggle errors only"
        title="errors only"
        @click="errorsOnly = !errorsOnly">
        errors
      </button>
      <button
        class="band-tool obs-chip obs-tail"
        :class="{ off: !tailOpen }"
        type="button"
        :aria-pressed="tailOpen"
        aria-label="toggle raw log tail"
        title="raw log tail"
        @click="tailOpen = !tailOpen">
        tail
      </button>
      <button
        class="band-tool obs-chip obs-clear"
        type="button"
        :disabled="tailOpen || !events.length"
        :aria-label="clearAria"
        :title="clearTitle"
        @click="clearStream">
        clear
      </button>
    </TabBand>

    <!-- the SOURCE family: one toggle per source, multi-select, AND-composed with the
         severity chip. A view filter over the fetched page, like errors-only - the ring
         has one page in it, so no toggle costs a round trip. -->
    <div
      v-if="!tailOpen"
      class="obs-sources"
      role="toolbar"
      aria-label="source filters">
      <button
        v-for="s in SOURCES"
        :key="s"
        class="obs-src"
        :class="['src-' + s, { off: !sourceOn(s) }]"
        type="button"
        :aria-pressed="sourceOn(s)"
        :aria-label="'toggle ' + s + ' records'"
        :title="sourceTitle(s)"
        @click="toggleSource(s)">
        <span class="obs-src-dot" aria-hidden="true"></span>{{ s }}
      </button>
    </div>

    <p
      v-if="readError"
      class="panel-status obs-error">
      {{ readError }}
    </p>

    <div
      v-if="!tailOpen"
      class="obs-body">
      <div
        class="obs-head"
        :class="{ allscope: allScope }"
        aria-hidden="true">
        <span v-if="allScope" class="obs-cell obs-session">session</span>
        <span class="obs-cell obs-time">time</span>
        <span class="obs-cell obs-type">type</span>
        <span class="obs-cell obs-lvl">level</span>
        <span class="obs-cell obs-tool">tool</span>
        <span class="obs-cell obs-dur">dur</span>
        <span class="obs-cell obs-sum">summary</span>
      </div>
      <template v-if="shown.length">
        <div
          v-for="e in shown"
          :key="e.seq"
          class="obs-row"
          :class="[e.level, 'src-' + sourceFor(e), { allscope: allScope }]"
          :data-source="sourceFor(e)">
          <span
            v-if="allScope"
            class="obs-cell obs-session"
            :title="e.session || 'no session (one-shot run)'">
            {{ shortSession(e.session) }}
          </span>
          <span class="obs-cell obs-time" :title="e.time">{{ formatEventTime(e.time) }}</span>
          <span class="obs-cell obs-type" :title="'source: ' + sourceFor(e)">{{ e.type }}</span>
          <span class="obs-cell obs-lvl">{{ e.level }}</span>
          <span class="obs-cell obs-tool">{{ e.tool || "" }}</span>
          <span class="obs-cell obs-dur">{{ formatDuration(e.duration_ms) }}</span>
          <span class="obs-cell obs-sum" :title="e.summary">{{ e.summary }}</span>
          <span v-if="e.error" class="obs-cell obs-errline">{{ e.error }}</span>
        </div>
      </template>
      <p v-else class="panel-status">{{ emptyLine }}</p>
    </div>

    <div v-else ref="bodyEl" class="obs-body obs-tailbody">
      <template v-if="tailLines.length">
        <p
          v-for="(line, i) in tailLines"
          :key="i"
          class="obs-tail-line">
          {{ line }}
        </p>
      </template>
      <p v-else-if="!loaded" class="panel-status">loading</p>
      <p v-else class="panel-status">log file is empty</p>
    </div>

    <p class="obs-meta">{{ metaLine }}</p>
  </section>
</template>

<script setup lang="ts">
  import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
  import { isNarrowWidth } from "./narrow";
  import {
    describeReadError,
    errorCount,
    EVENTS_PATH,
    eventsQuery,
    eventSource,
    formatDuration,
    formatEventTime,
    LOGS_PATH,
    logsQuery,
    parseEventsPayload,
    parseLogsPayload,
    parseSessionLabels,
    REFRESH_MS,
    SESSIONS_PATH,
    shortSession,
    SOURCES,
    sourceCounts,
    visibleEvents,
    type ObservatoryEvent,
    type Source,
  } from "./rows";

  // `session` is the OPEN session (app.vue's focusedSessionId).
  const props = defineProps<{ session?: string | null }>();

  const events = ref<ObservatoryEvent[]>([]);
  const buffered = ref(0);
  const capacity = ref(0);
  const tailPath = ref("");
  const tailLines = ref<string[]>([]);
  const loaded = ref(false);
  const readError = ref("");

  const scopeAll = ref(false);
  const errorsOnly = ref(false);
  const tailOpen = ref(false);

  // the source family: every source starts ON (the unfiltered stream), a click removes or
  // returns one - multi-select, AND-composed with the severity chip.
  const enabled = ref<Set<Source>>(new Set(SOURCES));
  // the clear button's watermark: display-only, so a refresh keeps arriving and paints
  // over it while the rows already read stay hidden.
  const clearedThrough = ref(0);
  // session id -> store label; the only provenance the stream lacks is a run's origin,
  // and the scheduler writes it as the session's label.
  const labels = ref<Map<string, string>>(new Map());

  const allScope = computed(() => scopeAll.value || !props.session);
  const scopedSession = computed(() => (allScope.value ? "" : (props.session ?? "")));

  const scopeLabel = computed(() => (allScope.value ? "all sessions" : "this session"));
  const scopeAria = computed(() =>
    props.session ? "toggle all sessions" : "all sessions (no open session)",
  );
  const scopeTitle = computed(() => {
    if (!props.session) return "no open session - every session is in scope";
    return scopeAll.value ? "showing every session" : `showing session ${props.session}`;
  });

  function toggleScopeAll() {
    if (!props.session) return; // nothing to switch to
    scopeAll.value = !scopeAll.value;
  }

  function sourceOn(s: Source): boolean {
    return enabled.value.has(s);
  }

  function toggleSource(s: Source) {
    const next = new Set(enabled.value);
    if (next.has(s)) next.delete(s);
    else next.add(s);
    enabled.value = next;
  }

  function sourceFor(e: ObservatoryEvent): Source {
    return eventSource(e, labels.value);
  }

  function sourceTitle(s: Source): string {
    const n = sourceCounts(events.value, labels.value)[s];
    return `${s} - ${n} record${n === 1 ? "" : "s"} on this page`;
  }

  const shown = computed(() =>
    visibleEvents(
      events.value,
      {
        errorsOnly: errorsOnly.value,
        sources: Array.from(enabled.value),
        clearedThrough: clearedThrough.value,
      },
      undefined,
      labels.value,
    ),
  );

  // the clear button's display watermark: everything read so far goes out of view, the
  // ring and the log file keep it (a later record still paints).
  function clearStream() {
    const top = events.value.reduce((m, e) => (e.seq > m ? e.seq : m), clearedThrough.value);
    clearedThrough.value = top;
  }

  const clearAria = "clear the visible stream";
  const clearTitle = computed(() => {
    if (tailOpen.value) return "the raw tail is the log file - nothing here clears it";
    return "clear the view - the ring and the log file keep every record";
  });

  const emptyLine = computed(() => {
    if (!events.value.length) {
      return loaded.value
        ? "no events recorded yet - the ring fills as runs execute"
        : "loading";
    }
    if (enabled.value.size === 0) return "every source is off - turn a source chip back on";
    if (errorsOnly.value && !errorCount(events.value)) return "no errors in the ring";
    if (!shown.value.length) return "the stream is cleared - new records appear as they arrive";
    return "no row matches the filters";
  });

  const metaLine = computed(() => {
    if (tailOpen.value) {
      const n = tailLines.value.length;
      return `${tailPath.value || "no log file"} · ${n} line${n === 1 ? "" : "s"} · refreshed every ${REFRESH_MS / 1000}s`;
    }
    const total = events.value.length;
    const errs = errorCount(events.value);
    const count = shown.value.length === total
      ? `${total} event${total === 1 ? "" : "s"}`
      : `${shown.value.length} of ${total} events`;
    const ring = capacity.value ? `${buffered.value}/${capacity.value} buffered` : "ring empty";
    const cleared = clearedThrough.value > 0 ? ` · cleared through ${clearedThrough.value}` : "";
    return `${count} · ${errs} error${errs === 1 ? "" : "s"} · ${ring}${cleared}`;
  });

  // one in-flight read at a time: a scope flip while a fetch is airborne must not paint
  // the OLD scope's answer over the new one.
  let ticket = 0;

  async function readStream(): Promise<void> {
    const mine = ++ticket;
    const path = `${EVENTS_PATH}?${eventsQuery(scopedSession.value)}`;
    try {
      const res = await fetch(path);
      if (mine !== ticket) return;
      if (!res.ok) throw new Error(describeReadError(EVENTS_PATH, res.status));
      const body = parseEventsPayload(await res.json());
      if (mine !== ticket) return;
      events.value = body.events;
      buffered.value = body.buffered;
      capacity.value = body.capacity;
      readError.value = "";
    } catch (e) {
      if (mine !== ticket) return;
      // a transient failure never wipes the view.
      readError.value = e instanceof Error ? e.message : String(e);
    }
    loaded.value = true;
    void readLabelsForPage();
  }

  // the session labels: one read on mount, plus one more whenever the page carries a
  // session nobody has labelled yet. A session the store no longer lists is REMEMBERED as
  // a miss - re-reading for it every 5s would be a poll of a deleted row.
  const labelMisses = new Set<string>();
  let labelTicket = 0;

  async function readLabels(): Promise<void> {
    const mine = ++labelTicket;
    try {
      const res = await fetch(SESSIONS_PATH);
      if (mine !== labelTicket) return;
      if (!res.ok) return; // the labels only attribute runs; an unreadable store costs the cron chip its rows
      const map = parseSessionLabels(await res.json());
      if (mine !== labelTicket) return;
      labels.value = map;
    } catch {
      // same degradation: the pane keeps painting, every record by its own producer
      return;
    }
    for (const id of events.value) {
      if (id.session && !labels.value.has(id.session)) labelMisses.add(id.session);
    }
  }

  function readLabelsForPage(): Promise<void> {
    const unknown = events.value.some(
      (e) => e.session && !labels.value.has(e.session) && !labelMisses.has(e.session),
    );
    return unknown ? readLabels() : Promise.resolve();
  }

  async function readTail(): Promise<void> {
    const mine = ++ticket;
    const path = `${LOGS_PATH}?${logsQuery()}`;
    try {
      const res = await fetch(path);
      if (mine !== ticket) return;
      if (!res.ok) throw new Error(describeReadError(LOGS_PATH, res.status));
      const body = parseLogsPayload(await res.json());
      if (mine !== ticket) return;
      tailPath.value = body.path;
      tailLines.value = body.lines;
      readError.value = "";
    } catch (e) {
      if (mine !== ticket) return;
      readError.value = e instanceof Error ? e.message : String(e);
    }
    loaded.value = true;
  }

  function refresh(): Promise<void> {
    return tailOpen.value ? readTail() : readStream();
  }

  // the tail follows its own bottom while the reader is parked there;
  // scrolled up (reading history) an update never yanks the viewport.
  const bodyEl = ref<HTMLElement | null>(null);
  async function followTail() {
    const el = bodyEl.value;
    if (!el || !tailOpen.value) return;
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 20;
    await nextTick();
    if (atBottom) el.scrollTop = el.scrollHeight;
  }
  watch(tailLines, followTail, { flush: "post" });

  watch(scopedSession, () => void readStream());
  watch(tailOpen, () => void refresh());

  let timer: ReturnType<typeof setInterval> | null = null;

  onMounted(() => {
    void refresh();
    timer = setInterval(() => void refresh(), REFRESH_MS);
  });

  onBeforeUnmount(() => {
    if (timer !== null) clearInterval(timer);
    timer = null;
  });

  // narrow adaptation, the module-UI recipe.
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
      // the flip changes the observed layout itself: mutate on the next frame.
      requestAnimationFrame(() => {
        if (resizeObs === null) return;
        for (const w of pending) narrow.value = isNarrowWidth(w);
      });
    });
    resizeObs.observe(rootEl.value);
    narrow.value = isNarrowWidth(Math.round(rootEl.value.getBoundingClientRect().width));
  });
  onBeforeUnmount(() => {
    resizeObs?.disconnect();
    resizeObs = null;
  });
</script>

<style scoped>
  .obs-pane {
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
  .obs-error {
    color: var(--err);
    opacity: 0.9;
  }
  .obs-body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 4px 12px 6px;
  }
  .obs-head,
  .obs-row {
    display: flex;
    align-items: baseline;
    gap: 8px;
  }
  .obs-head {
    position: sticky;
    top: 0;
    z-index: 1;
    padding: 4px 0 3px;
    font-size: 9.5px;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--text-dim);
    background: var(--z1);
    border-bottom: 1px solid var(--stroke);
  }
  .obs-row {
    padding: 3px 0 3px 6px;
    border-left: 2px solid transparent;
    font-size: 11px;
    line-height: 1.45;
  }
  .obs-row:hover {
    background: var(--z1-raised);
  }
  .obs-row.error {
    border-left-color: var(--err);
  }
  .obs-row.error .obs-lvl {
    color: var(--err);
  }
  .obs-row.warn {
    border-left-color: var(--accent);
  }
  .obs-row.warn .obs-lvl {
    color: var(--accent);
  }
  .obs-row .obs-lvl {
    color: var(--text-dim);
  }
  .obs-cell {
    flex: 0 0 auto;
    white-space: nowrap;
  }
  .obs-time,
  .obs-dur,
  .obs-session {
    font-family: var(--mono);
    font-size: 10px;
    color: var(--text-dim);
  }
  .obs-type {
    font-family: var(--mono);
    font-size: 10.5px;
    min-width: 68px;
  }
  .obs-lvl {
    min-width: 34px;
    font-size: 9.5px;
    text-transform: uppercase;
  }
  .obs-tool {
    font-family: var(--mono);
    font-size: 10px;
    color: var(--text-dim);
    max-width: 110px;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .obs-sum {
    flex: 1 1 auto;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .obs-errline {
    flex: 1 0 100%;
    font-family: var(--mono);
    font-size: 10px;
    color: var(--err);
    white-space: pre-wrap;
    opacity: 0.85;
  }
  .obs-meta {
    margin: 0;
    padding: 4px 12px 8px;
    font-size: 10px;
    opacity: 0.45;
  }
  .obs-tail-line {
    margin: 0;
    font-family: var(--mono);
    font-size: 10.5px;
    line-height: 1.5;
    white-space: break-spaces;
    word-break: break-all;
    opacity: 0.75;
  }
  .obs-pane .obs-chip[aria-pressed="true"] {
    color: var(--text);
  }
  .obs-pane .obs-chip.off {
    opacity: 0.45;
  }
  /* the source family: one toggle per source, its own accent from the app's existing
     ladder (llm the accent, cron ok - a schedule's healthy tick, kanban the plain text,
     agent the dim base: the loop is most rows, so it is the quietest). --unread and
     --err are semantic-only and never decorate a source. */
  .obs-sources {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 2px;
    padding: 3px 12px 0;
    flex: none;
  }
  .obs-src {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    min-height: 18px;
    padding: 0 6px;
    border: 1px solid transparent;
    border-radius: var(--r-chip);
    background: transparent;
    color: var(--text-dim);
    font-family: var(--mono);
    font-size: 10.5px;
    cursor: pointer;
  }
  .obs-src:hover {
    background: var(--z2);
    color: var(--text);
  }
  .obs-src[aria-pressed="true"] {
    color: var(--src, var(--text));
    border-color: color-mix(in srgb, var(--src, var(--stroke)) 45%, transparent);
  }
  .obs-src.off {
    opacity: 0.45;
  }
  .obs-src-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--src, var(--text-dim));
    flex: none;
  }
  .obs-src.off .obs-src-dot {
    background: var(--text-dim);
    opacity: 0.5;
  }
  .obs-src.src-llm {
    --src: var(--accent);
  }
  .obs-src.src-agent {
    --src: var(--text-dim);
  }
  .obs-src.src-cron {
    --src: var(--ok);
  }
  .obs-src.src-kanban {
    --src: var(--text);
  }
  .obs-row.src-llm .obs-type {
    color: var(--accent);
  }
  .obs-row.src-cron .obs-type {
    color: var(--ok);
  }
  .obs-row.src-kanban .obs-type {
    color: var(--text);
  }
  .obs-row.src-agent .obs-type {
    color: var(--text-dim);
  }
  .obs-pane.narrow .obs-sources {
    padding: 2px 8px 0;
  }
  .obs-pane.narrow .obs-head {
    display: none;
  }
  .obs-pane.narrow .obs-row {
    flex-wrap: wrap;
    gap: 6px;
    padding-left: 4px;
  }
  .obs-pane.narrow .obs-sum {
    flex: 1 0 100%;
    white-space: normal;
  }
  .obs-pane.narrow .obs-tool {
    max-width: 76px;
  }
  .obs-pane.narrow .obs-body {
    padding: 4px 8px 6px;
  }
  .obs-pane.narrow .obs-meta {
    padding: 3px 8px 6px;
  }
</style>
