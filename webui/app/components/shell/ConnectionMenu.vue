<template>
  <!--
    the connection switcher: a menubar pill over the saved-instance list. Saved rows are
    backend state (GET /instances); the pill renders the cache first and the backend list
    replaces it, so an unreachable instance still shows what it knew.
    INTERACTION SPEC (DESIGN.md: specified before it is built)
      roles: pill = button, aria-haspopup="dialog", aria-expanded; panel = dialog,
        aria-label "Connections"; each row entry = button with aria-current="true" on the
        connection the page is on; remove = button, aria-label "Remove <name>";
        the add form = form with labelled inputs, its refusal = role="alert".
      focus: opens with focus on the current-instance entry; Tab walks the panel; the
        pill's own click returns focus to the pill on close.
      escape: Escape inside the panel closes it and returns focus to the pill.
      dismissal: Escape, a second pill click, and a pointerdown outside the pill/panel.
      narrow: below the squeeze breakpoint the pill keeps the dot + chevron only (its
        accessible name rides aria-label) and the panel becomes a bar-wide sheet, so a
        340px window never measures past its own width.
  -->
  <div ref="rootEl" class="conn" @keydown.esc.stop.prevent="closeMenu">
    <button
      ref="pillEl"
      type="button"
      class="conn-pill"
      aria-haspopup="dialog"
      :aria-expanded="open"
      :aria-label="`Connection: ${label}`"
      :title="`Connection: ${label}`"
      @click="toggle">
      <span
        class="conn-dot"
        :class="{ offline }" />
      <span class="conn-label">{{ label }}</span>
      <Icon
        name="chevron-down"
        :size="13" />
    </button>

    <div
      v-if="open"
      ref="panelEl"
      class="conn-panel"
      role="dialog"
      aria-label="Connections">
      <p
        v-if="offline"
        class="conn-note"
        role="status">
        offline - showing the cached list
      </p>

      <button
        type="button"
        class="conn-row"
        :class="{ current: onCurrent }"
        :aria-current="onCurrent ? 'true' : undefined"
        @click="closeMenu">
        <span
          class="conn-check"
          aria-hidden="true">{{ onCurrent ? "\u2713" : "" }}</span>
        <span class="conn-row-name">This instance</span>
        <span class="conn-row-url">{{ currentOrigin }}</span>
      </button>

      <div
        v-for="row in rows"
        :key="row.url"
        class="conn-row"
        :class="{ current: row.url === currentOrigin }">
        <button
          type="button"
          class="conn-row-open"
          :aria-current="row.url === currentOrigin ? 'true' : undefined"
          @click="switchTo(row)">
          <span
            class="conn-check"
            aria-hidden="true">{{ row.url === currentOrigin ? "\u2713" : "" }}</span>
          <span class="conn-row-name">{{ row.name }}</span>
          <span class="conn-row-url">{{ row.url }}</span>
          <span
            v-if="row.url === lastUsed && row.url !== currentOrigin"
            class="conn-row-used"
            title="last used">last used</span>
        </button>
        <button
          type="button"
          class="conn-remove"
          :aria-label="`Remove ${row.name}`"
          :title="`Remove ${row.name}`"
          @click="remove(row)">
          <Icon
            name="trash"
            :size="13" />
        </button>
      </div>

      <p
        v-if="!rows.length"
        class="conn-empty">
        no saved connections yet
      </p>

      <form
        class="conn-add"
        @submit.prevent="add">
        <span class="conn-heading">Add connection</span>
        <input
          v-model="draftName"
          class="conn-input"
          type="text"
          placeholder="name"
          aria-label="Connection name"
          :maxlength="MAX_INSTANCE_NAME">
        <input
          v-model="draftURL"
          class="conn-input"
          type="text"
          placeholder="http://host:port"
          aria-label="Connection address"
          spellcheck="false">
        <p
          v-if="error"
          class="conn-error"
          role="alert">{{ error }}</p>
        <p
          v-else
          class="conn-hint">
          an existing address renames that row
        </p>
        <button
          type="submit"
          class="conn-save">
          Save
        </button>
      </form>
    </div>
  </div>
</template>

