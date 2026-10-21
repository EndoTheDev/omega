<template>
  <!-- card t_e1e61680: the /insights parity view. Hosts as a MAIN-area
       dock tab (registry entry: singleton, closable) - the width
       pressure ruling: this table set wants the wide main pane, not the
       ~300px inspector column, so it takes no `narrow` machinery (that
       recipe is for inspector surfaces). No chart dependency: honest
       rows and CSS-width bars, the same numbers the TUI prints. -->
  <section class="pane insights">
    <header class="insights-head">
      <div>
        <h2 class="insights-title">usage insights</h2>
        <p class="insights-period">
          {{ data ? data.period : "loading" }}<template v-if="data && data.period_start">: {{ data.period_start }} — {{ data.period_end }}</template>
        </p>
      </div>
      <div class="insights-controls">
        <label
          class="insights-days"
          title="window size - mirrors the TUI's /insights [days] arg">
          <select
            v-model.number="days"
            aria-label="window in days, 0 = all time">
            <option :value="7">7 days</option>
            <option :value="30">30 days</option>
            <option :value="90">90 days</option>
            <option :value="0">all time</option>
          </select>
        </label>
        <button
          class="insights-refresh"
          title="recompute"
          :disabled="loading"
          @click="load()">&#8635;</button>
      </div>
    </header>

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
      <!-- overview: the TUI report's Overview block -->
      <div class="insights-grid">
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

        <!-- activity by day: honest CSS bars, one row per weekday -->
        <section class="insights-box">
          <h3>activity by day</h3>
          <table class="insights-table">
            <tbody>
              <tr
                v-for="d in daily"
                :key="d.day">
                <th scope="row">{{ d.day }}</th>
                <td class="insights-bar-cell">
                  <div
                    class="insights-bar"
                    :style="{ width: barPct(d.count) + '%' }" />
                </td>
                <td class="insights-count">{{ count(d.count) }}</td>
              </tr>
            </tbody>
          </table>
        </section>

        <!-- top tools + notable sessions (hidden when zero, like the TUI) -->
        <section
          v-if="data.tools.length"
          class="insights-box">
          <h3>top tools</h3>
          <table class="insights-table">
            <tbody>
              <tr
                v-for="t in data.tools"
                :key="t.name">
                <th scope="row">{{ t.name }}</th>
                <td class="insights-bar-cell">
                  <div
                    class="insights-bar tool"
                    :style="{ width: toolPct(t.count) + '%' }" />
                </td>
                <td class="insights-count">{{ count(t.count) }}</td>
                <td class="insights-pct">{{ toolShare(t.count) }}%</td>
              </tr>
            </tbody>
          </table>
        </section>
        <section
          v-if="notables.length"
          class="insights-box">
          <h3>notable sessions</h3>
          <dl class="insights-overview">
            <div
              v-for="n in notables"
              :key="n.label">
              <dt>{{ n.label }}</dt>
              <dd>{{ count(n.row.value) }} <span class="insights-detail">({{ n.row.detail }})</span></dd>
            </div>
          </dl>
        </section>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
  import { computed, onMounted, ref, watch } from "vue";
  import {
    fetchInsights,
    InsightsPayload,
    InsightsDayRow,
  } from "../../composables/omega";

  const data = ref<InsightsPayload | null>(null);
  const loading = ref(false);
  const loadError = ref<string | null>(null);
  // null until first load: the request goes out with no param, exactly
  // like a bare `/insights` (server default window = the TUI default 30)
  const days = ref<number | null>(null);

  const emptyText = computed(() =>
    days.value === 0 ? "no sessions yet" : `no sessions in the last ${days.value ?? 30} days`,
  );

  const daily = computed<InsightsDayRow[]>(() =>
    data.value ? [...data.value.daily].sort((a, b) => a.count - b.count) : [],
  );
  const dailyMax = computed(() => Math.max(...daily.value.map((d) => d.count), 1));
  const barPct = (c: number) => (c / dailyMax.value) * 100;

  const toolMax = computed(() => Math.max(...(data.value?.tools ?? []).map((t) => t.count), 1));
  const toolPct = (c: number) => (c / toolMax.value) * 100;
  const toolShare = (c: number) =>
    data.value && data.value.tool_calls > 0
      ? Math.round((c / data.value.tool_calls) * 1000) / 10
      : 0;

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

  // thousands separators, matching the TUI's FormatNumber output
  const count = (n: number): string => n.toLocaleString("en-US");

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
  .insights {
    display: flex;
    flex-direction: column;
    overflow-y: auto;
    padding: 10px 14px;
    gap: 10px;
  }
  .insights-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 10px;
  }
  .insights-title {
    margin: 0;
    font-size: 13px;
    font-weight: 600;
    color: var(--text);
  }
  .insights-period {
    margin: 2px 0 0;
    font-size: 11px;
    color: var(--text-dim);
  }
  .insights-controls {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .insights-days select {
    background: var(--z1-raised);
    color: var(--text);
    border: 1px solid var(--stroke);
    border-radius: 6px;
    font-size: 11px;
    padding: 3px 6px;
  }
  .insights-refresh {
    background: none;
    border: none;
    color: var(--text-dim);
    font-size: 14px;
    cursor: pointer;
    padding: 2px 4px;
  }
  .insights-refresh:hover { color: var(--text); }

  .insights-status,
  .insights-error {
    margin: 6px 2px;
    font-size: 12px;
    color: var(--text-dim);
  }

  .insights-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
    gap: 10px;
    align-content: start;
  }
  .insights-box {
    border: 1px solid var(--z1-raised);
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
  .insights-detail { color: var(--text-dim); font-size: 11px; }

  .insights-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 12px;
  }
  .insights-table th {
    text-align: left;
    font-weight: 400;
    color: var(--text);
    padding: 2px 6px 2px 0;
    white-space: nowrap;
  }
  .insights-bar-cell {
    width: 100%;
    min-width: 60px;
    padding: 2px 6px 2px 0;
  }
  .insights-bar {
    height: 8px;
    min-width: 1px;
    border-radius: 3px;
    background: color-mix(in srgb, var(--accent) 60%, transparent);
  }
  .insights-bar.tool {
    background: color-mix(in srgb, var(--accent) 35%, transparent);
  }
  .insights-count,
  .insights-pct {
    text-align: right;
    color: var(--text);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .insights-pct { color: var(--text-dim); }
</style>