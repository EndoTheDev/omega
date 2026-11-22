<template>
  <!-- The status bar, TWO zones. A right-click anywhere on the bar opens the stat menu
       (statusStats.ts): every stat with its check state, one click to hide or show it. -->
  <footer
    class="statusbar"
    @contextmenu="onStatMenu">
    <div class="zone left">
      <!--
        health leads the bar: the dot (and its word on wide) stays visible on narrow.
      -->
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
      <!--
        the OPEN SESSION's pending calls: the count is the focused chat's own
        transcript, never a machine-wide number.
      -->
      <template v-if="shows('pending')">
      <button
        v-if="pending > 0"
        class="seg seg-btn pending hide-sm"
        type="button"
        :title="`${pending} pending tool calls in the open session - click to focus the pane`"
        :aria-label="`${pending} pending tool calls, focus the session pane`"
        @click="$emit('focus-pending')">
        {{ pending }} pending
      </button>
      </template>
      <template v-if="shows('tokens')">
      <span
        v-if="tokens"
        class="seg hide-sm"
        title="tokens used, active session">
        {{ tokensEstimated ? "~" : "" }}{{ tokens }} tok
      </span>
      </template>
      <template v-if="shows('context')">
      <span
        v-if="contextSize"
        class="seg hide-sm"
        title="model context window">
        {{ formatCtx }} ctx
      </span>
      </template>
      <template v-if="shows('throughput')">
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
      </template>
      <template v-if="shows('speed')">
      <span
        v-if="tokensPerSecond"
        class="seg hide-sm"
        title="generation speed, last run">
        {{ tokensPerSecond }} tok/s
      </span>
      </template>
      <template v-if="shows('messages')">
      <span class="seg hide-sm">
        <span
          v-if="streaming"
          class="streaming-seg"
          >streaming</span
        >
        <span v-else>{{ messageCount }} messages</span>
      </span>
      </template>
      <!--
        version LAST: the rightmost thing on the bar.
        The cap rides the inner span, never this.seg: a display declaration here would
        out-rule.hide-sm and put the build back on the narrow bar (the seg itself stays
        flex + hide-sm).
      -->
      <template v-if="shows('version')">
      <span
        v-if="version"
        class="seg hide-sm"
        :title="`server build - ${version}`">
        <span class="build-text">{{ shortVersion(version) }}</span>
      </span>
      </template>

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
  import { openContextMenu } from "../../composables/contextMenu";
  import { statMenuItems, statVisible } from "./statusStats";
  import WorkspaceSelect from "../chat/WorkspaceSelect.vue";

  const props = defineProps<{
    healthy: boolean;
    streaming: boolean;
    messageCount: number;
    tokens: number;
    // true while `tokens` carries a run-time estimate instead of reported usage
    tokensEstimated?: boolean;
    contextSize: number;
    tokensPerSecond: number;
    liveDeltasPerSecond: number;
    lastChunksPerSecond: number;
    version: string;
    upSeconds: number;
    // the workdir segment's value: the focused chat's EFFECTIVE workdir (the
    // never-blank law, composables/defaultWorkspace.ts).
    workdir: string;
    workdirTarget: boolean;
    pending: number;
    socket: string;
    /** the persisted hidden stat ids (the layout record's set); absent = every stat shows */
    hiddenStats?: readonly string[];
    providerReachable?: boolean | null;
    providerCheckedAt?: number;
    providerDetail?: string;
    databaseHealthy?: boolean | null;
    databaseDetail?: string;
    lastErrorMessage?: string | null;
    lastErrorAt?: number;
  }>();

  const emits = defineEmits<{
    (e: "update:workdir", value: string): void;
    (e: "focus-pending"): void;
    /** the stat menu's one write: the host persists the hidden set in the layout record */
    (e: "toggle-stat", id: string, hidden: boolean): void;
  }>();

  const NOT_REPORTED = "not reported";

  const shows = (id: string) => statVisible(props.hiddenStats ?? [], id);

  // the bar's whole inventory in one menu; a right-click is always the same door. The
  // Reset row rides the SAME toggle writer - one emit per hidden id, so the host's single
  // setStatHidden persists the cleared set exactly as a manual un-toggle would.
  function onStatMenu(e: MouseEvent) {
    e.preventDefault();
    void openContextMenu(
      e.clientX,
      e.clientY,
      statMenuItems(props.hiddenStats ?? [], (id, hidden) => {
        emits("toggle-stat", id, hidden);
      }),
    );
  }

  const providerSourced = computed(() => typeof props.providerReachable === "boolean");
  const databaseSourced = computed(() => typeof props.databaseHealthy === "boolean");
  const lastErrorSourced = computed(() => typeof props.lastErrorMessage === "string");
  const unsourcedCount = computed(() => {
    let n = 0;
    if (!providerSourced.value) n++;
    if (!databaseSourced.value) n++;
    if (!lastErrorSourced.value) n++;
    return n;
  });

  function ageOf(unix?: number): string {
    if (!unix) return "";
    const ms = Date.now() - unix * 1000;
    return ms > 0 ? `${formatDurationMs(ms)} ago` : "";
  }

  const formatCtx = computed(() => {
    const n = props.contextSize;
    if (n >= 1_000_000) {
      const m = n / 1_000_000;
      return `${Number.isInteger(m) ? m : m.toFixed(1)}M`;
    }
    if (n >= 1_000) return `${Math.round(n / 1000)}k`;
    return String(n);
  });

  const formatUp = computed(() => {
    const total = Math.floor(props.upSeconds);
    if (total < 1) return "0s";
    return formatDurationMs(total * 1000);
  });

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