<script setup lang="ts">
  import { computed, nextTick, onMounted, onUnmounted, ref } from "vue";
  import Icon from "../ui/Icon.vue";
  import {
    MAX_INSTANCES,
    MAX_INSTANCE_NAME,
    lastUsedInstance,
    loadInstances,
    navigateToInstance,
    normalizeInstanceName,
    normalizeInstanceURL,
    readInstanceCache,
    removeInstance,
    saveInstances,
    upsertInstance,
    writeInstanceCache,
    type SavedInstance,
  } from "../../composables/instances.ts";

  const props = defineProps<{
    // the page's own origin: the row it matches is "this instance".
    currentOrigin: string;
  }>();

  const rootEl = ref<HTMLElement | null>(null);
  const pillEl = ref<HTMLButtonElement | null>(null);
  const panelEl = ref<HTMLElement | null>(null);

  const open = ref(false);
  const offline = ref(false);
  const error = ref("");
  const draftName = ref("");
  const draftURL = ref("");

  // the cache renders first - synchronously, so the menu never flashes empty.
  const cached = readInstanceCache();
  const rows = ref<SavedInstance[]>(cached.list);
  const lastUsed = ref(cached.lastUsed);

  const onCurrent = computed(() => Boolean(props.currentOrigin));
  const label = computed(() => rows.value.find((row) => row.url === props.currentOrigin)?.name ?? "This instance");

  async function refresh(): Promise<void> {
    const loaded = await loadInstances();
    rows.value = loaded.list;
    offline.value = loaded.offline;
    lastUsed.value = lastUsedInstance();
  }

  onMounted(() => {
    document.addEventListener("pointerdown", onOutsidePointer, true);
    void refresh();
  });
  onUnmounted(() => {
    document.removeEventListener("pointerdown", onOutsidePointer, true);
  });

  function onOutsidePointer(e: PointerEvent): void {
    if (!open.value) return;
    const target = e.target as Node | null;
    if (target && rootEl.value?.contains(target)) return;
    open.value = false;
  }

  async function toggle(): Promise<void> {
    if (open.value) {
      closeMenu();
      return;
    }
    open.value = true;
    error.value = "";
    await nextTick();
    panelEl.value?.querySelector<HTMLElement>(".conn-row")?.focus();
  }

  function closeMenu(): void {
    if (!open.value) return;
    open.value = false;
    pillEl.value?.focus();
  }

  async function commit(next: SavedInstance[]): Promise<void> {
    try {
      const saved = await saveInstances(next);
      rows.value = saved;
      offline.value = false;
      error.value = "";
      writeInstanceCache({ list: saved, lastUsed: lastUsed.value });
      draftName.value = "";
      draftURL.value = "";
    } catch (e) {
      error.value = (e as Error).message;
    }
  }

  async function add(): Promise<void> {
    error.value = "";
    let name = "";
    let url = "";
    try {
      name = normalizeInstanceName(draftName.value);
      url = normalizeInstanceURL(draftURL.value);
    } catch (e) {
      error.value = (e as Error).message;
      return;
    }
    const next = upsertInstance(rows.value, { name, url });
    if (next.length > MAX_INSTANCES) {
      error.value = `at most ${MAX_INSTANCES} connections`;
      return;
    }
    await commit(next);
  }

  async function remove(row: SavedInstance): Promise<void> {
    await commit(removeInstance(rows.value, row.url));
  }

  function switchTo(row: SavedInstance): void {
    lastUsed.value = row.url;
    open.value = false;
    navigateToInstance(row.url);
  }
</script>

