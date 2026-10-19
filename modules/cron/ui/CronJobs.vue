<template>
  <section class="cron-list">
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
    <ul
      v-else-if="jobs.length"
      class="cron-jobs">
      <li
        v-for="j in jobs"
        :key="j.name"
        class="cron-row"
        :class="{ disabled: !j.enabled }">
        <div class="cron-head">
          <span class="cron-name">{{ j.name }}</span>
          <span
            class="cron-enabled"
            :class="j.enabled ? 'on' : 'off'">
            {{ j.enabled ? "enabled" : "disabled" }}
          </span>
        </div>
        <span class="cron-meta">{{ j.schedule }}</span>
        <span
          v-if="j.next"
          class="cron-meta">next {{ shortTime(j.next) }}</span>
      </li>
    </ul>
    <p
      v-else
      class="panel-status">
      no cron jobs (crons.yaml not activated)
    </p>
  </section>
</template>

<script setup lang="ts">
  import { onMounted, ref } from "vue";

  // Module-owned UI (card t_db430fea): the cron scheduler's job set,
  // from GET /cron/jobs. READ-ONLY v1 by settled decision - names,
  // schedules, enablement and next fire time render here; toggling and
  // editing stay where they belong (crons.yaml is human-authored, and
  // omega may only ever propose jobs). Self-contained fetch: a module's
  // UI carries its own wiring so it can ride the desktop / VS Code
  // builds alone (the ToolsList canary precedent).
  interface CronJobInfo {
    name: string;
    schedule: string;
    enabled: boolean;
    next: string;
  }
  const jobs = ref<CronJobInfo[]>([]);
  const loaded = ref(false);
  const error = ref("");

  // shortTime renders the RFC3339 next fire as a local, compact
  // date-time ("10/20 08:00") - the raw RFC string is noise in a row
  // that already carries the schedule.
  function shortTime(rfc3339: string): string {
    const t = new Date(rfc3339);
    if (Number.isNaN(t.getTime())) return rfc3339;
    const pad = (n: number) => String(n).padStart(2, "0");
    return `${pad(t.getMonth() + 1)}/${pad(t.getDate())} ${pad(t.getHours())}:${pad(t.getMinutes())}`;
  }

  onMounted(async () => {
    try {
      const res = await fetch("/cron/jobs");
      if (!res.ok) throw new Error(`GET /cron/jobs: ${res.status}`);
      jobs.value = await res.json();
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    }
    loaded.value = true;
  });
</script>

<style scoped>
  .cron-list {
    padding: 10px 12px 14px;
  }
  .panel-status {
    margin: 0;
    font-size: 11px;
    opacity: 0.6;
  }
  .cron-jobs {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .cron-row {
    display: flex;
    flex-direction: column;
    gap: 1px;
    padding: 5px 0;
    border-bottom: 1px solid color-mix(in srgb, currentcolor 10%, transparent);
  }
  .cron-row:last-child {
    border-bottom: none;
  }
  .cron-row.disabled {
    opacity: 0.5;
  }
  .cron-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 6px;
  }
  .cron-name {
    font-size: 12px;
    font-weight: 600;
    opacity: 0.9;
  }
  .cron-enabled {
    font-size: 10px;
  }
  .cron-enabled.on {
    color: var(--ok, #4caf50);
  }
  .cron-enabled.off {
    opacity: 0.55;
  }
  .cron-meta {
    font-size: 11px;
    opacity: 0.6;
  }
</style>