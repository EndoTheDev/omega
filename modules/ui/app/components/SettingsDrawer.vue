<template>
  <!-- no title row: the menubar gear is the toggle - this is just content -->
  <aside class="settings">
    <section class="settings-group">
      <h3>reasoning</h3>
      <label class="setting-row">
        <input
          :checked="showThinking"
          type="checkbox"
          @change="
            $emit(
              'update:showThinking',
              ($event.target as HTMLInputElement).checked,
            )
          " />
        <span>show thinking blocks in chat</span>
      </label>
    </section>
    <section class="settings-group">
      <h3>theme</h3>
      <select
        class="model-chip theme-select"
        :value="themeMode"
        @change="
          $emit(
            'set-theme',
            ($event.target as HTMLSelectElement).value as
              | 'auto'
              | 'light'
              | 'dark',
          )
        ">
        <option value="auto">auto (system)</option>
        <option value="light">light</option>
        <option value="dark">dark</option>
      </select>
    </section>
    <p class="settings-note">
      tools run with the server registry; per-tool toggles arrive with the next
      backend revision.
    </p>
  </aside>
</template>

<script setup lang="ts">
  defineProps<{
    showThinking: boolean;
    themeMode: "auto" | "light" | "dark";
  }>();

  defineEmits<{
    (e: "update:showThinking", value: boolean): void;
    (e: "set-theme", mode: "auto" | "light" | "dark"): void;
  }>();
</script>
