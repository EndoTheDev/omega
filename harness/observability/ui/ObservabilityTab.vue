<template>
  <!-- the observability TAB - the face of the observability capsule. -->
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
      <!--
        the errors-only chip: a view filter over the fetched page, never a server.
      -->
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
      <!-- the raw tail: the SECONDARY view inside this tab -->
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
    </TabBand>

    <p
      v-if="readError"
      class="panel-status obs-error">
      {{ readError }}
    </p>

    <!-- the PRIMARY view: one row per structured event -->
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
          :class="[e.level, { allscope: allScope }]">
          <span
            v-if="allScope"
            class="obs-cell obs-session"
            :title="e.session || 'no session (one-shot run)'">
            {{ shortSession(e.session) }}
          </span>
          <span class="obs-cell obs-time" :title="e.time">{{ formatEventTime(e.time) }}</span>
          <span class="obs-cell obs-type">{{ e.type }}</span>
          <span class="obs-cell obs-lvl">{{ e.level }}</span>
          <span class="obs-cell obs-tool">{{ e.tool || "" }}</span>
          <span class="obs-cell obs-dur">{{ formatDuration(e.duration_ms) }}</span>
          <span class="obs-cell obs-sum" :title="e.summary">{{ e.summary }}</span>
          <span v-if="e.error" class="obs-cell obs-errline">{{ e.error }}</span>
        </div>
      </template>
      <p v-else-if="!loaded" class="panel-status">loading</p>
      <p v-else-if="errorsOnly && events.length" class="panel-status">
        no errors in the ring
      </p>
      <p v-else class="panel-status">
        no events recorded yet - the ring fills as runs execute
      </p>
    </div>

    <!-- the SECONDARY view: the raw tail, verbatim -->
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
    formatDuration,
    formatEventTime,
    LOGS_PATH,
    logsQuery,
    parseEventsPayload,
    parseLogsPayload,
    REFRESH_MS,
    shortSession,
    visibleEvents,
    type ObservatoryEvent,
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

  const shown = computed(() => visibleEvents(events.value, errorsOnly.value));

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
    return `${count} · ${errs} error${errs === 1 ? "" : "s"} · ${ring}`;
  });

  // the two reads --------------------------------------------------- One in-flight
  // read at a time: a scope flip while a fetch is airborne must not paint the OLD
  // scope's answer over the new one.
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

  // a scope flip or a tail flip re-reads IMMEDIATELY.
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
      // the flip changes the observed layout itself: mutate on the next frame, never.
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
  /* errors are VISUALLY distinct. */
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