<style scoped>
  .conn {
    position: relative;
    display: flex;
    align-items: center;
  }
  .conn-pill {
    display: flex;
    align-items: center;
    gap: 6px;
    height: 24px;
    max-width: 240px;
    padding: 0 8px;
    background: var(--z1);
    color: var(--text-dim);
    border: 1px solid transparent;
    border-radius: var(--r-chip);
    font-family: var(--mono);
    font-size: 11.5px;
    cursor: pointer;
  }
  .conn-pill:hover,
  .conn-pill[aria-expanded="true"] {
    color: var(--text);
    border-color: var(--stroke);
  }
  .conn-label {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .conn-dot {
    flex: none;
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--ok);
  }
  /* absence reads dim: an offline list is a stale cache, not a health claim. */
  .conn-dot.offline {
    background: var(--text-dim);
    opacity: 0.6;
  }
  .conn-panel {
    position: absolute;
    top: calc(100% + 4px);
    left: 0;
    z-index: 50;
    display: flex;
    flex-direction: column;
    gap: 2px;
    width: 320px;
    max-height: 60vh;
    overflow-y: auto;
    padding: 6px;
    background: var(--z2);
    border: 1px solid var(--stroke);
    border-radius: var(--r-card);
    box-shadow: var(--shadow-2);
  }
  .conn-note,
  .conn-hint,
  .conn-empty {
    margin: 2px 6px;
    font-size: 11px;
    color: var(--text-dim);
  }
  .conn-note {
    opacity: 0.75;
  }
  .conn-row {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 5px 6px;
    border-radius: var(--r-chip);
    color: var(--text);
    font: inherit;
    text-align: left;
  }
  .conn-row:hover {
    background: var(--z1-raised);
  }
  button.conn-row {
    width: 100%;
    background: 0 0;
    border: 0;
    cursor: pointer;
  }
  .conn-row-open {
    display: flex;
    align-items: center;
    gap: 6px;
    flex: 1 1 auto;
    min-width: 0;
    padding: 0;
    background: 0 0;
    border: 0;
    color: inherit;
    font: inherit;
    text-align: left;
    cursor: pointer;
  }
  .conn-check {
    flex: none;
    width: 11px;
    color: var(--accent);
    font-size: 12px;
    text-align: center;
  }
  .conn-row-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .conn-row-url {
    flex: 1 1 auto;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-dim);
    font-family: var(--mono);
    font-size: 11px;
  }
  .conn-row-used {
    flex: none;
    color: var(--text-dim);
    font-size: 10px;
    opacity: 0.7;
  }
  .conn-remove {
    flex: none;
    display: flex;
    align-items: center;
    padding: 4px;
    background: 0 0;
    border: 0;
    border-radius: var(--r-chip);
    color: var(--text-dim);
    cursor: pointer;
  }
  .conn-remove:hover {
    color: var(--err);
  }
  .conn-add {
    display: grid;
    grid-template-columns: 1fr;
    gap: 4px;
    margin-top: 4px;
    padding-top: 6px;
    border-top: 1px solid var(--stroke);
  }
  .conn-heading {
    margin: 0 6px;
    font-size: 11px;
    color: var(--text-dim);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .conn-input {
    margin: 0 2px;
    padding: 4px 6px;
    background: var(--z1);
    border: 1px solid var(--stroke);
    border-radius: var(--r-chip);
    color: var(--text);
    font-family: var(--mono);
    font-size: 11.5px;
  }
  .conn-input:focus {
    outline: none;
    border-color: var(--accent);
  }
  .conn-error {
    margin: 0 6px;
    font-size: 11px;
    color: var(--err);
  }
  .conn-save {
    justify-self: end;
    margin: 2px;
    padding: 3px 12px;
    background: var(--z1-raised);
    border: 1px solid var(--stroke);
    border-radius: var(--r-chip);
    color: var(--text);
    font-size: 11.5px;
    cursor: pointer;
  }
  .conn-save:hover {
    border-color: var(--accent);
  }
  /*
   * the squeeze: the bar's floor with a labelled pill (brand + pill + four edge
   * toggles) is 371px, so a 340px window measured 371 and the settings pane never
   * flipped narrow. Below this breakpoint - the round stop above that floor - the
   * pill sheds its label (aria-label carries the name) and the panel opens as a
   * bar-wide sheet: a 320px dropdown anchored beside a 35px pill would overflow
   * the same window.
   */
  @media (max-width: 480px) {
    .conn-pill {
      gap: 4px;
      padding: 0 5px;
    }
    .conn-label {
      display: none;
    }
    .conn-panel {
      position: fixed;
      top: calc(var(--menubar-h) + 4px);
      left: 8px;
      right: 8px;
      width: auto;
    }
  }
</style>
