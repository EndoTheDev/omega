<template>
  <!-- card t_e1e61680: the /insights parity view. card t_d7fd3ad3: the
       home moved - insights is a default-visible SECONDARY-sidebar tab
       now (registry: singleton, unclosable, area "right"), superseding
       the main-area width ruling. v0.14.3: the body references the
       skills/tools tabs - the shared RegistryList (v0.14.1) renders
       every block but the period summary as read-only rows (no fold:
       showState=false), with the honest CSS bars riding the row's
       #side slot. The same numbers the TUI prints, the browser
       pattern's look. No chart dependency. -->
  <section class="pane insights">
    <!-- card t_308e6d9b: the UNIVERSAL band (the shared TabBand) - the
         window selector + recompute ride the band now; the period text
         rides the band's small print. The band renders before the
         .insights-scroll body: scrolling the dashboard never moves it. -->
    <TabBand
      title="Insights"
      :title-attr="data ? `${data.period}${data.period_start ? ': ' + data.period_start + ' — ' + data.period_end : ''}` : undefined">
      <span
        class="insights-period"
        :title="data ? data.period : 'loading'">{{ data ? data.period : "loading" }}</span>
      <!-- card t_acad02ec (audit M1): the window selector rides the themed
           listbox (BandSelect) - a native select popup is OS chrome over
           the pane. The fixed literals below are the devProxy contract
           (nuxt.config devProxy keys /insights?days=7|30|90|0). -->
      <BandSelect
        class="insights-days"
        :model-value="days ?? INSIGHTS_DEFAULT_DAYS"
        :options="WINDOW_OPTIONS"
        label="window in days, 0 = all time"
        drop="down"
        @update:model-value="days = $event" />
      <button
        class="insights-refresh band-tool"
        title="recompute"
        aria-label="recompute insights"
        :disabled="loading"
        @click="load()">
        <!-- card t_acad02ec (audit M7): the recompute glyph rides the Icon
             vocabulary - no raw HTML entities -->
        <Icon
          name="refresh"
          :size="13" />
      </button>
    </TabBand>

    <div class="insights-scroll">
    <p
      v-if="loadError"
      class="settings-error insights-error">
      {{ loadError }}
    </p>
    <p
      v-else-if="loading"
      class="insights-status">
      loading…
    </p>
    <p
      v-else-if="data && data.sessions === 0"
      class="insights-status">
      {{ emptyText }}
    </p>

    <div
      v-else-if="data"
      class="insights-body">
      <!-- overview: the TUI report's Overview block - the period
           summary keeps its stat rows; the rest of the body is the
           registry browser's list/row/interaction pattern (v0.14.3,
           the skills/tools tabs' shared RegistryList). -->
      <section class="insights-box">
        <h3>overview</h3>
        <dl class="insights-overview">
          <div><dt>sessions</dt><dd>{{ count(data.sessions) }}</dd></div>
          <div><dt>messages</dt><dd>{{ count(data.messages) }}</dd></div>
          <div><dt>user messages</dt><dd>{{ count(data.user_messages) }}</dd></div>
          <div><dt>tool calls</dt><dd>{{ count(data.tool_calls) }}</dd></div>
          <div><dt>tokens (est.)</dt><dd>{{ count(data.total_tokens) }}</dd></div>
          <div><dt>avg msgs/session</dt><dd>{{ data.avg_session_msgs.toFixed(1) }}</dd></div>
        </dl>
      </section>

      <!-- activity by day: one registry row per weekday, the honest CSS
           bar rides the row's #side slot; the expanded detail names the
           row's raw count (TUI parity: the same numbers the TUI prints) -->
      <h3 class="insights-group">activity by day</h3>
      <RegistryList
        kind="insights"
        :rows="dailyRows"
        :loaded="!!data"
        :show-state="false"
        empty-text="no activity recorded">
        <template #side="{ row }">
          <span
            class="insights-bar"
            :style="{ width: barPct(row.value) + '%' }" />
          <span class="insights-count">{{ count(row.value) }}</span>
        </template>
      </RegistryList>

      <!-- top tools (hidden when zero, like the TUI): registry rows with
           call bars; the expanded detail carries the share of all calls -->
      <template v-if="data.tools.length">
        <h3 class="insights-group">top tools</h3>
        <RegistryList
          kind="insights"
          :rows="toolRows"
          :loaded="!!data"
          :show-state="false"
          empty-text="no tool calls yet">
          <template #side="{ row }">
            <span
              class="insights-bar tool"
              :style="{ width: toolPct(row.value) + '%' }" />
            <span class="insights-count">{{ count(row.value) }}</span>
          </template>
        </RegistryList>
      </template>

      <!-- notable sessions (hidden when zero, like the TUI): the label
           row expands into the row's own detail line -->
      <template v-if="notables.length">
        <h3 class="insights-group">notable sessions</h3>
        <RegistryList
          kind="insights"
          :rows="notableRows"
          :loaded="!!data"
          :show-state="false"
          empty-text="no notable sessions" />
      </template>
    </div>
    </div>
  </section>
