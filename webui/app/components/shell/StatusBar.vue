<template>
  <!-- The status bar, TWO zones (card t_8fd38f88, the settings-tier2
       round-1/2 ruling - VSCode's actual arrangement):
         LEFT  = work context: the workdir selector, moved OUT of the
                 composer (the composer chip died with it).
         RIGHT = live metrics + the build/uptime segment.
       The CLICKABLE SET IS CLOSED: workdir, health, pending. Every other
       segment is display-only. No model segment (the composer keeps the
       model + reasoning chips), no session label (the open tab names the
       session). Narrow (<=768px) collapses to the health dot + workdir:
       every other segment carries hide-sm. -->
  <footer class="statusbar">
    <div class="zone left">
      <WorkspaceSelect
        :workspace="workdir"
        variant="bar"
        :disabled="!workdirTarget"
        @update:workspace="$emit('update:workdir', $event)" />
    </div>

    <div class="zone right">
      <!-- the OPEN SESSION's pending calls (ruling 4): the count is the
           focused chat's own transcript, never a machine-wide number.
           Click = jump to the pane. -->
      <button
        v-if="pending > 0"
        class="seg seg-btn pending hide-sm"
        type="button"
        :title="`${pending} pending tool calls in the open session - click to focus the pane`"
        :aria-label="`${pending} pending tool calls, focus the session pane`"
        @click="$emit('focus-pending')">
        {{ pending }} pending
      </button>
      <span
        v-if="tokens"
        class="seg hide-sm"
        title="tokens used, active session">
        {{ tokens }} tok
      </span>
      <span
        v-if="contextSize"
        class="seg hide-sm"
        title="model context window">
        {{ formatCtx }} ctx
      </span>
      <span
        v-if="streaming && liveDeltasPerSecond"
        class="seg hide-sm"
        title="canvas delta repaints per second (live)">
        {{ liveDeltasPerSecond }} d/s
      </span>
      <span
        v-else-if="lastChunksPerSecond"
        class="seg hide-sm"
        title="chunks per second, last run">
        {{ lastChunksPerSecond }} ch/s
      </span>
      <span
        v-if="tokensPerSecond"
        class="seg hide-sm"
        title="generation speed, last run">
        {{ tokensPerSecond }} tok/s
      </span>
      <!-- health: the dot (and its word on wide) stays visible on narrow
           (ruling 6); the click opens the detail popover (seam ruling). -->
      <button
        class="seg seg-btn health"
        type="button"
        title="server health - click for detail"
        aria-label="server health"
        aria-haspopup="dialog"
        :aria-expanded="healthOpen"
        @click="toggleHealth">
        <span
          class="dot"
          :class="healthy ? 'ok' : 'down'"
          aria-hidden="true" />
        <span class="hide-sm">{{ healthy ? "online" : "offline" }}</span>
      </button>
      <span
        v-if="version"
        class="seg hide-sm"
        :title="`server build and uptime - ${version}`">
        {{ shortVersion(version) }} · up {{ formatUp }}
      </span>
      <span class="seg hide-sm">
        <span
          v-if="streaming"
          class="streaming-seg"
          >streaming</span
        >
        <span v-else>{{ messageCount }} messages</span>
      </span>

      <!-- the health detail popover: the four ruled facts, sourced where a
           source exists. Uptime + build + server reachability ride the
           real /health body (pushed every 5s). provider / database / last
           error have NO wire fact today - api/ws.go's healthState carries
           {status, uptime_seconds, version} only, and nothing else on the
           wire reports a provider probe, a db ping or a last error - so
           they render the honest not-reported state instead of an invented
           one, and the footnote names the gap. The server-side enrichment
           is a Go card of its own (this card's files are webui only). -->
      <div
        v-if="healthOpen"
        class="health-pop"
        role="dialog"
        aria-label="server health detail">
        <div class="hp-row">
          <span class="hp-key">server</span>
          <span
            class="hp-val"
            :class="{ down: !healthy }">
            {{ healthy ? "reachable" : "not reachable" }}
          </span>
        </div>
        <div class="hp-row">
          <span class="hp-key">provider</span>
          <span class="hp-val muted">{{ NOT_REPORTED }}</span>
        </div>
        <div class="hp-row">
          <span class="hp-key">database</span>
          <span class="hp-val muted">{{ NOT_REPORTED }}</span>
        </div>
        <div class="hp-row">
          <span class="hp-key">uptime</span>
          <span class="hp-val">{{ formatUp }}</span>
        </div>
        <div class="hp-row">
          <span class="hp-key">last error</span>
          <span class="hp-val muted">{{ NOT_REPORTED }}</span>
        </div>
        <p class="hp-note">
          provider, database and last error: this build's /health reports
          status, uptime and version only.
        </p>
        <div class="hp-foot">
          <span>{{ version || "build unknown" }}</span>
          <span>socket {{ socket }}</span>
        </div>
      </div>
    </div>
  </footer>
</template>

<script setup lang="ts">
  import { computed, onBeforeUnmount, onMounted, ref } from "vue";
  import {
    formatDurationMs,
    shortVersion,
  } from "../../composables/omega";
  import WorkspaceSelect from "../chat/WorkspaceSelect.vue";

  const props = defineProps<{
    healthy: boolean;
    streaming: boolean;
    messageCount: number;
    tokens: number;
    contextSize: number;
    tokensPerSecond: number;
    liveDeltasPerSecond: number;
    // last-known chunks/s (persisted last_run block, or the run that
    // just ended) - shown while no stream is live
    lastChunksPerSecond: number;
    // server build tag + uptime from /health; empty version hides the seg
    version: string;
    upSeconds: number;
    // the workdir segment's value: the focused chat's EFFECTIVE workdir
    // (the never-blank law, composables/defaultWorkspace.ts). Empty is
    // rendered by the picker as its own "no workspace" empty state.
    workdir: string;
    // false when no chat tab is focused: the picker shows the configured
    // default but has no session intent to write.
    workdirTarget: boolean;
    // the OPEN session's pending tool calls (0 hides the segment)
    pending: number;
    // the page's push-channel state (idle|connecting|open|reconnecting|closed)
    socket: string;
  }>();

  defineEmits<{
    (e: "update:workdir", value: string): void;
    /** the pending segment's jump: focus the open session's pane */
    (e: "focus-pending"): void;
  }>();

  const NOT_REPORTED = "not reported";

  // 1048576 -> "1M", 32768 -> "32k"
  const formatCtx = computed(() => {
    const n = props.contextSize;
    if (n >= 1_000_000) {
      const m = n / 1_000_000;
      return `${Number.isInteger(m) ? m : m.toFixed(1)}M`;
    }
    if (n >= 1_000) return `${Math.round(n / 1000)}k`;
    return String(n);
  });

  // uptime reads in the same voice as the elapsed meters ("2m 05s",
  // "1h 02m"); a fresh server reads "0s"
  const formatUp = computed(() => {
    const total = Math.floor(props.upSeconds);
    if (total < 1) return "0s";
    return formatDurationMs(total * 1000);
  });

  // --- the health popover (the one detail seam) ---------------------------
  const healthOpen = ref(false);
  function toggleHealth() {
    healthOpen.value = !healthOpen.value;
  }
  // outside click closes it (the WorkspaceSelect / ModelSelect family)
  function onDocClick(e: MouseEvent) {
    if (!healthOpen.value) return;
    if (!(e.target instanceof Element) || !e.target.closest(".health-pop, .health")) {
      healthOpen.value = false;
    }
  }
  onMounted(() => document.addEventListener("mousedown", onDocClick));
  onBeforeUnmount(() => document.removeEventListener("mousedown", onDocClick));
</script>
