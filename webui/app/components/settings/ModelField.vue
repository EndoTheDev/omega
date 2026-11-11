<template>
  <!-- omega webui: the TYPED MODEL PICKER (card t_70c28128; THE COMPOSER'S
       picker since t_dac84a33). A settings model field renders the same
       popover the chat composer has - one collapsible section per
       provider, search across sections, a listing failure shown inline in
       its section, footer actions (refresh / add custom / edit custom) -
       from the ONE shared catalog (composables/modelCatalog.ts; the
       catalogs were always shared, only the rendering differed).
       The knob's slot filter rides the picker's qualify hook: a model the
       knob refuses stays VISIBLE, disabled with its reason ("not a vision
       model"), so the catalog reads complete instead of quietly missing
       rows. A pick maps onto the value the knob persists (see commit()).
       No usable list = no dropdown: a failed fetch (or a catalog whose
       every model is disqualified for this slot) falls back to free text
       with the hint spelled out - the escape hatch is the DEGRADED state,
       never a silent dead dropdown. -->
  <template v-if="mode === 'list'">
    <ModelSelect
      class="setting-select setting-model"
      :model="value"
      :models="flatModels"
      :groups="groups"
      :label="knob.label"
      :inherit-row="inheritRow"
      :qualify="qualify"
      drop="down"
      @update:model="commit" />
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
  import type { ModelProviderSection, SettingKnob } from "~/composables/omega";
  import {
    knobModelValue,
    pickerNeedsFreeText,
    slotRejectReason,
    useModelCatalog,
    type ModelFilter,
  } from "~/composables/modelCatalog";
  // THE composer's picker (card t_1914a8c9): settings and chat render the
  // SAME popover since t_dac84a33 - never a second grouped picker
  import ModelSelect from "../chat/ModelSelect.vue";

  const props = defineProps<{
    knob: SettingKnob;
    value: string;
  }>();

  const emit = defineEmits<{
    (e: "update", value: string): void;
  }>();

  const { state: catalog } = useModelCatalog();
  const filter = (props.knob.filter ?? "") as ModelFilter;
  const sections = computed<ModelProviderSection[]>(
    () => catalog.value.sections ?? [],
  );

  // The picker's groups: EVERY section and EVERY model, unfiltered - the
  // slot rule lands on the rows as a reason (qualify), never as a drop.
  const groups = computed<ModelProviderSection[]>(() =>
    sections.value.map((s) => ({
      name: s.name,
      models: s.models ?? [],
      capabilities: s.capabilities,
      error: s.error,
    })),
  );

  // the flat floor: the picker's default section while no grouped payload
  // arrived (a /models answer without providers, or the loader's first
  // paint). A pick from it carries the "default:" grammar prefix.
  const flatModels = computed(() =>
    (catalog.value.entries ?? []).map((e) => e.name),
  );

  // qualify: the reason a row may not be picked in THIS knob's slot, or
  // null. The wire's per-model capabilities come from the row's own
  // section; the curated families fill in where the wire is silent
  // (effectiveCapabilities). The flat floor has no section, so its flags
  // come from the shared entry list.
  function qualify(section: string, model: string): string | null {
    if (filter !== "vision" && filter !== "decision") return null;
    const wire = sections.value.length
      ? sections.value.find((s) => s.name === section)?.capabilities?.[model]
      : catalog.value.entries?.find((e) => e.name === model)?.capabilities;
    return slotRejectReason(filter, model, wire);
  }

  // The NAMED rows the picker will render pickable. Feeds the degrade rule
  // only - a filtered slot whose every model is disqualified has no live
  // choice and must fall back to free text. A failed section contributes
  // nothing (it renders its own error line); the inherit row is NOT a
  // model and never counts (pickerNeedsFreeText's contract).
  const pickableCount = computed(() => {
    const rows = sections.value.length
      ? sections.value
          .filter((s) => !s.error)
          .flatMap((s) =>
            (s.models ?? []).map((m) => ({ section: s.name, model: m })),
          )
      : (catalog.value.entries ?? []).map((e) => ({
          section: "default",
          model: e.name,
        }));
    return rows.filter((r) => qualify(r.section, r.model) === null).length;
  });

  type Mode = "list" | "fallback";
  const mode = computed<Mode>(() =>
    pickerNeedsFreeText(
      pickableCount.value,
      catalog.value.loading,
      catalog.value.error,
    )
      ? "fallback"
      : "list",
  );

  // The inherit row appears ONLY for a knob the schema marks
  // default_option (card t_df46c8cf; the flags land in t_b403ad88) -
  // Endo's exception: an APPROVAL decision is deliberate, never inherited,
  // so the decision slot gets no row even once it carries the flag, and
  // the literal word "default" has no route into a decision knob.
  const inheritRow = computed(
    () => props.knob.defaultOption === true && filter !== "decision",
  );

  // THE COLLISION (card t_dac84a33): "default" has two meanings and they
  // stay apart - the composer grammar's "default:model" addresses the base
  // provider's SECTION, the aux word "default" means INHERIT the chat's
  // model. knobModelValue keeps slot knobs verbatim and strips the section
  // prefix from every other knob, so the literal word can only arrive from
  // the pinned inherit row.
  const sectionNames = computed(() =>
    sections.value.map((s) => s.name).filter((n) => n !== "default"),
  );

  function commit(raw: string) {
    emit(
      "update",
      knobModelValue(raw, props.knob.slot === true, sectionNames.value),
    );
  }

  const hint = computed(() => {
    if (catalog.value.error !== null)
      return inheritRow.value
        ? `provider model list unavailable (${catalog.value.error}) - type "default" to follow the chat's model, or type a model name`
        : `provider model list unavailable (${catalog.value.error}) - type the model name`;
    if (catalog.value.loading) return "loading the provider's models…";
    const lead =
      filter === "decision"
        ? "no decision models in the provider's list"
        : filter === "vision"
          ? "no vision models in the provider's list"
          : "the provider's model list came back empty";
    return inheritRow.value
      ? `${lead} - type "default" to follow the chat's model, or type a model name`
      : `${lead} - type the model name`;
  });
</script>
