<template>
  <!-- omega webui: the NAMED PROVIDER INSTANCES editor (card
       t_7dcd4280). One row per instance: the row's name IS the
       instance id (config.yaml providers:), so two accounts of one
       type are two rows. remove drops the row (a removed instance
       strands ITS sessions - their next send fails loudly, never a
       silent fallback); add appends a blank row. The list REPLACES
       the server's providers: map on save; a masked api key
       ("****") round-trips as "keep the existing secret" - typing
       over it replaces the secret, clearing it keeps the masked
       round trip failing server-side (a masked key must mean
       something). -->
  <div class="providers-editor">
    <div
      v-for="(inst, i) in rows"
      :key="i"
      class="provider-row"
      :data-instance="inst.name || undefined">
      <div class="provider-row-fields">
        <label class="provider-cell">
          <span>name</span>
          <input
            class="setting-input"
            type="text"
            autocomplete="off"
            spellcheck="false"
            :value="inst.name"
            :aria-label="`instance ${i + 1} name`"
            @input="setRow(i, 'name', ($event.target as HTMLInputElement).value)" />
        </label>
        <label class="provider-cell">
          <span>type</span>
          <BandSelect
            class="setting-select"
            :model-value="inst.type"
            :options="types"
            label="instance type"
            drop="down"
            @update:model-value="setRow(i, 'type', $event)" />
        </label>
        <label class="provider-cell">
          <span>model</span>
          <input
            class="setting-input"
            type="text"
            autocomplete="off"
            spellcheck="false"
            :value="inst.model_name"
            :aria-label="`instance ${i + 1} model`"
            @input="setRow(i, 'model_name', ($event.target as HTMLInputElement).value)" />
        </label>
        <label class="provider-cell provider-host">
          <span>host</span>
          <input
            class="setting-input"
            type="text"
            autocomplete="off"
            spellcheck="false"
            :value="inst.host"
            :aria-label="`instance ${i + 1} host`"
            @input="setRow(i, 'host', ($event.target as HTMLInputElement).value)" />
        </label>
        <label class="provider-cell">
          <span>api key</span>
          <input
            class="setting-input"
            type="password"
            autocomplete="off"
            :value="inst.api_key"
            :aria-label="`instance ${i + 1} api key`"
            @input="setRow(i, 'api_key', ($event.target as HTMLInputElement).value)" />
        </label>
        <button
          class="provider-remove"
          type="button"
          :aria-label="`remove instance ${inst.name || i + 1}`"
          @click="removeRow(i)">
          remove
        </button>
      </div>
    </div>
    <button
      class="provider-add"
      type="button"
      @click="addRow">
      add instance
    </button>
  </div>
</template>

<script setup lang="ts">
  import { computed } from "vue";
  import type { SettingKnob, ProviderInstanceRow } from "~/composables/omega";
  // THE shared listbox (card t_acad02ec) - never a native select
  // popup (OS chrome over the panes)
  import BandSelect from "../ui/BandSelect.vue";

  const props = defineProps<{
    knob: SettingKnob;
    value: ProviderInstanceRow[];
  }>();

  const emit = defineEmits<{
    (e: "update", value: ProviderInstanceRow[]): void;
  }>();

  const types = ["ollama", "openai", "anthropic"];

  const rows = computed<ProviderInstanceRow[]>(() => props.value ?? []);

  function setRow(i: number, key: keyof ProviderInstanceRow, value: string) {
    emit(
      "update",
      rows.value.map((row, j) => (j === i ? { ...row, [key]: value } : row)),
    );
  }

  function addRow() {
    emit("update", [
      ...rows.value,
      { name: "", type: "openai", model_name: "", host: "", api_key: "" },
    ]);
  }

  function removeRow(i: number) {
    emit(
      "update",
      rows.value.filter((_, j) => j !== i),
    );
  }
</script>

<style scoped>
  .providers-editor {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    max-width: 100%;
  }

  .provider-row-fields {
    display: grid;
    grid-template-columns: minmax(6rem, 1fr) minmax(6rem, min-content) minmax(7rem, 1.2fr) minmax(6rem, 1.2fr) minmax(6rem, 1fr) min-content;
    gap: 0.4rem;
    align-items: end;
    padding-right: 0.25rem;
  }

  .provider-cell {
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
    min-width: 0;
  }

  .provider-cell span {
    font-size: 0.72em;
    opacity: 0.6;
  }

  .provider-cell :deep(.setting-input),
  .provider-cell :deep(.setting-select) {
    max-width: 100%;
  }

  .provider-remove,
  .provider-add {
    border: 1px solid var(--border, rgba(128, 128, 128, 0.35));
    background: transparent;
    color: inherit;
    border-radius: 6px;
    padding: 0.25rem 0.55rem;
    font-size: 0.8em;
    cursor: pointer;
  }

  .provider-remove:hover {
    color: var(--danger, #c0392b);
  }

  .provider-add {
    align-self: flex-start;
  }
</style>
