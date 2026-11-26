<template>
  <!-- The status bar, TWO zones. A right-click anywhere on the bar opens the stat menu
       (statusStats.ts): every stat with its check state, one click to hide or show it. -->
  <footer
    class="statusbar"
    @contextmenu="onStatMenu">
    <div class="zone left">
      <!--
        the connection control: the health dot and the connected instance's name.
        One click opens the panel - the connection list above the health facts.
        The dot is the state (ok / down, plus offline for a stale cached list);
        the name is the label, and it sheds on narrow while the dot never does.
      -->
      <button
        class="seg seg-btn health"
        type="button"
        :title="`${instanceName} - click for connections and server health`"
        :aria-label="`Connection: ${instanceName}`"
        aria-haspopup="dialog"
        :aria-expanded="healthOpen"
        @click="toggleHealth">
        <span
          class="dot"
          :class="[healthy ? 'ok' : 'down', { offline }]"
          aria-hidden="true" />
        <span class="hide-sm">{{ instanceName }}</span>
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
      <template v-if="shows('cache')">
      <span
        class="seg hide-sm"
        :class="{ muted: cachePercent == null }"
        :title="cacheTitle">
        {{ cacheLabel }}
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
      <UpdateChip />
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
        <!--
          the connection list: who this page is talking to, and the saved rows one
          click away. The list is settings' write surface - this panel only reads
          it and switches, so there is no add form and no remove button here.
        -->
        <div class="hp-conn">
          <span class="hp-head">connections</span>
          <div
            class="hp-crow hp-cur"
            role="group"
            aria-current="true"
            :title="connectionTitle(instanceName, currentOrigin)"
            :aria-label="connectionLabel(instanceName, currentOrigin)">
            <span
              v-if="stateOf(currentOrigin)"
              class="dot hp-dot"
              :class="dotClass(currentOrigin)"
              aria-hidden="true" />
            <span class="hp-cname">{{ instanceName }}</span>
            <span class="hp-curl">{{ currentOrigin || "this page" }}</span>
            <span
              v-if="onHome"
              class="hp-default">
              default
            </span>
            <span
              v-if="currentUnverified"
              class="hp-unverified"
              :title="UNVERIFIED_TITLE">
              unverified
            </span>
          </div>
          <button
            v-if="homeRow"
            type="button"
            class="hp-crow hp-switch"
            :title="connectionTitle(homeRow.name, homeRow.url)"
            :aria-label="connectionLabel(`Switch to ${homeRow.name}`, homeRow.url)"
            @click="switchTo(homeRow)">
            <span
              v-if="stateOf(homeRow.url)"
              class="dot hp-dot"
              :class="dotClass(homeRow.url)"
              aria-hidden="true" />
            <span class="hp-cname">{{ homeRow.name }}</span>
            <span class="hp-curl">{{ homeRow.url }}</span>
            <span class="hp-default">default</span>
          </button>
          <button
            v-for="row in savedRows"
            :key="row.url"
            type="button"
            class="hp-crow hp-switch"
            :title="connectionTitle(row.name, row.url)"
            :aria-label="connectionLabel(`Switch to ${row.name}`, row.url)"
            @click="switchTo(row)">
            <span
              v-if="stateOf(row.url)"
              class="dot hp-dot"
              :class="dotClass(row.url)"
              aria-hidden="true" />
            <span class="hp-cname">{{ row.name }}</span>
            <span class="hp-curl">{{ row.url }}</span>
            <span
              v-if="row.unverified"
              class="hp-unverified"
              :title="UNVERIFIED_TITLE">
              unverified
            </span>
            <span
              v-if="row.url === lastUsed"
              class="hp-used">last used</span>
          </button>
          <p
            v-if="statusNote"
            class="hp-note"
            role="status">
            {{ statusNote }}
          </p>
          <p
            v-if="offline"
            class="hp-note"
            role="status">
            offline - showing the cached list
          </p>
          <p
            v-else-if="!rows.length"
            class="hp-note">
            no saved connections yet
          </p>
        </div>
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
  import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
  import {
    formatDurationMs,
    shortVersion,
  } from "../../composables/omega";
  import { openContextMenu } from "../../composables/contextMenu";
  import { formatCacheShare } from "../../composables/tokenstat";
  import { statMenuItems, statVisible } from "./statusStats";
  import {
    INSTANCE_CHANGED_EVENT,
    instanceAddressKey,
    instancesAreAppOwned,
    lastUsedInstance,
    loadInstances,
    navigateToInstance,
    onInstancesReply,
    readInstanceCache,
    switchInstancesInApp,
    type SavedInstance,
  } from "../../composables/instances.ts";
  import {
    instanceReasons,
    startInstanceStatusPoll,
    stateFor,
    stateView,
    type InstanceState,
  } from "../../composables/instancesStatus.ts";
  import { RUNTIME_CONFIG_READY } from "../../composables/desktopChrome.ts";
  import WorkspaceSelect from "../chat/WorkspaceSelect.vue";
  import UpdateChip from "./UpdateChip.vue";

  const props = defineProps<{
    healthy: boolean;
    streaming: boolean;
    messageCount: number;
    tokens: number;
    // true while `tokens` carries a run-time estimate instead of reported usage
    tokensEstimated?: boolean;
    // the latest response's prompt-cache share (percent); null = not reported
    cachePercent?: number | null;
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
    /** the page's own origin: the saved row it matches is the current connection */
    currentOrigin: string;
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

  // the prompt-cache segment: the provider's own report, or the plain word for
  // its absence - a nil cache figure never renders as a measured zero.
  const cacheLabel = computed(() =>
    props.cachePercent == null
      ? `cache ${NOT_REPORTED}`
      : `${formatCacheShare(props.cachePercent)} cache`,
  );
  const cacheTitle = computed(() =>
    props.cachePercent == null
      ? "prompt cache: the provider reported no cache figure for the latest response"
      : "prompt cache hit share of the latest response",
  );

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

  // the connection control: the bar's dot and name ARE the switcher now. The
  // cache paints at setup (so the label is never blank) and the list's source replaces
  // it on mount; an unreachable source keeps the cached rows and the panel says the list
  // is stale. In a desktop window the source is the APP (the host's own list); a browser
  // tab reads its server's /instances.
  const cachedInstances = readInstanceCache();
  const rows = ref<SavedInstance[]>(cachedInstances.list);
  const lastUsed = ref(cachedInstances.lastUsed);
  const offline = ref(false);
  // the desktop's own home origin, from the app's answer. Home is derived state, never a
  // stored row: the app resolves it from the launch, and a browser tab has none - there
  // the page's own origin is the home.
  const homeOrigin = ref("");

  const homeName = computed(
    () => rows.value.find((row) => row.url === homeOrigin.value)?.name ?? "Home",
  );
  const onHome = computed(() => !!homeOrigin.value && props.currentOrigin === homeOrigin.value);
  const instanceName = computed(() => {
    if (onHome.value) return homeName.value;
    return rows.value.find((row) => row.url === props.currentOrigin)?.name ?? "This instance";
  });
  // the current connection has its own marked row; the rest are the switches. A stored row
  // naming the home address is the home row - one place never renders twice.
  const savedRows = computed(() =>
    rows.value.filter(
      (row) => row.url !== props.currentOrigin && !(homeOrigin.value && row.url === homeOrigin.value),
    ),
  );
  // the way back, first among the switches: the app's home when the page is not on it.
  const homeRow = computed<SavedInstance | null>(() =>
    homeOrigin.value && props.currentOrigin !== homeOrigin.value
      ? { name: homeName.value, url: homeOrigin.value }
      : null,
  );
  // the marker is one field on the row (the row type both surfaces read), never a colour.
  const UNVERIFIED_TITLE = "added anyway - nothing answered at this address when it was added";
  const currentUnverified = computed(
    () => rows.value.find((row) => row.url === props.currentOrigin)?.unverified === true,
  );

  // the panel's dots: ONE batch read of every listed address, started when the panel
  // opens and stopped when it closes - a closed panel probes nothing. The list and its
  // flags are the input; the serve answers one row per address.
  const statusStates = ref<Map<string, InstanceState>>(new Map());
  const statusReasons = ref<Map<string, string>>(new Map());
  const statusNote = ref("");
  const statusPoll = startInstanceStatusPoll({
    onReply: (reply, states) => {
      statusStates.value = states;
      statusReasons.value = instanceReasons(reply);
      statusNote.value = reply.note;
    },
  });
  const stateOf = (url: string): InstanceState | "" => stateFor(statusStates.value, url);
  // the batch's rows: the list plus this page's own connection, which may not be a saved
  // row at all (a desktop window's list is the app's) - the bar's row still gets a dot.
  const statusRows = computed<SavedInstance[]>(() => {
    const known = new Set(rows.value.map((row) => row.url));
    const extra: SavedInstance[] = [];
    if (homeOrigin.value && !known.has(homeOrigin.value)) {
      extra.push({ name: homeName.value, url: homeOrigin.value });
      known.add(homeOrigin.value);
    }
    const origin = props.currentOrigin;
    if (origin && !known.has(origin)) {
      extra.push({ name: instanceName.value, url: origin });
    }
    return extra.length ? [...rows.value, ...extra] : rows.value;
  });
  function stateWord(url: string): string {
    const state = stateOf(url);
    return state ? stateView(state).word : "";
  }
  function dotClass(url: string): string {
    const state = stateOf(url);
    return state ? stateView(state).cls : "";
  }
  // the reason rides the tooltip; a row with no answer yet carries the plain address
  function connectionTitle(name: string, url: string): string {
    if (!url) return name;
    const state = stateOf(url);
    if (!state) return `${name} - ${url}`;
    const reason = statusReasons.value.get(instanceAddressKey(url)) ?? "";
    return `${name} - ${url}\n${stateView(state).word}${reason ? `: ${reason}` : ""}`;
  }
  // the row's accessible name carries the state word - the dot itself is aria-hidden, so
  // the colour is never the only carrier
  function connectionLabel(name: string, url: string): string {
    const word = url ? stateWord(url) : "";
    return word ? `${name} - ${word}` : name;
  }
  function repaintStates(): void {
    if (statusPoll.running) void statusPoll.refresh(statusRows.value);
  }

  async function refreshInstances(): Promise<void> {
    const loaded = await loadInstances();
    rows.value = loaded.list;
    offline.value = loaded.offline;
    homeOrigin.value = loaded.home ?? "";
    lastUsed.value = lastUsedInstance();
    repaintStates();
  }

  // switching is a full reload: a browser tab leaves the page, the app moves its window.
  function switchTo(row: SavedInstance): void {
    lastUsed.value = row.url;
    healthOpen.value = false;
    if (instancesAreAppOwned()) {
      switchInstancesInApp(row.url);
      return;
    }
    navigateToInstance(row.url);
  }

  // The panel is a read of the list, so every answer the app emits repaints it, and a
  // browser tab's own save announces itself. One subscription each, taken at mount.
  const stopInstanceReplies = onInstancesReply((reply) => {
    rows.value = reply.instances;
    offline.value = false;
    homeOrigin.value = reply.home ?? "";
    lastUsed.value = lastUsedInstance();
    repaintStates();
  });
  function onInstancesChanged() {
    void refreshInstances();
  }

  // The Core shim is injected at NavigationCompleted, AFTER the SPA has mounted, so the
  // mount read cannot yet tell that the app owns the list and read the page's origin
  // instead. The shim announces itself; this re-reads from the app.
  function onRuntimeReady() {
    if (instancesAreAppOwned()) void refreshInstances();
  }

  const healthOpen = ref(false);
  function toggleHealth() {
    healthOpen.value = !healthOpen.value;
  }
  // OPEN the poll with the panel, CLOSE it with the panel: a shut panel costs no traffic.
  watch(healthOpen, (open) => {
    if (open) statusPoll.start(statusRows.value);
    else statusPoll.stop();
  });
  // outside click closes it (the WorkspaceSelect / ModelSelect family)
  function onDocClick(e: MouseEvent) {
    if (!healthOpen.value) return;
    if (!(e.target instanceof Element) || !e.target.closest(".health-pop, .health")) {
      healthOpen.value = false;
    }
  }
  onMounted(() => {
    document.addEventListener("mousedown", onDocClick);
    window.addEventListener(INSTANCE_CHANGED_EVENT, onInstancesChanged);
    window.addEventListener(RUNTIME_CONFIG_READY, onRuntimeReady);
    void refreshInstances();
  });
  onBeforeUnmount(() => {
    document.removeEventListener("mousedown", onDocClick);
    window.removeEventListener(INSTANCE_CHANGED_EVENT, onInstancesChanged);
    window.removeEventListener(RUNTIME_CONFIG_READY, onRuntimeReady);
    statusPoll.stop();
    stopInstanceReplies();
  });
</script>

<style scoped>
  /* the connection list, above the health facts: one column, each row a name and
     its address. The current row is marked and inert; a saved row IS the switch
     (a full reload on the other origin). */
  .hp-conn {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding-bottom: 8px;
    border-bottom: 1px solid var(--stroke);
  }
  .hp-head {
    margin: 0 4px 2px;
    font-size: 10.5px;
    color: var(--text-dim);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .hp-crow {
    display: flex;
    align-items: baseline;
    gap: 8px;
    min-width: 0;
    padding: 3px 4px;
    border-radius: var(--r-chip);
  }
  /* the row's state dot: shared .dot paint, centered on the row's line box */
  .hp-dot {
    flex: none;
    align-self: center;
  }
  .hp-cur {
    color: var(--text);
  }
  .hp-switch {
    background: 0 0;
    border: 0;
    font: inherit;
    text-align: left;
    color: var(--text-dim);
    cursor: pointer;
  }
  .hp-switch:hover {
    background: var(--z1-raised);
    color: var(--text);
  }
  .hp-cname {
    flex: 0 1 auto;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .hp-curl {
    flex: 1 1 auto;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 10.5px;
    opacity: 0.75;
  }
  .hp-used {
    flex: none;
    font-size: 10px;
    opacity: 0.7;
  }
  /* added anyway: text, never a colour - the tooltip says why the row is unproven. */
  .hp-unverified {
    flex: none;
    font-size: 9.5px;
    padding: 0 0.25rem;
    border: 1px solid var(--border, rgba(128, 128, 128, 0.35));
    border-radius: 4px;
    text-transform: uppercase;
    letter-spacing: 0.02em;
    opacity: 0.8;
    cursor: help;
  }
  /* the home row's mark: text, always rendered - the default is never a colour alone. */
  .hp-default {
    flex: none;
    font-size: 9.5px;
    padding: 0 0.25rem;
    border: 1px solid var(--border, rgba(128, 128, 128, 0.35));
    border-radius: 4px;
    text-transform: uppercase;
    letter-spacing: 0.02em;
    opacity: 0.8;
  }
  /* a stale cached list reads dim: absence is not a health claim. */
  .dot.offline {
    background: var(--text-dim);
    opacity: 0.6;
  }
  /*
   * the squeeze: below 480px the panel spans the bar instead of a 280px card
   * anchored at its left edge, so a 340px window keeps the whole sheet inside
   * the viewport (the name already sheds with .hide-sm). The panel stays a child
   * of the bar - absolute, not fixed - so it opens directly above it at every
   * width and never needs viewport math.
   */
  @media (max-width: 480px) {
    .health-pop {
      left: 8px;
      right: 8px;
      width: auto;
    }
  }
</style>
