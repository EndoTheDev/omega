<template>
  <section class="pane insights">
    <!--
      the UNIVERSAL band (the shared TabBand).
      The band renders before the.insights-scroll body: scrolling the dashboard never
      moves it.
    -->
    <TabBand
      title="Insights"
      :title-attr="data ? `${data.period}${data.period_start ? ': ' + data.period_start + ' — ' + data.period_end : ''}` : undefined">
      <!--
        (audit M1): the window selector rides the themed listbox (BandSelect).
        The fixed literals below are the devProxy contract (nuxt.config devProxy keys
        /insights?days=7|30|90|0).
      -->
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
        <!-- (audit M7): the recompute glyph rides the Icon vocabulary. -->
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
      <!-- overview: the /insights report's Overview block. -->
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

      <!--
        activity by day: one registry row per weekday, the honest CSS bar rides the
        row's #side slot.
      -->
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

      <!-- top tools (hidden when zero, like the /insights report). -->
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

      <!-- notable sessions (hidden when zero, like the /insights report). -->
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
  import RegistryList from "~/components/RegistryList.vue";
  import Icon from "~/components/ui/Icon.vue";
  import {
    fetchInsights,
    InsightsPayload,
    InsightsDayRow,
    type RegistryRow,
  } from "~/composables/omega";
  import { INSIGHTS_DEFAULT_DAYS } from "./defaultWindow";

  const data = ref<InsightsPayload | null>(null);
  const loading = ref(false);
  const loadError = ref<string | null>(null);
  // initial window = 7 days (see./defaultWindow.ts).
  const days = ref<number | null>(INSIGHTS_DEFAULT_DAYS);

  // the fixed window literals (the /insights [days] arg): the band select's options.
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

  // -- the registry-browser row models (v0.14.3, the skills/tools pattern on the
  // /insights report's blocks): every block renders through the shared RegistryList -
  // read-only rows (no fold), the #side slot carries the honest CSS bar + count the
  // plain name line can't.
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

  // notable sessions: a triple of [label, stat]; rows with value 0 are omitted.
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

  watch(days, () => load());
  onMounted(load);
</script>

<style scoped>
  /* the root stops being the scrollport. */
  /* the root stays padding-free. */
  .insights {
    flex: 1;
    min-height: 0;
    min-width: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }
  .insights-scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    overflow-x: hidden;
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 4px 14px 10px;
  }
  /* (audit M1): the window select rides the shared BandSelect chip now. */
  .insights-days {
    flex-shrink: 0;
  }
  .insights-days :deep(.model-chip) {
    padding: 2px 6px;
    font-size: 11px;
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

  /* the registry-browser body. */
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

  /* the row's honest bar + count, riding the row's #side slot. */
  .insights-bar {
    flex: none;
    height: 8px;
    min-width: 1px;
    max-width: 96px;
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