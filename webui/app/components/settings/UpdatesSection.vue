<template>
  <!--
    The update channel's own pane: current version, channel, staged state, the four
    actions, and the last receipt. Every action posts to /updates - the pane never
    downloads, installs or restarts by itself.
  -->
  <div class="updates-pane" data-section="updates-status">
    <div class="updates-facts">
      <span class="updates-version">{{ status?.current || "unknown" }}</span>
      <span class="updates-channel">{{ status?.channel || "stable" }}</span>
      <span v-if="status && !status.managed" class="updates-badge">not release-managed</span>
      <span v-if="status && status.active_runs > 0" class="updates-badge">
        {{ status.active_runs }} run{{ status.active_runs === 1 ? "" : "s" }} active
      </span>
    </div>

    <p class="updates-line" :class="{ failed: status?.state === 'failed' }">
      {{ updateHeadline(status) }}
    </p>

    <div
      v-if="inFlight && status?.state !== 'finalizing'"
      class="updates-progress"
      role="progressbar"
      :aria-valuemin="0"
      :aria-valuemax="100"
      :aria-valuenow="progress">
      <span class="updates-progress-fill" :style="{ width: progress + '%' }" />
    </div>

    <div class="updates-actions">
      <button class="updates-btn" type="button" :disabled="busy" @click="check">
        check now
      </button>
      <button
        v-if="status?.state === 'available'"
        class="updates-btn primary"
        type="button"
        :disabled="busy"
        @click="stage">
        stage {{ status?.available || "the release" }}
      </button>
      <button
        v-if="canFinalize"
        class="updates-btn primary"
        type="button"
        :disabled="busy"
        @click="restart">
        restart to update
      </button>
      <button v-if="canFinalize" class="updates-btn" type="button" :disabled="busy" @click="discard">
        discard
      </button>
    </div>

    <div v-if="prompt" class="updates-confirm" role="alertdialog" aria-label="restart confirmation">
      <p>{{ prompt }}</p>
      <div class="updates-actions">
        <button class="updates-btn primary" type="button" :disabled="busy" @click="confirmRestart">
          restart anyway
        </button>
        <button class="updates-btn" type="button" :disabled="busy" @click="prompt = false">
          cancel
        </button>
      </div>
    </div>

    <p v-if="error" class="updates-error">{{ error }}</p>
    <p v-if="status?.receipt" class="updates-receipt">{{ updateReceiptLine(status.receipt) }}</p>
  </div>
</template>

<script setup lang="ts">
  import { computed, onBeforeUnmount, onMounted, ref } from "vue";
  import {
    checkForUpdates,
    discardStagedUpdate,
    fetchUpdateStatus,
    finalizeUpdate,
    stageUpdate,
    updateCanFinalize,
    updateHeadline,
    updateInFlight,
    updateReceiptLine,
    updateRestartPrompt,
    type UpdateStatus,
  } from "../../composables/updates";

  const status = ref<UpdateStatus | null>(null);
  const error = ref("");
  const busy = ref(false);
  const prompt = ref(false);
  let timer: ReturnType<typeof setInterval> | null = null;

  const inFlight = computed(() => updateInFlight(status.value));
  const canFinalize = computed(() => updateCanFinalize(status.value));
  const progress = computed(() => Math.max(0, Math.min(100, status.value?.progress ?? 0)));

  function adopt(st: UpdateStatus) {
    status.value = st;
    error.value = st.error ?? "";
  }

  async function refresh() {
    try {
      adopt(await fetchUpdateStatus());
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    }
  }

  /** one action, one status: the server answers the new state, the pane renders it. */
  async function act(run: () => Promise<UpdateStatus>) {
    busy.value = true;
    error.value = "";
    try {
      adopt(await run());
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    } finally {
      busy.value = false;
    }
  }

  const check = () => act(checkForUpdates);
  const stage = () => act(stageUpdate);
  const discard = () => act(discardStagedUpdate);

  function restart() {
    if ((status.value?.active_runs ?? 0) > 0) {
      prompt.value = true;
      return;
    }
    void act(() => finalizeUpdate(true));
  }

  function confirmRestart() {
    prompt.value = false;
    void act(() => finalizeUpdate(true));
  }

  // The server is mid-flight while staging or booting the copy: poll, and stop as soon as
  // it settles. A finalize drops the connection - the page reloads into the new build.
  onMounted(() => {
    void refresh();
    timer = setInterval(() => {
      if (inFlight.value && status.value?.state !== "finalizing") void refresh();
    }, 2000);
  });
  onBeforeUnmount(() => {
    if (timer !== null) clearInterval(timer);
  });
</script>

<style scoped>
  .updates-pane {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 10px 0 2px;
    font-family: var(--mono);
    font-size: 12px;
  }
  .updates-facts {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--text-dim);
  }
  .updates-version {
    color: var(--text);
  }
  .updates-channel,
  .updates-badge {
    padding: 1px 6px;
    border: 1px solid var(--stroke);
    border-radius: var(--r-chip);
    font-size: 11px;
  }
  .updates-line {
    margin: 0;
    color: var(--text-dim);
  }
  .updates-line.failed,
  .updates-error {
    color: var(--err);
  }
  .updates-progress {
    height: 3px;
    background: var(--z1-raised);
    border-radius: var(--r-chip);
    overflow: hidden;
  }
  .updates-progress-fill {
    display: block;
    height: 100%;
    background: var(--accent);
    transition: width 0.2s linear;
  }
  .updates-actions {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .updates-btn {
    padding: 3px 10px;
    border: 1px solid var(--stroke);
    border-radius: var(--r-chip);
    background: transparent;
    color: var(--text);
    font-family: var(--mono);
    font-size: 11px;
    cursor: pointer;
  }
  .updates-btn:hover:not(:disabled) {
    background: var(--z1-raised);
  }
  .updates-btn:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .updates-btn.primary {
    border-color: var(--accent);
  }
  .updates-confirm {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 8px;
    border: 1px solid var(--stroke);
    border-radius: var(--r-chip);
  }
  .updates-confirm p {
    margin: 0;
    color: var(--text);
  }
  .updates-receipt {
    margin: 0;
    color: var(--text-dim);
  }
</style>
