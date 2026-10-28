<template>
  <div
    class="composer"
    :class="{ expanded: expanded }">
    <div
      v-if="attachments.length"
      class="attach-strip">
      <div
        v-for="(img, i) in attachments"
        :key="i"
        class="attach-thumb"
        :title="'attached image ' + (i + 1)">
        <img :src="dataUrl(img)" alt="" />
        <button
          class="attach-remove"
          type="button"
          title="remove attachment"
          @click="$emit('remove-attachment', i)">&times;</button>
      </div>
    </div>
    <!-- the fork anchor (t_a62eeb43): a visible affordance while the
         next send is chained onto a historical message - the send POSTs
         /chat {fork} and runs FROM that point; × drops the anchor -->
    <div
      v-if="forkAnchor"
      class="fork-chip">
      <span>fork: next send continues from this point</span>
      <button
        class="fork-clear"
        type="button"
        title="drop the fork anchor"
        @click="$emit('clear-fork')">&times;</button>
    </div>
    <div class="composer-row">
      <button
        class="plus-btn"
        type="button"
        title="attach images"
        aria-label="attach images"
        @click="fileInput?.click()">+</button>
      <textarea
        ref="ta"
        :value="draft"
        :aria-expanded="paletteOpen"
        aria-haspopup="listbox"
        :aria-activedescendant="
          paletteOpen && active >= 0 ? 'composer-palette-opt-' + active : undefined
        "
        placeholder="message omega"
        rows="1"
        @input="onInput"
        @keydown="onKeydown"
        @keyup.left="onCaret"
        @keyup.right="onCaret"
        @keyup.home="onCaret"
        @keyup.end="onCaret"
        @click="onCaret"
        @paste="onPaste" />
    </div>
    <!-- the / command palette and @ mention picker (t_07aed3f9): the
         TUI's autocomplete surface on the webui's tokens. "/" at input
         start lists the commands (shell + module registry + skills);
         "@" lists workspace files plus the session:/skill: context
         injections. Expansion itself is server-side - the picker only
         types the tokens. -->
    <div
      v-if="paletteOpen"
      ref="paletteEl"
      class="composer-palette"
      role="listbox"
      id="composer-palette">
      <ul class="palette-list">
        <li
          v-for="(item, i) in paletteItems"
          :key="item.kind + ':' + item.name"
          :id="'composer-palette-opt-' + i"
          role="option"
          :aria-selected="i === active"
          :class="{ active: i === active }"
          @pointerdown.prevent
          @click="acceptItem(item)">
          <span class="palette-name">{{ item.name }}</span>
          <span class="palette-desc">{{ item.description }}</span>
        </li>
        <li
          v-if="paletteItems.length === 0"
          class="palette-none"
          role="option"
          aria-selected="false">
          no matches
        </li>
      </ul>
    </div>
    <input
      ref="fileInput"
      type="file"
      accept="image/*"
      multiple
      style="display: none"
      @change="onFilesPicked" />
    <button
      v-if="overflowing"
      class="expand-btn"
      type="button"
      :title="expanded ? 'collapse composer' : 'expand composer'"
      @click="toggleExpand">
      {{ expanded ? "▾" : "▴" }}
    </button>
    <div class="toolbar">
      <ModelSelect
        :model="model"
        :models="models"
        @update:model="$emit('update:model', $event)" />
      <!-- the session's working folder chip (card t_9de5b009): pick +
           apply = the app writes the session row; the folder's trust
           rules govern the session, its file operations root there -->
      <WorkspaceSelect
        :workspace="workspace"
        :disabled="!workspaceBound"
        @update:workspace="$emit('update:workspace', $event)" />
      <ThinkingSelect
        :level="thinking"
        @update:level="$emit('update:thinking', $event)" />
      <!-- ONE morphing button (t_357cbe9c; retry left in t_95d2bc34):
           while a run streams this is the stop control; idle it is send
           always - dimmed (disabled) when a draft and the attachments
           are both empty, armed once there is something to send. The
           replay affordance renders in the newest response's action row
           (ChatMessage's msg-retry chip), never here. -->
      <button
        class="send"
        :class="{ cancel: face === 'stop' }"
        type="button"
        :title="faceTitle"
        :disabled="face === 'send' && !canSend"
        @click="onButton">
        {{ faceLabel }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
  import ModelSelect from "./ModelSelect.vue";
  import ThinkingSelect from "./ThinkingSelect.vue";
  import WorkspaceSelect from "./WorkspaceSelect.vue";
  import {
    dataUrl,
    mediaTypeFromBase64,
    fetchFilesBrowse,
    fetchSessions,
    type OmegaImage,
  } from "../../composables/omega";
  import { composerFace } from "../../composables/chat";
  import {
    paletteCommands,
    filterCommands,
    mentionQueryAt,
    mentionPrefixOf,
    mentionInnerQuery,
    replaceMentionToken,
    clampIndex,
    fetchPaletteCommands,
    fetchPaletteSkills,
  } from "../../composables/commands";
  import type { PaletteItem } from "../../composables/commands";

  const props = defineProps<{
    draft: string;
    model: string;
    models: string[];
    // THIS session's run is streaming: the send button is CANCEL now
    busy: boolean;
    thinking: string;
    // the session's remembered working folder ("" = none) - the chip
    // shows it, picking a new one emits update:workspace
    workspace?: string;
    // false when no session is bound yet: a folder pins on a SESSION
    // row, and a draft has nothing to pin on - the chip disables
    workspaceBound?: boolean;
    // pending image attachments - owned by the app: the transcript
    // bubble and the interrupted retry both need them
    attachments: OmegaImage[];
    // set while the next send is chained onto a historical message
    // (the fork affordance on the response block) - shows the chip
    forkAnchor?: string | null;
  }>();

  const emit = defineEmits<{
    (e: "update:draft", value: string): void;
    (e: "update:model", value: string): void;
    (e: "update:thinking", value: string): void;
    (e: "update:workspace", value: string): void;
    (e: "send"): void;
    (e: "cancel"): void;
    (e: "attach-files", images: OmegaImage[]): void;
    (e: "remove-attachment", index: number): void;
    (e: "attach-url", url: string): void;
    (e: "clear-fork"): void;
  }>();

  // send needs text OR an attachment - image-only messages are valid
  const canSend = computed(() => !!props.draft.trim() || props.attachments.length > 0);

  // the morph (t_357cbe9c): busy -> stop, else send. The retry face left
  // with the affordance (t_95d2bc34) - the action row owns replay now.
  const face = computed(() => composerFace(props.busy));
  const faceLabel = computed(() => (face.value === "stop" ? "■ stop" : "send"));
  const faceTitle = computed(() =>
    face.value === "stop" ?
      "cancel the running response (what streamed stays)"
      : "send",
  );
  function onButton() {
    if (face.value === "stop") emit("cancel");
    else emit("send");
  }

  // grow with content: 1 row at rest, up to 4 rows (COMPOSER_MAX), then the
  // textarea scrolls. Past the cap an expand button appears (top right) to
  // grow the composer beyond it, up to 60% of the viewport.
  // Explicit 21px line-height in CSS makes the row math deterministic:
  // 4 rows = 84px of content height.
  const COMPOSER_MAX = 84;

  const MAX_ATTACH_BYTES = 10 * 1024 * 1024; // mirrors the backend's cap

  const ta = ref<HTMLTextAreaElement | null>(null);
  const fileInput = ref<HTMLInputElement | null>(null);
  const expanded = ref(false);
  const overflowing = ref(false);

  function resize() {
    const el = ta.value;
    if (!el) return;
    el.style.height = "auto";
    const full = el.scrollHeight;
    overflowing.value = full > COMPOSER_MAX + 1;
    if (expanded.value) {
      // expanded: the composer fills the chat pane (CSS flex) - no inline
      // height, the textarea stretches and scrolls
      el.style.height = "";
      return;
    }
    el.style.height = `${Math.min(full, COMPOSER_MAX)}px`;
  }

  function toggleExpand() {
    expanded.value = !expanded.value;
    resize();
  }

  // draft clears after send (parent) - shrink back on the next tick
  watch(
    () => props.draft,
    () => nextTick(resize),
  );

  // fileToImage reads one File into the ai.ImageContent shape: base64
  // (data: prefix stripped) plus a magic-sniffed media type. Returns
  // {img: null, reason} for oversize or non-image files - handleFiles
  // reports the first reason instead of silently dropping.
  async function fileToImage(
    file: File,
  ): Promise<{ img: OmegaImage | null; reason?: string }> {
    if (file.size > MAX_ATTACH_BYTES) {
      return {
        img: null,
        reason: `${file.name || "pasted image"} too large: ${(file.size / 1048576).toFixed(1)} MB (max 10 MB)`,
      };
    }
    const text = await new Promise<string>((resolve, reject) => {
      const r = new FileReader();
      r.onload = () => resolve(String(r.result));
      r.onerror = () => reject(new Error("read failed"));
      r.readAsDataURL(file);
    });
    const base64 = text.slice(text.indexOf(",") + 1);
    if (!base64) return { img: null, reason: "unreadable image data" };
    const magic = mediaTypeFromBase64(base64);
    if (!magic) {
      return {
        img: null,
        reason: `${file.name || "file"} is not a supported image (png, jpeg, gif, webp, bmp)`,
      };
    }
    return { img: { media_type: magic, base64 } };
  }

  async function handleFiles(files: FileList | File[]) {
    const images: OmegaImage[] = [];
    let firstReason = "";
    for (const file of Array.from(files)) {
      const { img, reason } = await fileToImage(file);
      if (img) images.push(img);
      else if (!firstReason && reason) firstReason = reason;
    }
    if (images.length) emit("attach-files", images);
    if (firstReason) window.alert(firstReason);
  }

  function onFilesPicked(e: Event) {
    const files = (e.target as HTMLInputElement).files;
    if (files?.length) void handleFiles(files);
    if (fileInput.value) fileInput.value.value = "";
  }

  function onPaste(e: ClipboardEvent) {
    // Clipboard image files win: paste attaches them
    const files = Array.from(e.clipboardData?.files ?? []);
    if (files.length) {
      void handleFiles(files);
      return;
    }
    // A lone plain-text URL offers attach-as-image - the SERVER fetches
    // the bytes (never cross-origin from the browser). Normal text
    // paste falls through untouched.
    const text = (e.clipboardData?.getData("text/plain") ?? "").trim();
    if (/^https?:\/\/\S+$/i.test(text)) {
      emit("attach-url", text);
    }
  }

  // --- the / command palette and @ mention picker (t_07aed3f9) ---
  //
  // Derived from draft + caret. "/" as the input's first char with the
  // caret still inside its first word = the slash list; a running
  // "@..." token at the caret = the mention list. Esc sets dismissed
  // until the next draft change - a dropdown stays closed until a new
  // query re-opens it.
  //
  // NOTE: the mode is DERIVED (mention.value.active), never stored in
  // an imperative ref flipped from onInput - that flip raced the draft
  // prop update (the handler sees the PREVIOUS emit's draft) and the
  // bare-@ list rendered empty one beat too late.

  const active = ref(-1);
  const dismissed = ref(false);
  // the caret the palette derives from (token surgery needs it)
  const caretAt = ref(0);

  // lazy seam snapshots: the extension command registry and the skills
  // list fetch once per mount; session and workspace-file rows
  // refetch on every mention open (cheap local reads, never the agent)
  const extCmds = ref<{ name: string; description?: string }[]>([]);
  const skillRows = ref<{ name: string; description: string }[]>([]);
  const sessionRows = ref<{ id: string; label: string }[]>([]);
  const fileRows = ref<{ name: string }[]>([]);

  let paletteLoaded = false;
  async function loadPaletteData() {
    if (paletteLoaded) return;
    paletteLoaded = true;
    const [ext, sk] = await Promise.all([fetchPaletteCommands(), fetchPaletteSkills()]);
    extCmds.value = ext as { name: string; description?: string }[];
    skillRows.value = sk;
  }

  async function loadMentionRows() {
    const [sess, files] = await Promise.all([
      fetchSessions().catch(() => []),
      fetchFilesBrowse("").catch(() => ({ entries: [] })),
    ]);
    sessionRows.value = sess.map((s) => ({ id: s.id, label: s.label || "" }));
    fileRows.value = files.entries
      .filter((e) => !e.is_dir)
      .map((e) => ({ name: e.name }))
      .slice(0, 50);
  }

  const mention = computed(() => mentionQueryAt(props.draft, caretAt.value));

  const paletteOpen = computed(() => {
    if (dismissed.value) return false;
    // slash lists while the caret is still inside the command word -
    // past the first space it is arg territory, the palette stands down
    if (props.draft.startsWith("/")) {
      const space = props.draft.indexOf(" ");
      if (caretAt.value <= (space === -1 ? props.draft.length : space)) return true;
    }
    return mention.value.active;
  });

  // allItems branches on the DERIVED mode (see the note above): the
  // mention computeds, else the slash list - slash mode when the draft
  // starts with "/", and empty otherwise.
  const allItems = computed<PaletteItem[]>(() => {
    if (!paletteOpen.value) return [];
    if (mention.value.active) {
      const tok = mention.value.token;
      const prefix = mentionPrefixOf(tok);
      const inner = mentionInnerQuery(tok);
      if (prefix === "session:") {
        return sessionRows.value.map((s) => ({
          name: "session:" + s.id,
          description: s.label || s.id,
          kind: "session" as const,
        }));
      }
      if (prefix === "skill:") {
        return skillRows.value.map((s) => ({
          name: "skill:" + s.name,
          description: s.description,
          kind: "skill" as const,
        }));
      }
      // bare @: workspace-root files plus the two prefix rows
      const prefixRows: PaletteItem[] = [
        { name: "session:", description: "inject a session's history", kind: "session" },
        { name: "skill:", description: "inject a skill's content", kind: "skill" },
      ];
      const files: PaletteItem[] = fileRows.value.map((f) => ({
        name: f.name,
        description: "inline this file (images attach)",
        kind: "file" as const,
      }));
      const rows = [...prefixRows, ...files];
      return inner ? filterCommands(inner, rows) : rows;
    }
    if (!props.draft.startsWith("/")) return [];
    const query = props.draft.slice(0, caretAt.value).replace(/\s.*/, "");
    return filterCommands(query, paletteCommands(extCmds.value, skillRows.value));
  });

  // the dropdown scrolls in CSS (.palette-list max-height + overflow-y):
  // the cap only guards a runaway extension roster from rendering the
  // whole DOM, it doesn't truncate a reachable row
  const PALETTE_MAX = 100;
  const paletteItems = computed(() => allItems.value.slice(0, PALETTE_MAX));

  // keep the keyboard highlight visible while the list scrolls
  const paletteEl = ref<HTMLElement | null>(null);
  watch(active, (i) => {
    const opts = paletteEl.value?.querySelectorAll('[role=option]');
    const el = i >= 0 && opts ? opts[i] : null;
    el?.scrollIntoView({ block: 'nearest' });
  });

  watch(paletteItems, (items) => {
    if (items.length === 0) active.value = -1;
    else if (active.value < 0 || active.value >= items.length) active.value = 0;
  });

  function onInput(e: Event) {
    const el = e.target as HTMLTextAreaElement;
    caretAt.value = el.selectionStart ?? 0;
    dismissed.value = false;
    emit("update:draft", el.value);
    resize();
  }

  function onCaret(e: Event) {
    const el = e.target as HTMLTextAreaElement;
    caretAt.value = el.selectionStart ?? 0;
  }

  // the lazy loads ride mode transitions reactively (same race note as
  // above): a mention opening loads its rows once; the slash surface
  // loads the command registry + skills snapshot
  const mentionActive = computed(() => mention.value.active);
  const slashSurface = computed(() => !mentionActive.value && props.draft.startsWith("/"));
  watch(mentionActive, (on) => {
    if (on) {
      void loadMentionRows();
      // the skill: prefix rows ride the same snapshot the slash
      // palette uses - a cold session that opens @ directly needs it
      void loadPaletteData();
    }
  });
  watch(slashSurface, (on) => {
    if (on) void loadPaletteData();
  });
  onMounted(() => {
    if (slashSurface.value) void loadPaletteData();
  });

  function closePalette() {
    dismissed.value = true;
  }

  // acceptItem: mention rows insert their completed token at the caret;
  // command rows complete the draft to the command (typed args ride
  // along) and SUBMIT - selecting a command runs it, no agent
  // round-trip ever (the palette's law: commands act locally).
  function acceptItem(item: PaletteItem) {
    if (!paletteOpen.value) return;
    if (!mention.value.active && (item.kind === "shell" || item.kind === "extension" || item.kind === "skill")) {
      // preserve any args typed after the command word
      const space = props.draft.indexOf(" ");
      const rest = space === -1 ? "" : props.draft.slice(space);
      closePalette();
      emit("update:draft", item.name + rest);
      emit("send");
      active.value = -1;
      return;
    }
    const insertion = "@" + item.name;
    const next = replaceMentionToken(props.draft, caretAt.value, mention.value.token, insertion);
    closePalette();
    emit("update:draft", next.draft);
    active.value = -1;
    nextTick(() => {
      const el = ta.value;
      if (el && next.caret >= 0) {
        el.focus();
        el.setSelectionRange(next.caret, next.caret);
        caretAt.value = next.caret;
      }
    });
  }

  // Tab completes without submitting (TUI acceptMatch parity): the
  // draft becomes the completed token, the palette closes.
  function acceptActive(submit: boolean) {
    const item = paletteItems.value[active.value];
    if (!item) return closePalette();
    if (submit) return acceptItem(item);
    if (!mention.value.active) {
      emit("update:draft", item.name);
      closePalette();
      return;
    }
    // a mention Tab completes too (TUI acceptMatch parity): the token
    // inserts at the caret, palette closes, no send
    return acceptItem(item);
  }

  function onKeydown(e: KeyboardEvent) {
    if (paletteOpen.value) {
      const len = paletteItems.value.length;
      if (e.key === "ArrowDown") {
        e.preventDefault();
        active.value = clampIndex(active.value, 1, len);
        return;
      }
      if (e.key === "ArrowUp") {
        e.preventDefault();
        active.value = clampIndex(active.value, -1, len);
        return;
      }
      if (e.key === "Escape") {
        e.preventDefault();
        e.stopPropagation();
        closePalette();
        return;
      }
      if (e.key === "Tab") {
        e.preventDefault();
        acceptActive(false);
        return;
      }
      if (e.key === "Enter") {
        // the palette owns Enter while it is open: the highlighted
        // command runs, the highlighted mention inserts
        e.preventDefault();
        acceptActive(true);
        return;
      }
    }
    if (e.key === "Enter") {
      e.preventDefault();
      emit("send");
    }
  }

  // composer auto-focus (card t_b40131ac): the ChatTab parent delivers
  // focus-on-select and type-to-capture through this handle - focusing
  // the textarea IS the feature, the character then lands via the input
  // event (focus moves during keydown, insertion targets the focused
  // element, no synthetic draft injection)
  defineExpose({
    focus: () => {
      ta.value?.focus();
    },
  });
</script>