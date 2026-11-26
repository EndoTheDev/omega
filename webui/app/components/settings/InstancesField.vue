<template>
  <!--
    The saved connections the status bar panel lists and switches to. This is the ONE
    write surface for the list: rows are named addresses. In a desktop window the APP
    writes them (its own home's config.yaml `desktop.instances`); in a browser tab the
    active server does. The pane validates no shape itself - the serve's validator is the
    only validator, and its refusal renders as written in the alert line.

    An add is a commitment, so it PROVES the address first: the serve probes it (shape,
    liveness, omega-ness) and nothing lands on a refusal. A refusal that is not a shape
    (down now, not omega, its Host guard, no route to check through) carries the
    add-anyway hatch: the operator knows the address is real, and the row lands stamped
    unverified.
  -->
  <div class="instances-editor">
    <p class="instance-hint">
      every saved connection is a name and an address - the status bar
      panel lists these rows and switches to one on a click. an address
      that is already saved renames that row; removing a row removes it
      from the panel too. the list is config.yaml's `desktop.instances`,
      hand-editable there
    </p>
    <div
      v-for="(row, i) in rows"
      :key="i"
      class="instance-row"
      :data-instance="row.name || undefined">
      <div class="instance-row-fields">
        <label class="instance-cell">
          <span>name</span>
          <input
            class="setting-input"
            type="text"
            autocomplete="off"
            spellcheck="false"
            placeholder="laptop"
            :value="row.name"
            :aria-label="`connection ${i + 1} name`"
            @input="setRow(i, 'name', ($event.target as HTMLInputElement).value)" />
        </label>
        <label class="instance-cell instance-address">
          <span>address</span>
          <input
            class="setting-input"
            type="text"
            autocomplete="off"
            spellcheck="false"
            placeholder="http://host:port"
            :value="row.url"
            :aria-label="`connection ${i + 1} address`"
            @input="setRow(i, 'url', ($event.target as HTMLInputElement).value)" />
        </label>
        <button
          class="instance-remove"
          type="button"
          :aria-label="`remove connection ${row.name || i + 1}`"
          @click="removeRow(i)">
          remove
        </button>
      </div>
      <span
        v-if="row.unverified"
        class="instance-unverified"
        :title="UNVERIFIED_TITLE">
        unverified
      </span>
    </div>
    <p
      v-if="!rows.length"
      class="instance-empty">
      no saved connections yet
    </p>
    <div class="instance-add">
      <label class="instance-cell">
        <span>name</span>
        <input
          v-model="draftName"
          class="setting-input"
          type="text"
          autocomplete="off"
          spellcheck="false"
          placeholder="laptop"
          aria-label="new connection name"
          @input="refusal = null" />
      </label>
      <label class="instance-cell instance-address">
        <span>address</span>
        <input
          v-model="draftURL"
          class="setting-input"
          type="text"
          autocomplete="off"
          spellcheck="false"
          placeholder="http://host:port"
          aria-label="new connection address"
          @input="refusal = null" />
      </label>
      <button
        class="instance-add-btn"
        type="button"
        :disabled="checking"
        @click="addRow">
        {{ checking ? "checking..." : "add connection" }}
      </button>
    </div>
    <p
      v-if="refusal"
      class="instance-error instance-probe-error"
      role="alert">
      <span class="instance-probe-reason">{{ refusal.reason }}</span>
      <button
        v-if="canAddAnyway"
        class="instance-anyway"
        type="button"
        @click="addAnyway">
        add anyway
      </button>
    </p>
    <p
      v-if="error"
      class="instance-error"
      role="alert">{{ error }}</p>
    <p
      v-else-if="!refusal"
      class="instance-add-note">
      an existing address renames that row; the address and name save with the
      pane's save button, like every other setting
    </p>
  </div>
</template>

<script setup lang="ts">
  import { computed, ref } from "vue";
  import type { SettingKnob } from "~/composables/omega";
  import {
    addInstanceRow,
    hatchInstanceRow,
    removeInstance,
    type InstanceProbe,
    type SavedInstance,
  } from "~/composables/instances";

  const props = defineProps<{
    knob: SettingKnob;
    value: SavedInstance[];
    // the server's refusal for THIS knob, as written; the pane's save path supplies it
    error?: string | null;
  }>();

  const emit = defineEmits<{
    (e: "update", value: SavedInstance[]): void;
  }>();

  const rows = computed<SavedInstance[]>(() => props.value ?? []);
  const draftName = ref("");
  const draftURL = ref("");
  // the add action's own answer: the probe's refusal for the row being added, rendered
  // beside the hatch. Cleared by the next add or by editing the draft.
  const refusal = ref<InstanceProbe | null>(null);
  const checking = ref(false);

  const UNVERIFIED_TITLE =
    "added anyway - nothing answered at this address when it was added";

  // a shape the file's validator refuses could never be saved by any build, so the hatch is
  // offered on every refusal but that one.
  const canAddAnyway = computed(() => refusal.value !== null && refusal.value.failure !== "syntax");

  function setRow(i: number, key: "name" | "url", value: string) {
    emit(
      "update",
      rows.value.map((row, j) => (j === i ? { ...row, [key]: value } : row)),
    );
  }

  async function addRow() {
    if (checking.value) return;
    checking.value = true;
    refusal.value = null;
    try {
      const outcome = await addInstanceRow(rows.value, { name: draftName.value, url: draftURL.value });
      if (!outcome.added) {
        refusal.value = outcome.probe;
        return;
      }
      emit("update", outcome.list);
      draftName.value = "";
      draftURL.value = "";
    } finally {
      checking.value = false;
    }
  }

  function addAnyway() {
    const refused = refusal.value;
    if (!refused) return;
    emit("update", hatchInstanceRow(rows.value, { name: draftName.value, url: draftURL.value }, refused));
    refusal.value = null;
    draftName.value = "";
    draftURL.value = "";
  }

  function removeRow(i: number) {
    const row = rows.value[i];
    if (!row) return;
    emit("update", removeInstance(rows.value, row.url));
  }
</script>

<style scoped>
  .instances-editor {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    max-width: 100%;
  }

  .instance-hint,
  .instance-add-note,
  .instance-empty {
    margin: 0;
    font-size: 11px;
    color: var(--text-dim);
    max-width: 52ch;
  }

  .instance-row {
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
  }

  .instance-row-fields,
  .instance-add {
    display: grid;
    grid-template-columns: minmax(6rem, 1fr) minmax(10rem, 1.6fr) min-content;
    gap: 0.4rem;
    align-items: end;
    padding-right: 0.25rem;
  }

  .instance-cell {
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
    min-width: 0;
  }

  .instance-cell span {
    font-size: 0.72em;
    opacity: 0.6;
  }

  .instance-cell :deep(.setting-input) {
    max-width: 100%;
  }

  .instance-error {
    margin: 0;
    font-size: 11px;
    color: var(--danger, #c0392b);
  }

  /* the probe's refusal line: the reason, and the hatch beside it when it is offered */
  .instance-probe-error {
    display: flex;
    align-items: baseline;
    flex-wrap: wrap;
    gap: 0.5rem;
    max-width: 52ch;
  }

  .instance-anyway {
    flex: none;
    border: 1px solid var(--border, rgba(128, 128, 128, 0.35));
    background: transparent;
    color: inherit;
    border-radius: 6px;
    padding: 0.15rem 0.45rem;
    font-size: 0.9em;
    cursor: pointer;
    white-space: nowrap;
  }

  .instance-anyway:hover {
    text-decoration: underline;
  }

  /* the added-anyway marker: text, never a colour - the row says why it is unproven */
  .instance-unverified {
    align-self: flex-start;
    margin-left: 0.1rem;
    padding: 0 0.3rem;
    border: 1px solid var(--border, rgba(128, 128, 128, 0.35));
    border-radius: 4px;
    font-size: 10px;
    color: var(--text-dim);
    text-transform: uppercase;
    letter-spacing: 0.02em;
    cursor: help;
  }

  .instance-add-btn[disabled] {
    opacity: 0.6;
    cursor: default;
  }

  .instance-remove,
  .instance-add-btn {
    border: 1px solid var(--border, rgba(128, 128, 128, 0.35));
    background: transparent;
    color: inherit;
    border-radius: 6px;
    padding: 0.25rem 0.55rem;
    font-size: 0.8em;
    cursor: pointer;
    white-space: nowrap;
  }

  .instance-remove:hover {
    color: var(--danger, #c0392b);
  }

  .instance-add-btn {
    align-self: end;
  }
</style>