</template>

<script setup lang="ts">
  import { computed, onMounted, ref, watch } from "vue";
  import RegistryList from "../RegistryList.vue";
  import Icon from "../ui/Icon.vue";
  import {
    fetchInsights,
    InsightsPayload,
    InsightsDayRow,
    type RegistryRow,
  } from "../../composables/omega";
  import { INSIGHTS_DEFAULT_DAYS } from "./defaultWindow";

  const data = ref<InsightsPayload | null>(null);
  const loading = ref(false);
  const loadError = ref<string | null>(null);
  // initial window = 7 days (see ./defaultWindow.ts) - NOT the server
  // default 30 a bare `/insights` gets. null only survives for the
  // load() fold; a user pick (30/90/all) wins for the tab's lifetime.
  const days = ref<number | null>(INSIGHTS_DEFAULT_DAYS);

  // the fixed window literals (the TUI's /insights [days] arg): the band
  // select's options. days=0 is "all time" - the server's bare /insights
  // default (30) is deliberately NOT an option (card t_d7fd3ad3).
  const WINDOW_OPTIONS = [
    { value: 7, label: "7 days" },
    { value: 30, label: "30 days" },
    { value: 90, label: "90 days" },
    { value: 0, label: "all time" },
  ];

  const emptyText = computed(() =>
    days.value === 0 ? "no sessions yet" : `no sessions in the last ${days.value ?? 30} days`,
  );

  const daily = computed<InsightsDayRow[]>(() =>
    data.value ? [...data.value.daily].sort((a, b) => a.count - b.count) : [],
  );

  // --- the registry-browser row models (v0.14.3, the skills/tools
  // pattern on the TUI report's blocks): every block renders through
  // the shared RegistryList - read-only rows (no fold), the #side slot
  // carries the honest CSS bar + count the plain name line can't. ---
  const dailyRows = computed<RegistryRow[]>(() =>
    daily.value.map((d) => ({
      name: d.day,
      detail: [`${count(d.count)} ${d.count === 1 ? "message" : "messages"}`],
      disabled: false,
      value: d.count,
    })),
  );

  const toolMax = computed(() => Math.max(...(data.value?.tools ?? []).map((t) => t.count), 1));
  const toolPct = (c: number) => (c / toolMax.value) * 100;
  const toolShare = (c: number) =>
    data.value && data.value.tool_calls > 0
      ? Math.round((c / data.value.tool_calls) * 1000) / 10
      : 0;

  const toolRows = computed<RegistryRow[]>(() =>
    (data.value?.tools ?? []).map((t) => ({
      name: t.name,
      detail: [`${toolShare(t.count)}% of all tool calls`],
      disabled: false,
      value: t.count,
    })),
  );

  // notable sessions: a triple of [label, stat]; rows with value 0 are
  // omitted - the TUI prints nothing for them, so neither does this.
  const notables = computed(() => {
    if (!data.value) return [];
    return [
      { label: "most messages", row: data.value.notable_msgs },
      { label: "most tokens", row: data.value.notable_tokens },
      { label: "most tool calls", row: data.value.notable_tools },
    ].filter((n) => n.row.value > 0);
  });

  const notableRows = computed<RegistryRow[]>(() =>
    notables.value.map((n) => ({
      name: n.label,
      description: `${count(n.row.value)}`,
      detail: [n.row.detail || "no further detail"],
      disabled: false,
      value: n.row.value,
    })),
  );

  const dailyMax = () => Math.max(...daily.value.map((d) => d.count), 1);
  const barPct = (c: number | undefined) => ((c ?? 0) / dailyMax()) * 100;

  // thousands separators, matching the TUI's FormatNumber output
  const count = (n: number | undefined): string =>
    (n ?? 0).toLocaleString("en-US");

  async function load() {
    loading.value = true;
    loadError.value = null;
    try {
      data.value = await fetchInsights(
        days.value === null ? null : days.value,
      );
    } catch (err) {
      loadError.value = err instanceof Error ? err.message : String(err);
    } finally {
      loading.value = false;
    }
  }

  // the select's model drives refetch (null only shows on first load)
  watch(days, () => load());
  onMounted(load);
