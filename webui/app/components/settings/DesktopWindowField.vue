<template>
  <!--
    One settings section, the desktop window's four options. In the embedded window the
    APP answers them: its own home's config.yaml for the three toggles, the OS autostart
    registration for start with system, so the page shows the app's rows whatever server
    served it. In a browser tab only the three config toggles exist and they belong to the
    serving server, so this field renders the one-line reason instead of a row it cannot
    read - the section's rows below are that server's config.yaml.
  -->
  <div
    class="desktop-window"
    data-desktop-window>
    <p
      v-if="!appOwned"
      class="desktop-window-hint">
      start with system and the window's own behavior belong to the desktop app - open omega
      as a desktop window to set them. the toggles below are this server's config.yaml, which
      a desktop opened from this home reads at its next launch
    </p>
    <template v-else>
      <p
        v-if="state.error"
        class="desktop-window-error"
        role="alert">
        {{ state.error }}
      </p>
      <div
        v-for="row in rows"
        :key="row.key"
        class="setting-row desktop-window-row"
        :data-window-row="row.key">
        <label class="desktop-window-toggle">
          <input
            type="checkbox"
            :checked="checked(row.key)"
            :disabled="busy"
            :aria-label="row.label"
            @change="flip(row.key, ($event.target as HTMLInputElement).checked)" />
          <span>{{ row.label }}</span>
        </label>
        <p
          v-if="row.help"
          class="setting-help">
          {{ row.help }}
        </p>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
  import { computed, onMounted, ref } from "vue";
  import type { SettingKnob } from "~/composables/omega";
  import {
    DESKTOP_AUTOSTART_HELP,
    DESKTOP_AUTOSTART_LABEL,
    DESKTOP_AUTOSTART_ROW,
    DESKTOP_WINDOW_ROWS,
    desktopWindowChecked,
    desktopWindowState,
    requestDesktopWindowOptions,
    setDesktopWindowOption,
    type DesktopWindowKey,
    type DesktopWindowState,
  } from "~/composables/desktopWindow";

  const props = defineProps<{
    /** the page is the embedded window: the host answers the rows, the tray flips autostart */
    appOwned: boolean;
    /** the desktop group's knobs: the three config rows borrow their label and help */
    knobs: SettingKnob[];
  }>();

  const state = ref<DesktopWindowState>(desktopWindowState(null));
  const busy = ref(false);

  const rows = computed(() =>
    DESKTOP_WINDOW_ROWS.map((key) => {
      if (key === DESKTOP_AUTOSTART_ROW) {
        return { key, label: DESKTOP_AUTOSTART_LABEL, help: DESKTOP_AUTOSTART_HELP };
      }
      const knob = props.knobs.find((k) => k.key === `desktop.${key}`);
      return {
        key,
        label: knob?.label ?? key.replace(/_/g, " "),
        help: knob?.help ?? "",
      };
    }),
  );

  function checked(key: DesktopWindowKey): boolean {
    return desktopWindowChecked(state.value, key);
  }

  async function flip(key: DesktopWindowKey, on: boolean): Promise<void> {
    busy.value = true;
    try {
      state.value = await setDesktopWindowOption(key, on);
    } finally {
      busy.value = false;
    }
  }

  onMounted(() => {
    if (!props.appOwned) return;
    void requestDesktopWindowOptions().then((answer) => {
      state.value = answer;
    });
  });
</script>

<style scoped>
  .desktop-window {
    display: flex;
    flex-direction: column;
  }

  .desktop-window-hint {
    margin: 0 0 4px;
    font-size: 11px;
    color: var(--text-dim);
    max-width: 52ch;
  }

  .desktop-window-error {
    margin: 0 0 4px;
    font-size: 11px;
    color: var(--danger, #c0392b);
  }

  /* the row is a label plus its own help line: the section speaks once per row */
  .desktop-window-row {
    flex-direction: column;
    align-items: flex-start;
    gap: 2px;
    cursor: default;
  }

  .desktop-window-toggle {
    display: flex;
    align-items: center;
    gap: 10px;
    cursor: pointer;
  }
</style>
