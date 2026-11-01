<template>
  <section
    ref="rootEl"
    class="cron-list"
    :class="{ narrow: narrow }">
    <!-- card t_308e6d9b: the UNIVERSAL band - the Files pattern lands on
         every tab. Card t_529628d5: the band carries the compact in-band
         search seat (the t_7ee0a673 sessions seat, TabBand's shared
         .band-search) - the query filters the LOADED jobs by name or
         schedule, client-side, hide-rows-only - and the REFRESH tool
         (the Skills/Tools/MCP band-tool look): the click re-fetches GET
         /cron/jobs. Rendered BEFORE the scroll body: scrolling never
         moves it. -->
    <TabBand title="Cron">
      <div class="band-search">
        <SearchInput
          v-model="query"
          placeholder="Search cron jobs" />
      </div>
      <button
        class="band-tool"
        title="refresh"
        aria-label="refresh cron jobs"
        :disabled="refreshing"
        @click="refresh">
        <Icon
          name="refresh"
          :size="13" />
      </button>
    </TabBand>
    <div class="cron-body">
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
        v-else-if="filteredJobs.length"
        class="cron-jobs">
      <li
        v-for="j in filteredJobs"
        :key="cronKeyOf(j)"
        class="cron-row reg-row"
        :class="{ disabled: !j.enabled }">
        <div class="cron-head reg-row-head">
          <div class="reg-row-main">
            <span class="cron-name reg-row-name">{{ j.name }}</span>
            <!-- the row law's subtitle: the mono schedule expression,
                 with the next fire riding the same dim line - the
                 two-line reference anatomy, uniform row heights. -->
            <span class="reg-row-sub">
              <span class="cron-meta reg-row-sub-text">
                {{ j.schedule }}<template v-if="j.next"> - next {{ shortTime(j.next) }}</template>
              </span>
            </span>
          </div>
          <!-- card t_561dc4e0: the "enabled"/"disabled" text dies - the
               one right control is the SHARED TogglePill (card
               t_d93864e7, the MCP pill), two-step armed (no accidental
               first-click flips), top-aligned with the name. The
               confirmed click POSTs /cron/toggle (modules/cron/ui/
               toggle.ts): the write path is the per-job `enabled:` flag
               in the crons.yaml that owns the job, hot-reloaded - the
               same config-file law the MCP tab's own toggle rides. -->
          <TogglePill
            class="cron-toggle reg-row-ctrl"
            :state="j.enabled"
            :disabled="busy !== null"
            :armed="armed === cronKeyOf(j)"
            :label="`${j.enabled ? 'disable' : 'enable'} ${j.name}`"
            :confirm-label="`confirm apply to ${j.name}?`"
            @click="onToggle(j)" />
        </div>
        <p
          v-if="armed === cronKeyOf(j)"
          class="cron-confirm reg-row-note">{{ noteFor(j) }}</p>
      </li>
      </ul>
      <!-- card t_529628d5: the row anatomy law - the filter HIDES rows,
           it never changes their shape; a query that hides all of them
           reads as a search miss, NOT as the "no jobs configured" empty
           state (that state is the crons.yaml activation story and only
           speaks when the listing itself is empty). -->
      <p
        v-else-if="query.trim()"
        class="panel-status">
        no matching jobs
      </p>
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
    </div>
  </section>
</template>

