<template>
  <!-- omega webui: the NAMED PROVIDER INSTANCES editor (card
       t_7dcd4280, reshaped t_1914a8c9 to the Hermes shape, merged
       t_bf21ee07 into the ONE providers section). One row per
       instance: the row's name IS the instance id
       (config.yaml provider.instances) and the row is a NAMED ENDPOINT
       only - name, type, base url, api key, NO model column (models
       come from the instance's own catalog through the model pickers).
       The row named "default" is the primary, so the list always
       carries it; remove drops any other row (a removed instance
       strands ITS sessions - their next send fails loudly, never a
       silent fallback); add appends a blank row. The list REPLACES the
       server's list on save; a masked api key ("****") round-trips as
       "keep the existing secret" - typing over it replaces the secret,
       clearing it keeps the masked round trip failing server-side (a
       masked key must mean something). -->
  <div class="providers-editor">
    <!-- card t_fec41e66 (the one-line explainer survives the reshape):
         the rows carry no visible flow on their own - one line above
         the list states what a row IS and how a run reaches it -->
    <p class="provider-hint">
      every provider is a named instance: a name, its type, a base url
      and an api key - the model pickers (chat picker and the slot
      knobs below) group that endpoint's models under its name. the row
      named "default" is the provider the agent serves: it cannot be
      renamed or removed here (edit config.yaml), its type is yours to
      change. base urls and api keys are config.yaml's to set - this
      pane saves names and types
    </p>
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
            placeholder="my-ollama"
            :value="inst.name"
            :readonly="isPrimary(inst)"
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
        <label class="provider-cell provider-host">
          <span
            >base url<em v-if="isNew(inst)" class="provider-door"
              >config.yaml-only</em
            ></span
          >
          <input
            class="setting-input"
            type="text"
            autocomplete="off"
            spellcheck="false"
            :disabled="isNew(inst)"
            :placeholder="isNew(inst) ? 'config.yaml-only' : 'provider default'"
            :value="inst.host"
            :aria-label="`instance ${i + 1} base url`"
            @input="setRow(i, 'host', ($event.target as HTMLInputElement).value)" />
        </label>
        <label class="provider-cell">
          <span
            >api key<em v-if="isNew(inst)" class="provider-door"
              >config.yaml-only</em
            ></span
          >
          <input
            class="setting-input"
            type="password"
            autocomplete="off"
            :disabled="isNew(inst)"
            :value="inst.api_key"
            :aria-label="`instance ${i + 1} api key`"
            @input="setRow(i, 'api_key', ($event.target as HTMLInputElement).value)" />
        </label>
        <button
          class="provider-remove"
          type="button"
          :disabled="isPrimary(inst)"
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
    <!-- card t_4924df4c: a fresh row can only save its name and type,
         so the add flow says where an endpoint actually comes from -->
    <p class="provider-add-note">
      a new instance saves with its name and type - its base url and api
      key are set in config.yaml, and the row shows them here once the
      file carries them
    </p>
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

  // BandSelect's ladder is {value,label} objects: a bare string array
  // renders an EMPTY popup (the rows read o.value/o.label) while the
  // chip still shows the raw value - the type cell looked fine and did
  // nothing (Pre-existing at the merge base, fixed here because the
  // merged providers section claims the type is yours to change).
  const types: { value: string; label: string }[] = [
    { value: "ollama", label: "ollama" },
    { value: "openai", label: "openai" },
    { value: "anthropic", label: "anthropic" },
  ];

  const rows = computed<ProviderInstanceRow[]>(() => props.value ?? []);

  // The primary is the row named "default" (card t_bf21ee07): the
  // provider the agent serves, so its name is the contract and the row
  // never leaves the list through the pane (the server refuses a list
  // without it). Its type IS editable: a row is a row.
  const PRIMARY = "default";
  const isPrimary = (row: ProviderInstanceRow): boolean => row.name === PRIMARY;

  // isNew: a row the STORED list does not name. props.value is the draft
  // (SettingsTab reads draft-first); knob.value is the last payload the
  // server sent, which is exactly what applyProviderInstances compares a
  // submitted name against. A new row is legal with its name and type
  // ALONE - a host or api key on one answers "config.yaml is the only
  // door" (cmd/omega/settings.go), so the pane must not offer those
  // inputs. A row named like a stored one is a rename/retype, whose host
  // and masked key round-trip legally, so its inputs stay live.
  const isNew = (row: ProviderInstanceRow): boolean => {
    const stored = props.knob.value;
    if (!Array.isArray(stored)) return true;
    const name = row.name.trim();
    return !stored.some(
      (r) =>
        !!r &&
        typeof r === "object" &&
        typeof (r as ProviderInstanceRow).name === "string" &&
        (r as ProviderInstanceRow).name === name,
    );
  };

  function setRow(i: number, key: keyof ProviderInstanceRow, value: string) {
    if (key === "name" && isPrimary(rows.value[i]!)) return;
    emit(
      "update",
      rows.value.map((row, j) => (j === i ? { ...row, [key]: value } : row)),
    );
  }

  function addRow() {
    emit("update", [
      ...rows.value,
      { name: "", type: "openai", host: "", api_key: "" },
    ]);
  }

  function removeRow(i: number) {
    if (isPrimary(rows.value[i]!)) return;
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

  .provider-hint {
    margin: 0;
    font-size: 11px;
    color: var(--text-dim);
  }

  .provider-row-fields {
    display: grid;
    grid-template-columns: minmax(6rem, 1fr) minmax(6rem, min-content) minmax(8rem, 1.4fr) minmax(6rem, 1fr) min-content;
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

  /* card t_4924df4c: the door marker on a new row's gated cells - the
     field itself says config.yaml owns it, before any save click */
  .provider-door {
    margin-left: 0.35em;
    font-style: normal;
    font-size: 0.92em;
    letter-spacing: 0.01em;
    color: var(--text-dim);
    opacity: 1;
  }

  .provider-cell input:disabled {
    opacity: 0.55;
    cursor: not-allowed;
  }

  .provider-add-note {
    margin: 0;
    font-size: 11px;
    color: var(--text-dim);
    max-width: 42ch;
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