</script>

<style scoped>
  /* card t_308e6d9b: the root stops being the scrollport - the band
     anchors its top, the .insights-scroll wrapper under it owns the
     scroll; the old root padding rides the wrapper. */
  .insights {
    flex: 1;
    min-height: 0;
    min-width: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    padding: 0 14px 10px;
  }
  .insights-scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    overflow-x: hidden;
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding-top: 4px;
  }
  .insights-period {
    margin: 0;
    font-size: 11px;
    color: var(--text-dim);
    white-space: nowrap;
  }
  /* card t_acad02ec (audit M1): the window select rides the shared
     BandSelect chip now - no native select chrome; only seating rides
     here (the chip keeps the shared .model-chip look). */
  .insights-days {
    flex-shrink: 0;
  }
  .insights-days :deep(.model-chip) {
    padding: 2px 6px;
    font-size: 11px;
    /* the band's dim look: transparent at rest, the chip's hover raise
       stays (mirrors .band-tool) */
    background: transparent;
  }
  .insights-refresh {
    padding: 0 5px;
  }

  .insights-status,
  .insights-error {
    margin: 6px 2px;
    font-size: 12px;
    color: var(--text-dim);
  }

  /* the registry-browser body: group subheads over shared
     RegistryList sections (v0.14.3); the overview keeps its stat box */
  .insights-group {
    margin: 2px 0 0;
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--text-dim);
    padding: 0 2px;
  }
  .insights-body > .insights-group + .registry-list {
    padding-top: 2px;
  }
  .insights-box {
    /* card t_acad02ec (audit M7): the stroke token - --z1-raised is a
       surface fill, not a border color */
    border: 1px solid var(--stroke);
    border-radius: var(--r-pane);
    background: var(--z0);
    padding: 8px 10px;
  }
  .insights-box h3 {
    margin: 0 0 6px;
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--text-dim);
  }
  .insights-overview {
    margin: 0;
  }
  .insights-overview > div {
    display: flex;
    justify-content: space-between;
    gap: 8px;
    padding: 2px 0;
    font-size: 12px;
  }
  .insights-overview dt { color: var(--text-dim); margin: 0; }
  .insights-overview dd {
    margin: 0;
    color: var(--text);
    font-variant-numeric: tabular-nums;
  }

  /* the row's honest bar + count, riding the row's #side slot: the
     same CSS-width bars the TUI prints, no chart dependency */
  .insights-bar {
    flex: none;
    height: 8px;
    min-width: 1px;
    max-width: 96px;
    /* card t_acad02ec (audit M7): the 8/6/4 radius ladder - 3px is off
       it */
    border-radius: var(--r-chip);
    background: color-mix(in srgb, var(--accent) 60%, transparent);
    align-self: center;
  }
  .insights-bar.tool {
    background: color-mix(in srgb, var(--accent) 35%, transparent);
  }
  .insights-count {
    flex: none;
    color: var(--text);
    font-size: 11.5px;
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
</style>