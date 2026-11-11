<template>
  <!-- The status bar, TWO zones (card t_8fd38f88, the settings-tier2
       round-1/2 ruling - VSCode's actual arrangement):
         LEFT  = work context: the health segment and the workdir selector
                 (moved OUT of the composer; the composer chip died with
                 it). The health dot leads the whole bar (card t_5656d52f).
         RIGHT = live metrics + the build/uptime segment, version LAST.
       The CLICKABLE SET IS CLOSED: workdir, health, pending. Every other
       segment is display-only. No model segment (the composer keeps the
       model + reasoning chips), no session label (the open tab names the
       session). Narrow (<=768px) collapses to the health dot + workdir:
       every other segment carries hide-sm. -->
  <footer class="statusbar">
    <div class="zone left">
      <!-- health leads the bar (card t_5656d52f): the dot (and its word on
           wide) stays visible on narrow (ruling 6); the click opens the
           detail popover (seam ruling), which anchors LEFT because its
           trigger does. -->
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
      <!-- the open session's message count, or the live tail -->
      <span class="seg hide-sm">
        <span
          v-if="streaming"
          class="streaming-seg"
          >streaming</span
        >
        <span v-else>{{ messageCount }} messages</span>
      </span>
      <!-- version LAST (card t_5656d52f): the rightmost thing on the bar -
           a position change only, shortVersion + uptime text untouched -->
      <span
        v-if="version"
        class="seg hide-sm"
        :title="`server build and uptime - ${version}`">
        {{ shortVersion(version) }} · up {{ formatUp }}
      </span>

      <!-- the health detail popover: the four ruled facts, each one
           rendered from its wire source and "not reported" when the body
           carries no source for it (an older server, a probe that has not
           landed yet, no store mounted). Uptime + build + server
           reachability ride the /health status fields (pushed every 5s);
           provider, database and last error ride the optional facts the
           Go side reports (agent/api.HealthState, card t_5e2f3095).
           Card t_5656d52f: its trigger leads the bar now, so the card
           anchors to the bar's LEFT edge (main.css .health-pop) - a
           right-anchored popover would open away from its own dot. -->
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
          <span
            v-if="providerSourced"
            class="hp-val"
            :class="{ down: !providerReachable }"
            :title="providerDetail">
            {{ providerReachable ? "reachable" : "not reachable" }}
            <span class="hp-age">{{ ageOf(providerCheckedAt) }}</span>
          </span>
          <span v-else class="hp-val muted">{{ NOT_REPORTED }}</span>
        </div>
        <div class="hp-row">
          <span class="hp-key">database</span>
          <span
            v-if="databaseSourced"
            class="hp-val"
            :class="{ down: !databaseHealthy }"
            :title="databaseDetail">
            {{ databaseHealthy ? "healthy" : "unhealthy" }}
          </span>
          <span v-else class="hp-val muted">{{ NOT_REPORTED }}</span>
        </div>
        <div class="hp-row">
          <span class="hp-key">uptime</span>
          <span class="hp-val">{{ formatUp }}</span>
        </div>
        <div class="hp-row">
          <span class="hp-key">last error</span>
          <span
            v-if="lastErrorSourced"
            class="hp-val"
            :class="{ down: !!lastErrorMessage }"
            :title="lastErrorMessage">
            {{ lastErrorMessage || "none" }}
            <span v-if="lastErrorMessage" class="hp-age">{{ ageOf(lastErrorAt) }}</span>
          </span>
          <span v-else class="hp-val muted">{{ NOT_REPORTED }}</span>
        </div>
        <p v-if="unsourcedCount" class="hp-note">
          {{ unsourcedCount === 1 ? "one fact has" : `${unsourcedCount} facts have` }}
          no source in this server's /health - an older build, or the
          probe has not landed yet.
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
    // --- the health popover's optional facts (card t_5e2f3095) ------------
    // Every one is optional ON PURPOSE: a shell served by an older build
    // (or a process whose probe has not landed yet) sends nothing, and
    // the row renders "not reported" instead of an invented value.
    // providerReachable: the /health body's provider.reachable verdict;
    // null/undefined = no source.
    providerReachable?: boolean | null;
    // unix seconds of that verdict (the row's "12s ago" freshness hint)
    providerCheckedAt?: number;
    // the probe's error text, shown as the row's tooltip when it failed
    providerDetail?: string;
    databaseHealthy?: boolean | null;
    databaseDetail?: string;
    // lastErrorMessage: "" is a SOURCED "no errors since boot" (the row
    // reads "none"); undefined/null means the server reports no such fact.
    lastErrorMessage?: string | null;
    lastErrorAt?: number;
  }>();

  defineEmits<{
    (e: "update:workdir", value: string): void;
    /** the pending segment's jump: focus the open session's pane */
    (e: "focus-pending"): void;
  }>();

  const NOT_REPORTED = "not reported";

  // --- the popover's sourced-vs-unsourced gates --------------------------
  // A boolean is the only "sourced" shape: the wire carries a real
  // verdict or nothing at all.
  const providerSourced = computed(() => typeof props.providerReachable === "boolean");
  const databaseSourced = computed(() => typeof props.databaseHealthy === "boolean");
  // An empty message is sourced ("none"): only an absent field is a gap.
  const lastErrorSourced = computed(() => typeof props.lastErrorMessage === "string");
  // The footnote appears only while some row is unsourced - a fully
  // sourced popover carries no apology.
  const unsourcedCount = computed(() => {
    let n = 0;
    if (!providerSourced.value) n++;
    if (!databaseSourced.value) n++;
    if (!lastErrorSourced.value) n++;
    return n;
  });

  // ageOf renders "12s ago" for a unix-seconds stamp; empty when the
  // fact carries no time (the row then shows the value alone).
  function ageOf(unix?: number): string {
    if (!unix) return "";
    const ms = Date.now() - unix * 1000;
    return ms > 0 ? `${formatDurationMs(ms)} ago` : "";
  }

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
