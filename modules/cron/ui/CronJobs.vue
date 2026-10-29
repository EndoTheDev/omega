<template>
  <section
    ref="rootEl"
    class="cron-list"
    :class="{ narrow: narrow }">
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
    <!-- card t_6072e718: the empty state is the designed pattern, not a
         bare corner status line - centered in the panel body, the cron
         clock glyph above the headline, and the crons.yaml activation
         hint below it. Activation is a file action, so there is no
         call-to-action button. The constants live in emptyState.ts (the
         composition-test convention: the node suite pins them without
         compiling this SFC). -->
    <div
      v-else
      class="cron-empty">
      <Icon
        class="cron-empty-icon"
        name="cron"
        :size="20" />
      <p class="cron-empty-text">{{ CRON_EMPTY_TEXT }}</p>
      <p class="cron-empty-hint">{{ CRON_EMPTY_HINT }}</p>
    </div>
  </section>
</template>

<script setup lang="ts">
  import { onBeforeUnmount, onMounted, ref } from "vue";
  import { isNarrowWidth } from "./narrow";
  // the empty-state copy lives in its own PURE module (card t_6072e718,
  // the serverRows.ts / boardLogic.ts convention): the node suite pins
  // it without compiling this SFC.
  import { CRON_EMPTY_HINT, CRON_EMPTY_TEXT } from "./emptyState";

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

  // --- narrow adaptation, the module-UI recipe (card t_a2fdc630): this
  // root measures its OWN rendered container (the inspector column it
  // currently rides in) and flips the class; the scoped stylesheet
  // below condenses. Same mechanism as the shell's useNarrow - kept
  // in-module because module UI imports no shell code (Board
  // precedent); the threshold is unit-covered in ./narrow.ts.
  const rootEl = ref<HTMLElement | null>(null);
  const narrow = ref(false);
  let resizeObs: ResizeObserver | null = null;
  onMounted(() => {
    if (typeof ResizeObserver === "undefined" || !rootEl.value) return;
    // (same rAF deferral as the shell's useNarrow: the flip changes the
    // observed layout itself, so the callback must not mutate this frame)
    resizeObs = new ResizeObserver((entries) => {
      const pending = entries
        .map((e) => Math.round(e.contentRect.width))
        .filter((w) => w > 0);
      if (pending.length === 0) return;
      requestAnimationFrame(() => {
        if (resizeObs === null) return;
        for (const w of pending) narrow.value = isNarrowWidth(w);
      });
    });
    resizeObs.observe(rootEl.value);
    // mount measures NOW: a tab remount in place produces no further
    // resize entry, so the class must not wait for the next width
    // change (the shell's useNarrow does the same on mount)
    narrow.value = isNarrowWidth(Math.round(rootEl.value.getBoundingClientRect().width));
  });
  onBeforeUnmount(() => {
    resizeObs?.disconnect();
    resizeObs = null;
  });

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
    /* the empty state centers vertically in the panel body: the root
       owns the full height of the .module-panels scroll region (card
       t_6072e718). With jobs the list just does not stretch. */
    display: flex;
    flex-direction: column;
    min-height: 100%;
    box-sizing: border-box;
  }
  .panel-status {
    margin: 0;
    font-size: 11px;
    opacity: 0.6;
  }
  /* card t_6072e718: the designed empty state. Flexes to the body's full
     height and centers its glyph + copy both axes; the colors ride the
     shell variables so both themes read right (module UI carries its own
     scoped CSS, no main.css edit). */
  .cron-empty {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 2px;
    padding: 24px 12px;
    text-align: center;
  }
  .cron-empty-icon {
    color: var(--text-dim, #8a8a92);
    opacity: 0.8;
    margin-bottom: 6px;
  }
  .cron-empty-text {
    margin: 0;
    font-size: 12.5px;
    opacity: 0.75;
  }
  .cron-empty-hint {
    margin: 2px 0 0;
    font-size: 11px;
    color: var(--text-dim, #8a8a92);
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
  /* narrow: the module-UI recipe (card t_a2fdc630). The rows are
     already single-column; what dies first at ~300px is horizontal
     breathing room - the head's name/badge pair wraps and the chrome
     condenses so a long schedule line never elides. */
  .cron-list.narrow {
    padding: 6px 8px 10px;
  }
  .cron-list.narrow .cron-head {
    flex-wrap: wrap;
  }
  .cron-list.narrow .cron-name {
    font-size: 11.5px;
  }
</style>