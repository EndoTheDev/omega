<template>
  <!-- omega webui: the TYPED MODEL PICKER (card t_70c28128). A
       settings model field renders the shared BandSelect listbox
       populated from the provider's live model catalog, narrowed to
       the knob's slot (decision / vision / full chat list). The
       per-subsystem model knobs (card t_fc0b2f2d, knob.slot) ALSO
       list the named provider instances' models as "instance:model"
       values - the server routes the consult through that instance.
       No list = no dropdown: the fetch's error state (or an empty
       filtered result) falls back to free text with the hint spelled
       out - the escape hatch is the DEGRADED state, never a silent
       dead dropdown. -->
  <template v-if="mode === 'list'">
    <BandSelect
      class="setting-select setting-model"
      :model-value="value"
      :options="options"
      :label="knob.label"
      drop="down"
      @update:model-value="$emit('update', $event)" />
  </template>
  <template v-else>
    <input
      class="setting-input"
      type="text"
      autocomplete="off"
      spellcheck="false"
      :value="value"
      :aria-label="knob.label"
      @input="$emit('update', ($event.target as HTMLInputElement).value)" />
    <p class="setting-help setting-model-hint">{{ hint }}</p>
  </template>
</template>

<script setup lang="ts">
  import { computed } from "vue";
  import type { SettingKnob } from "~/composables/omega";
  import {
    filterModelEntries,
    useModelCatalog,
    type ModelFilter,
    type ModelEntry,
  } from "~/composables/modelCatalog";
  // THE shared listbox (card t_acad02ec) - never a new select
  // component, never a native select popup (OS chrome over the panes)
  import BandSelect from "../ui/BandSelect.vue";

  const props = defineProps<{
    knob: SettingKnob;
    value: string;
  }>();

  defineEmits<{
    (e: "update", value: string): void;
  }>();

  const { state: catalog } = useModelCatalog();
  const filter = (props.knob.filter ?? "") as ModelFilter;
  // card t_fc0b2f2d / t_1914a8c9: the per-subsystem slots also offer
  // the named provider instances - the models come from each
  // instance's own catalog section (the grouped /models payload), so
  // a slot named instance:model resolves on the server as before. A
  // section that failed to list contributes nothing (the picker shows
  // the failure in the chat picker; the slots ride the default
  // catalog's models and free text).
  const instanceOptions = computed(() => {
    if (props.knob.slot !== true) return [];
    const sections = catalog.value.sections ?? [];
    return sections
      .filter((s) => s.name !== "default" && !s.error)
      .flatMap((s) => {
        const entries: ModelEntry[] = (s.models ?? []).map((name) => ({
          name,
          capabilities: s.capabilities?.[name] ?? [],
        }));
        return filterModelEntries(entries, filter).map((e) => ({
          value: `${s.name}:${e.name}`,
          label: `${s.name}:${e.name}`,
        }));
      });
  });

  const options = computed(() => [
    ...instanceOptions.value,
    ...filterModelEntries(catalog.value.entries ?? [], filter).map((e) => ({
      value: e.name,
      label: e.name,
    })),
  ]);

  type Mode = "list" | "fallback";
  // list = the dropdown renders: while the catalog is in flight
  // (brief round trip, the hint says loading) and once it landed with
  // a non-empty filtered list. A listing that answered but matched
  // nothing in a filtered slot degrades the same way a failed fetch
  // does (the empty pop would be a dead dropdown with no escape) -
  // except a slot knob that already carries instance entries.
  const mode = computed<Mode>(() => {
    if (
      catalog.value.error === null &&
      (catalog.value.loading || options.value.length > 0)
    )
      return "list";
    return "fallback";
  });

  const hint = computed(() => {
    if (catalog.value.error !== null)
      return `provider model list unavailable (${catalog.value.error}) - type the model name`;
    if (catalog.value.loading) return "loading the provider's models…";
    if (options.value.length === 0)
      return filter === "decision"
        ? "no decision models in the provider's list - type the model name"
        : filter === "vision"
          ? "no vision models in the provider's list - type the model name"
          : "the provider's model list came back empty - type the model name";
    return "";
  });
</script>