<script setup lang="ts">
  import { computed, onBeforeUnmount, onMounted, ref } from "vue";
  import { isNarrowWidth } from "./narrow";
  // the empty-state copy lives in its own PURE module (card t_6072e718,
  // the serverRows.ts / boardLogic.ts convention): the node suite pins
  // it without compiling this SFC.
  import { CRON_EMPTY_HINT, CRON_EMPTY_TEXT } from "./emptyState";
  import {
    cronKeyOf,
    setCronJobEnabled,
    type CronJobWire,
  } from "./toggle";
  // card t_529628d5: the band search's filter (name / schedule),
  // pure so the node suite pins it without compiling the SFC.
  import { filterCronJobs } from "./search";

  // Module-owned UI (card t_db430fea): the cron scheduler's job set,
  // from GET /cron/jobs. v1 shipped read-only (names, schedules,
  // enablement and next fire time); card t_561dc4e0 adds the per-job
  // toggle: the two-step armed pill POSTs the `enabled:` flag into the
  // owning crons.yaml (the human-authored file gets an
  // operator-directed flag write - the same mcp.yaml law, module
  // routes own their config's flag field). Editing prompts/schedules
  // stays out of scope: crons.yaml remains the prompt's author.
  const jobs = ref<CronJobWire[]>([]);
  const loaded = ref(false);
  const error = ref("");

  // card t_529628d5: the in-band search - CLIENT-SIDE on the loaded
  // listing (the filter rides the already-fetched jobs; no new
  // backend route), name or schedule, hide-rows-only.
  const query = ref("");
  const filteredJobs = computed(() => filterCronJobs(jobs.value, query.value));

  // --- the fetch (shared by mount + the band's refresh tool) ---------
  // card t_529628d5: the refresh is GET /cron/jobs re-run - the same
  // fetch chain the mount drove (cron keeps its fetch semantics; the
  // band tool is the Skills/Tools/MCP pattern). A failure lands on the
  // panel's existing error line and the current rows stand (the
  // reload.ts ruling); the refreshing lock drops a second click while
  // a chain is in flight (the in-flight chain answers).
  const refreshing = ref(false);

  async function load(): Promise<void> {
    const res = await fetch("/cron/jobs");
    if (!res.ok) throw new Error(`GET /cron/jobs: ${res.status}`);
    jobs.value = (await res.json()) as CronJobWire[];
  }

  async function refresh(): Promise<void> {
    if (refreshing.value) return;
    refreshing.value = true;
    try {
      await load();
      error.value = "";
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    } finally {
      refreshing.value = false;
    }
  }

  // --- the per-job toggle (card t_561dc4e0) ----------------------------
  // The two-step mcp mechanic, armed locally: click 1 arms the row (the
  // confirm note states the consequence), click 2 commits - no
  // accidental first-click flips. busy holds the committing key so a
  // stacked double-click cannot overlap writes.
  const armed = ref<string | null>(null);
  const busy = ref<string | null>(null);

  function onToggle(j: CronJobWire) {
    if (busy.value !== null) return;
    if (armed.value !== cronKeyOf(j)) {
      armed.value = cronKeyOf(j);
      return;
    }
    apply(j);
  }

  // the commit: POST the toggle (the `enabled:` flag write lands and
  // the runner hot-reloads), then re-render from the fresh listing.
  // Armed clears whether the write lands or throws.
  async function apply(j: CronJobWire) {
    const target = !j.enabled;
    armed.value = null;
    busy.value = cronKeyOf(j);
    try {
      jobs.value = await setCronJobEnabled(cronKeyOf(j), target);
      error.value = "";
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    } finally {
      busy.value = null;
    }
  }

  function noteFor(j: CronJobWire): string {
    return j.enabled
      ? `disable ${j.name}? click again to confirm`
      : `enable ${j.name}? click again to confirm`;
  }

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
      await load();
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    }
    loaded.value = true;
  });
</script>

<style scoped>
  /* card t_308e6d9b: the band anchors the top, the body owns the scroll.
     The old root padding (10px 12px 14px) rides the body now; the
     empty state still centers in the body's full height (card
     t_6072e718). */
  .cron-list {
    flex: 1;
    min-height: 0;
    min-width: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }
  .cron-body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    overflow-x: hidden;
    padding: 4px 12px 14px;
    display: flex;
    flex-direction: column;
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
  /* card t_561dc4e0: the row geometry (paddings, separators, name/sub
     typography, the right-control seat, the confirm note line) lives
     in the shared resources css/registry-row.css anatomy - this block
     keeps only the cron specifics: the disabled dim, the mono
     schedule voice (the row law keeps the expression mono), the
     narrow condensation. */
  .cron-row.disabled {
    opacity: 0.5;
  }
  .cron-meta {
    font-family: var(--mono, monospace);
  }
  /* narrow: the module-UI recipe (card t_a2fdc630). The rows are
     already single-column; what dies first at ~300px is horizontal
     breathing room - the head's name/sub pair wraps and the chrome
     condenses so a long schedule line never elides. */
  .cron-list.narrow .cron-body {
    padding: 4px 8px 10px;
  }
  /* card t_561dc4e0: the narrow name shrunk to 11.5px under the old
     bespoke anatomy - the row law's uniform heights carry narrow (the
     shared .narrow .reg-row rule), the name keeps its one size. */
</style>