<template>
  <!--
    The saved connections the status bar panel lists and switches to. This is the ONE
    write surface for the list: rows are named addresses, and the whole array rides the
    pane's save path into config.yaml `desktop.instances` (the same writer and the same
    validator a hand edit hits). Nothing here validates a name or an address - the
    server's refusal is the only refusal, and it renders as written in the alert line.
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
          aria-label="new connection name" />
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
          aria-label="new connection address" />
      </label>
      <button
        class="instance-add-btn"
        type="button"
        @click="addRow">
        add connection
      </button>
    </div>
    <p
      v-if="error"
      class="instance-error"
      role="alert">{{ error }}</p>
    <p
      v-else
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
    removeInstance,
    upsertInstance,
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

  // address matching, never address validation: the same address typed with or without
  // its scheme and trailing slash is the same row, so the add renames it instead of
  // appending a duplicate the server would refuse.
  const addressKey = (raw: string): string => {
    const s = raw.trim().toLowerCase().replace(/\/+$/, "");
    return /^[a-z][a-z0-9+.-]*:\/\//.test(s) ? s : `http://${s}`;
  };

  function setRow(i: number, key: "name" | "url", value: string) {
    emit(
      "update",
      rows.value.map((row, j) => (j === i ? { ...row, [key]: value } : row)),
    );
  }

  function addRow() {
    const typed = draftURL.value.trim();
    const match = rows.value.find((row) => addressKey(row.url) === addressKey(typed));
    emit(
      "update",
      upsertInstance(rows.value, {
        name: draftName.value.trim(),
        // the stored address stays when this is a rename, so the row keeps its identity
        url: match ? match.url : typed,
      }),
    );
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
