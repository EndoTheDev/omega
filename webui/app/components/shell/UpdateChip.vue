<template>
  <!--
    The status bar's update chip: staged, or restart-ready. Quiet by design - it appears
    only while a release is on deck, and a click opens the Updates section of Settings.
  -->
  <button
    v-if="label"
    class="seg seg-btn update-chip"
    :class="chipClass"
    type="button"
    :title="title"
    :aria-label="title"
    @click="openPane">
    {{ label }}
  </button>
</template>

<script setup lang="ts">
  import { computed, onBeforeUnmount, onMounted, ref } from "vue";
  import { fetchUpdateStatus, type UpdateStatus } from "../../composables/updates";
  import { withSettingsSection } from "../settings/settingsNav";

  const status = ref<UpdateStatus | null>(null);
  let timer: ReturnType<typeof setInterval> | null = null;

  const label = computed(() => {
    switch (status.value?.state) {
      case "ready":
        return "restart ready";
      case "staging":
      case "preflight":
        return "update staged";
      case "available":
        return "update available";
      default:
        return "";
    }
  });
  const chipClass = computed(() => (status.value?.state === "ready" ? "ready" : ""));
  const title = computed(() => {
    const st = status.value;
    if (!st) return "";
    switch (st.state) {
      case "ready":
        return `omega ${st.staged_version ?? ""} is staged and pre-flight green - click for the Updates pane`;
      case "staging":
        return `downloading omega ${st.available ?? ""} - click for the Updates pane`;
      case "preflight":
        return "pre-flight: booting the staged binary against a copy of the store";
      case "available":
        return `omega ${st.available ?? ""} is available - click for the Updates pane`;
      default:
        return "";
    }
  });

  function openPane() {
    window.location.href = withSettingsSection(window.location.href, "updates");
  }

  async function refresh() {
    try {
      status.value = await fetchUpdateStatus();
    } catch {
      status.value = null; // an older server without the route, or a boot in flight
    }
  }

  onMounted(() => {
    void refresh();
    timer = setInterval(() => void refresh(), 60000);
  });
  onBeforeUnmount(() => {
    if (timer !== null) clearInterval(timer);
  });
</script>

<style scoped>
  .update-chip {
    font-family: var(--mono);
    color: var(--text-dim);
  }
  .update-chip.ready {
    color: var(--text);
  }
</style>
