<template>
  <div
    class="composer"
    :class="[composerModeClass, { expanded: expanded }]"
    @click="onCanvasClick">
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
    <!-- the reference CHIPS (card t_e67c372b): the draft's /skill and
         @file tokens render INLINE in the editor - a pill REPLACES the
         text it represents (chip replaces text), the raw draft string
         keeps the tokens throughout. The chip is non-editable; click
         opens the target, its x removes the token, Backspace with the
         caret behind it dissolves the chip back to the raw token text
         (the dissolve law). The strip is gone: pills sit beside their
         text was the whole complaint. -->
    <div class="composer-row">
      <button
        v-if="!expanded"
        class="plus-btn"
        type="button"
        title="attach images"
        aria-label="attach images"
        @click="fileInput?.click()">+</button>
      <!-- THE EDITOR (card t_e67c372b): a contenteditable div, not a
           textarea - chips must render inline in the text flow. The
           div's children are NEVER Vue-patched (the user's typed DOM is
           the live truth; the template deliberately leaves the element
           empty) - the sync module below rebuilds children from the
           draft only when the plan and the DOM disagree, so native
           typing never churns the caret. Draft extraction carries the
           raw tokens out of the chips' data-token, keeping the draft
           string the server-side expansion's exact truth. -->
      <div
        ref="ta"
        class="composer-input"
        contenteditable="true"
        role="textbox"
        aria-multiline="true"
        :data-placeholder="'message omega'"
        :aria-expanded="paletteOpen"
        aria-haspopup="listbox"
        :aria-activedescendant="
          paletteOpen && active >= 0 ? 'composer-palette-opt-' + active : undefined
        "
        spellcheck="false"
        @input="onInput"
        @keydown="onKeydown"
        @keyup.left="onCaret"
        @keyup.right="onCaret"
        @keyup.up="onCaret"
        @keyup.down="onCaret"
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
      v-if="overflowing || expanded"
      class="expand-btn"
      type="button"
      :title="expanded ? 'collapse composer' : 'expand composer'"
      @click="toggleExpand">
      {{ expanded ? "▾" : "▴" }}
    </button>
    <!-- the fullscreen bottom control row (card t_08ef793b): the + joins
         the chips and send here - the expanded state's one bottom row.
         v-if keeps the two + seats exclusive: compact keeps the row seat,
         fullscreen the toolbar seat - the compact DOM never changes. -->
    <div
      class="toolbar"
      :class="{ 'composer-bottom': expanded }">
      <button
        v-if="expanded"
        class="plus-btn"
        type="button"
        title="attach images"
        aria-label="attach images"
        @click="fileInput?.click()">+</button>
      <!-- control row order (v0.14.3, user ruling): workspace FIRST,
           then the model name, then the effort pill - the folder the
           session runs in is the row's first citizen -->
      <!-- the workspace chip (t_9de5b009, draft intent t_ea7e6a58):
           enabled pre-session too - unbound, it holds the draft's
           folder intent; bound, it rides the session row -->
      <WorkspaceSelect
        :workspace="workspace"
        @update:workspace="$emit('update:workspace', $event)" />
      <div class="model-group">
        <!-- model + effort are ONE wrap unit (t_641c2870, Endo
             2026-10-29): the toolbar wraps wherever it runs out of room,
             but the model chip must never strand the effort chip on a
             second row - as one flex item (.model-group, nowrap) the
             pair crosses any wrap boundary together or not at all -->
        <ModelSelect
          :model="model"
          :models="models"
          :groups="groups"
          @update:model="$emit('update:model', $event)"
          @refresh-models="$emit('refresh-models')" />
        <!-- the effort chip rides the shared BandSelect (v0.14.1 review
             lens: the dedicated chip+listbox was BandSelect's exact
             grammar minus nothing - one component, one grammar; only
             the pop's narrow floor survives, in main.css) -->
        <BandSelect
          class="thinking-select"
          label="Reasoning effort"
          :options="thinkingOptions"
          :model-value="thinking"
          @update:model-value="$emit('update:thinking', $event)" />
      </div>
      <!-- the workspace chip's note lives in WorkspaceSelect.vue (card
           t_9de5b009); this row orders workspace, model, effort -->
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
  import WorkspaceSelect from "./WorkspaceSelect.vue";
  import BandSelect from "../ui/BandSelect.vue";
  import ReferencePill from "./ReferencePill.vue";
  import { computed, getCurrentInstance, h, render } from "vue";
  import {
    dataUrl,
    mediaTypeFromBase64,
    fetchFilesBrowse,
    fetchSessions,
    fetchWorkspaceFiles,
    THINKING_UI_LEVELS,
    type FileEntry,
    type OmegaImage,
  } from "../../composables/omega";
  import { composerFace } from "../../composables/chat";
  import {
    paletteCommands,
    filterCommands,
    mentionQueryAt,
    mentionPrefixOf,
    replaceMentionToken,
    clampIndex,
    fetchPaletteCommands,
    fetchPaletteSkills,
  } from "../../composables/commands";
  import type { PaletteItem } from "../../composables/commands";
  // the pill token model (t_7f09bd0c): the strip scan, the Backspace
  // token surgery, and the @ path list's dir-first helpers share one
  // module with the sent-message renderer
  import {
    chipFor,
    deleteTokenAt,
    editorNodes,
    filterMentionRows,
    mentionDirOf,
    mentionRows,
    scanRefTokens,
    tokenBeforeCaret,
  } from "../../composables/pills";
  import type {
    EditorChipNode,
    EditorPiece,
    RefToken,
  } from "../../composables/pills";
  import { DOCK_VIEWER_OPEN_KEY } from "../../composables/viewer";
  // the composer's display mode (card t_5752861e, reshape t_f9fd3820):
  // one shared signal - SettingsTab writes it, the class binding carries
  // mode-compact/mode-full (wide is the base shape, no class)
  import { parseComposerMode, useComposerMode } from "../../composables/composerMode";
  const props = defineProps<{
    draft: string;
    model: string;
    models: string[];
    /** the grouped catalog (card t_1914a8c9): one section per provider -
         the picker renders collapsible sections from these, "default"
         first; empty falls back to the flat models prop */
    groups?: { name: string; models: string[]; capabilities?: Record<string, string[]>; error?: string }[];
    // THIS session's run is streaming: the send button is CANCEL now
    busy: boolean;
    thinking: string;
    // the working folder the chip shows: a bound session's pinned row
    // folder, or - while the tab is unbound - the draft's workspace
    // intent (t_ea7e6a58). Picking a new one emits update:workspace
    // with either meaning; the parent stores it.
    workspace?: string;
    // pending image attachments - owned by the app: the transcript
    // bubble and the interrupted retry both need them
    attachments: OmegaImage[];
    // set while the next send is chained onto a historical message
    // (the fork affordance on the response block) - shows the chip
    forkAnchor?: string | null;
  }>();

  // the display mode's shared signal (card t_5752861e): module ref, no
  // prop-drilling - the chat group's knob writes it straight
  const composerMode = useComposerMode();

  const emit = defineEmits<{
    (e: "update:draft", value: string): void;
    (e: "update:model", value: string): void;
    (e: "update:thinking", value: string): void;
    (e: "update:workspace", value: string): void;
    (e: "send"): void;
    (e: "cancel"): void;
    (e: "refresh-models"): void;
    (e: "attach-files", images: OmegaImage[]): void;
    (e: "remove-attachment", index: number): void;
    (e: "attach-url", url: string): void;
    (e: "clear-fork"): void;
  }>();

  // send needs text OR an attachment - image-only messages are valid
  const canSend = computed(() => !!props.draft.trim() || props.attachments.length > 0);

  // the effort chip's option ladder: same value for value + label (the
  // UI level IS the label - the wire mapping lives in omega.ts)
  const thinkingOptions = computed(() =>
    THINKING_UI_LEVELS.map((level) => ({ value: level, label: level })),
  );

  // the display mode's class (card t_5752861e, reshape t_f9fd3820):
  // wide is the unmodified base shape (pre-modes geometry, no class);
  // compact and full carry their modifier classes
  const composerModeClass = computed(() => {
    const m = parseComposerMode(composerMode.value);
    return m === "wide" ? null : `mode-${m}`;
  });

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
  // grow the composer beyond it - the FULLSCREEN state (card t_08ef793b): a
  // floating card where the text region is the tall element and the control
  // row (+, chips, send) pins to the card's bottom edge.
  // Explicit 21px line-height in CSS makes the row math deterministic:
  // 4 rows = 84px of content height.
  const COMPOSER_MAX = 84;

  const MAX_ATTACH_BYTES = 10 * 1024 * 1024; // mirrors the backend's cap

  const ta = ref<HTMLElement | null>(null);
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
    // the height math reads state-dependent layout: the class must land in
    // the DOM first. Measuring synchronously (the old shape) read the
    // STRETCHED textarea's scrollHeight on collapse - the compact height
    // pinned to the old 84px cap instead of the content's own height.
    nextTick(() => {
      resize();
      // fullscreen hands the canvas to the input immediately: the cursor
      // waits in the grown region the moment the toggle lands (card
      // t_08ef793b ruling 1 - the big space is typeable canvas)
      if (expanded.value) ta.value?.focus();
    });
  }

  // fullscreen canvas law (card t_08ef793b): a click on empty composer
  // space - the grown region's background, the chevron's gutter - lands the
  // caret. Control clicks (chips, send, pills, strips, palette) pass
  // through untouched, so chip menus and the palette behave as usual.
  function onCanvasClick(e: MouseEvent) {
    if (!expanded.value) return;
    const el = e.target as HTMLElement | null;
    if (!el || el === ta.value) return;
    if (
      el.closest(
        "button, input, [role=listbox], .composer-palette, .attach-strip, .fork-chip, .ref-chip, .model-select, .thinking-select, .workspace-select, .toolbar",
      )
    ) {
      return;
    }
    ta.value?.focus();
  }

  // the height follows every draft write: the plan watch fires the sync
  // (a no-op with chip parity, a rebuild otherwise) and resize runs in
  // both of its paths - the textarea-era draft watch folded into it.

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
  const fileRows = ref<FileEntry[]>([]);

  let paletteLoaded = false;
  async function loadPaletteData() {
    if (paletteLoaded) return;
    paletteLoaded = true;
    const [ext, sk] = await Promise.all([fetchPaletteCommands(), fetchPaletteSkills()]);
    extCmds.value = ext as { name: string; description?: string }[];
    skillRows.value = sk;
  }

  async function loadMentionRows() {
    const sess = await fetchSessions().catch(() => []);
    sessionRows.value = sess.map((s) => ({ id: s.id, label: s.label || "" }));
  }

  // the @ file rows (card t_7f09bd0c): ONE directory's listing at a time -
  // the token's dir (mentionDirOf). A draft workspace routes through the
  // workspace reader (an absolute-path read), a rootless one through
  // /files/browse (the resolved root + relative dir). The cached dir
  // skips refetches of the same listing; a mention close resets it so a
  // reopen sees the workspace that is bound NOW.
  const fileDir = ref<string | null>(null);
  const fileLoading = ref(false);
  async function loadFileRows(dir: string) {
    if (fileDir.value === dir || fileLoading.value) return;
    fileDir.value = dir;
    fileLoading.value = true;
    try {
      const payload = props.workspace ?
        await fetchWorkspaceFiles(props.workspace + (dir ? "/" + dir.replace(/\/$/, "") : ""))
      : await fetchFilesBrowse(dir);
      fileRows.value = (payload.entries ?? []) as FileEntry[];
    } catch {
      fileRows.value = [];
    } finally {
      fileLoading.value = false;
    }
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
      // bare @: the two prefix rows plus the token's dir listing - the
      // rows are dirs first (mentionRows, t_7f09bd0c); the leaf filters
      // (a drilled token's path part is its dir, not query text)
      const prefixRows: PaletteItem[] = [
        { name: "session:", description: "inject a session's history", kind: "session" },
        { name: "skill:", description: "inject a skill's content", kind: "skill" },
      ];
      const rows: PaletteItem[] = [...prefixRows, ...mentionRows(fileRows.value)];
      return filterMentionRows(tok, rows);
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
    absorbWrite(e.target as HTMLElement);
  }

  function onCaret(e: Event) {
    const el = e.target as HTMLElement;
    const at = caretFromDom(el);
    if (at >= 0) caretAt.value = at;
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
  // the @ drill (t_7f09bd0c): the token's dir drives the file rows - a
  // typed or drilled path lists its own directory, a closed mention
  // resets the cache dir so the next open refetches with the workspace
  // that is bound NOW
  watch(
    () => (mentionActive.value ? mentionDirOf(mention.value.token) : null),
    (dir) => {
      if (dir === null) {
        fileDir.value = null;
        return;
      }
      void loadFileRows(dir);
    },
  );
  watch(slashSurface, (on) => {
    if (on) void loadPaletteData();
  });
  onMounted(() => {
    if (slashSurface.value) void loadPaletteData();
    // a remount carrying a draft (tab switches keep per-tab state, card
    // t_e67c372b): the chips rebuild - and skill chips need the skills
    // snapshot the slash surface would otherwise only load on the next
    // "/" press
    if (scanRefTokens(props.draft).some((t) => t.kind === "skill")) {
      void loadPaletteData();
    }
    syncEditor();
  });

  function closePalette() {
    dismissed.value = true;
  }

  // --- the chip editor (t_e67c372b): a pill REPLACES the text it ---
  // --- represents, inline in the flow                              ---
  //
  // The editor is a contenteditable div (a textarea cannot render markup
  // in place). Its children are owned by this module, never by Vue's
  // patcher: native typing mutates the DOM freely, and the sync rewrites
  // the children ONLY when the plan (the chip law over the draft) and
  // the DOM disagree - a rebuild restores the caret at the same
  // draft-space index, so the cursor never visibly jumps. The draft
  // string always carries the RAW tokens: chips are a view of their
  // spans (data-token carries the span out), and the send posts the
  // same text the strip-era flow posted (the server-side at-mention
  // expansion is the consumer of that exact string).

  // chip eligibility: a file token stands as-is; a skill token chips as
  // soon as its name is a loaded skill's (a shell verb like "/theme" is
  // a command, never a pill - the slash surface has already loaded the
  // skills snapshot for any "/"-draft)
  const skillNames = computed(() => new Set(skillRows.value.map((s) => s.name)));
  const isPill = (t: RefToken): boolean =>
    t.kind === "file" || skillNames.value.has(t.name);

  // fresh swallows (the accept path) and dissolved reveals (the
  // Backspace law). Keys ride chipKeyOf's token identity; reactive Set
  // instances are replaced whole so the plan's computed re-runs.
  const forcedChip = ref<ReadonlySet<string>>(new Set());
  const dissolvedChip = ref<ReadonlySet<string>>(new Set());

  const editorPlan = computed<EditorPiece[]>(() =>
    editorNodes(
      props.draft,
      {
        caret: caretAt.value,
        paletteOpen: paletteOpen.value,
        forced: forcedChip.value,
        dissolved: dissolvedChip.value,
      },
      isPill,
    ),
  );

  // --- caret arithmetic (draft space <-> editor DOM) ---
  //
  // A chip contributes its token's full length to draft space but
  // offers exactly two caret seats: before it (the token's start index)
  // and after it (the span's end index). A token's interior indexes
  // exist in draft space but never in the chip'd DOM - a chip there
  // means the span is raw (dissolved or being typed), an ordinary text
  // node the caret can sit inside.

  // draftTextOf reads the editor's text: text nodes verbatim, chips via
  // their data-token. Stray browser elements walk into (they carry no
  // draft text that matters; the input paths never create them).
  function draftTextOf(el: HTMLElement): string {
    let out = "";
    const walk = (n: Node) => {
      for (const c of n.childNodes) {
        if (c.nodeType === Node.TEXT_NODE) {
          out += c.nodeValue ?? "";
        } else if (c instanceof Element) {
          if (c.dataset.refchip !== undefined) out += c.dataset.token ?? "";
          else walk(c);
        }
      }
    };
    walk(el);
    return out;
  }

  // caretFromDom maps the selection's collapsed caret to a draft index;
  // -1 means "no collapsed caret in the editor" (a ranged selection,
  // focus elsewhere) - callers fall back rather than guess.
  function caretFromDom(el: HTMLElement): number {
    const sel = window.getSelection();
    if (!sel?.rangeCount) return -1;
    const range = sel.getRangeAt(0);
    if (!range.collapsed) return -1;
    const anchor = range.startContainer;
    if (!el.contains(anchor)) return -1;
    const kids = Array.from(el.childNodes);
    let idx = 0;
    for (let i = 0; i < kids.length; i++) {
      const c = kids[i]!;
      // the caret anchors on the editor itself: offset i = before the
      // i-th child; offset kid-count = after everything (the loop end)
      if (anchor === el) {
        if (range.startOffset === i) return idx;
      } else if (c === anchor) {
        // the caret anchors on the child: a text node's offset, or an
        // element's start edge (defensive - the chip swallows mousedown)
        return c.nodeType === Node.TEXT_NODE ?
            idx + Math.min(range.startOffset, (c.nodeValue ?? "").length)
          : idx;
      }
      if (c.nodeType === Node.TEXT_NODE) idx += (c.nodeValue ?? "").length;
      else if (c instanceof Element && c.dataset.refchip !== undefined)
        idx += (c.dataset.token ?? "").length;
    }
    if (anchor === el && range.startOffset === kids.length) return idx;
    return -1; // an anchor deeper than the flat child list: not representable
  }

  // placeCaretAt puts the caret at a draft-space index in the DOM: text
  // interiors select into the text node, chip boundaries select against
  // the editor element (before/after the chip), the end collapses at the
  // child list's end. A silent no-op while the editor is not focused.
  function placeCaretAt(el: HTMLElement, index: number): void {
    if (document.activeElement !== el) return;
    const sel = window.getSelection();
    if (!sel) return;
    const kids = Array.from(el.childNodes);
    let idx = 0;
    for (let i = 0; i < kids.length; i++) {
      const c = kids[i]!;
      const len =
        c instanceof Element && c.dataset.refchip !== undefined ?
          (c.dataset.token ?? "").length
          : (c.nodeValue ?? "").length;
      if (index === idx) {
        sel.collapse(el, i);
        return;
      }
      if (index > idx && index < idx + len && c.nodeType === Node.TEXT_NODE) {
        sel.collapse(c, index - idx);
        return;
      }
      if (index === idx + len) {
        sel.collapse(el, i + 1);
        return;
      }
      idx += len;
    }
    sel.collapse(el, kids.length);
  }

  // --- the chip DOM + the sync ---

  const appContext = getCurrentInstance()?.appContext ?? null;

  // buildChip mounts ONE renderer's instance into a non-editable span:
  // ReferencePill stays the ONE renderer (composer chip + transcript
  // bubble = the same component, the same click law - open through the
  // viewer inject). The wrapper carries the raw token in data-token
  // (the draft reader's exact source), swallows mousedown (an element
  // selection would turn the next Backspace into a whole-chip delete -
  // the dissolve law needs the parked caret) and dragstart.
  function buildChip(t: RefToken): HTMLElement {
    const wrap = document.createElement("span");
    wrap.className = "ref-chip";
    wrap.contentEditable = "false";
    wrap.dataset.refchip = "";
    wrap.dataset.token = t.token;
    wrap.addEventListener("mousedown", (e) => e.preventDefault());
    wrap.addEventListener("dragstart", (e) => e.preventDefault());
    // chip click (t_7f09bd0c's law): a file opens in the Files pane, a
    // skill opens its SKILL.md. The x keeps the old strip's outright
    // removal; Backspace is the dissolve.
    const vnode = h("span", { class: "ref-chip-body" }, [
      h(ReferencePill, { pill: t, onOpen: () => openRef(t) }),
      h(
        "button",
        {
          class: "ref-remove",
          type: "button",
          title: "remove " + t.name,
          onClick: () => removeToken(t),
        },
        "\u00d7",
      ),
    ]);
    if (appContext) vnode.appContext = appContext;
    render(vnode, wrap);
    return wrap;
  }

  // openRef: the viewer inject the Files tab's row clicks ride
  const openViewerRef = inject(DOCK_VIEWER_OPEN_KEY, null);
  function openRef(t: RefToken) {
    openViewerRef?.({ kind: t.kind, name: t.name });
  }

  // sync brings the editor's DOM to the plan and rebuilds ONLY on
  // disagreement (the DOM's text out of parity with the draft, or the
  // chip sequence drifted) - typing and caret moves take the no-op path
  // and never churn the caret. Stale chip-state keys sweep first: a
  // token that left the scan (edited into prose, removed) releases its
  // forced/dissolved entry.
  function syncEditor(): void {
    const el = ta.value;
    if (!el) return;
    const live = new Set(scanRefTokens(props.draft).map((t) => t.token));
    let swept = false;
    for (const setref of [forcedChip, dissolvedChip] as const) {
      let next: Set<string> | null = null;
      for (const k of setref.value) {
        if (!live.has(k)) {
          if (!next) next = new Set(setref.value);
          next.delete(k);
          swept = true;
        }
      }
      if (next) {
        (setref as { value: ReadonlySet<string> }).value = next;
      }
    }
    const domText = draftTextOf(el);
    const domChips = Array.from(el.children)
      .filter((c) => c instanceof Element && c.dataset.refchip !== undefined)
      .map((c) => (c as HTMLElement).dataset.token ?? "");
    const wantChips = editorPlan.value
      .filter((p): p is EditorChipNode => p.type === "chip")
      .map((p) => p.token.token);
    if (domText === props.draft && domChips.join("\u0000") === wantChips.join("\u0000")) {
      // parity: maybe resize anyway (heights follow plain text edits,
      // whose own flush took the plan watch here)
      resize();
      return;
    }
    caretAt.value = Math.min(caretAt.value, props.draft.length);
    for (const c of Array.from(el.children)) {
      if (c instanceof Element && c.dataset.refchip !== undefined) {
        render(null, c); // unmount the renderer instance (no leak)
      }
    }
    while (el.firstChild) el.removeChild(el.firstChild);
    for (const p of editorPlan.value) {
      if (p.type === "text") el.appendChild(document.createTextNode(p.text));
      else el.appendChild(buildChip(p.token));
    }
    placeCaretAt(el, caretAt.value);
    resize();
  }

  // the sync triggers: any plan change (draft text, caret, chip state)
  // rebuilds the chip layer after the DOM settles
  watch(editorPlan, () => nextTick(syncEditor));

  // dissolveBehindCaret runs the Backspace law (t_e67c372b): the caret
  // collapsed and immediately behind a CHIPPED token - the chip dies and
  // the raw token text re-materializes. A reveal, not a delete: the
  // draft string is unchanged (the token was always in it), the reveal
  // is suppressed while the caret works the span, and leaving the span
  // re-chips it. Returns false so the default backspace runs (a ranged
  // selection, no chip behind the caret, a dissolved span's next
  // single-char delete).
  function dissolveBehindCaret(): boolean {
    const el = ta.value;
    if (!el) return false;
    const caret = caretFromDom(el);
    if (caret === -1) return false;
    const tok = tokenBeforeCaret(props.draft, caret);
    if (!tok || !isPill(tok)) return false;
    if (
      !chipFor(tok, {
        caret,
        paletteOpen: paletteOpen.value,
        forced: forcedChip.value,
        dissolved: dissolvedChip.value,
      })
    ) {
      return false;
    }
    if (forcedChip.value.has(tok.token)) {
      const next = new Set(forcedChip.value);
      next.delete(tok.token);
      forcedChip.value = next;
    }
    if (!dissolvedChip.value.has(tok.token)) {
      const next = new Set(dissolvedChip.value);
      next.add(tok.token);
      dissolvedChip.value = next;
    }
    // the caret stays parked at the span's end; the plan watch performs
    // the reveal and restores the caret there
    caretAt.value = tok.start + tok.token.length;
    return true;
  }

  // absorbWrite: the shared tail of every text write (typing, paste, the
  // newline insert) - read the text, clear a dismissed palette, sync the
  // caret, drop a browser-emptied editor's stray <br> artifact (it would
  // rob :empty of the placeholder), emit the draft, size.
  function absorbWrite(el: HTMLElement): void {
    const text = draftTextOf(el);
    dismissed.value = false;
    const at = caretFromDom(el);
    if (at >= 0) caretAt.value = Math.min(at, text.length);
    if (text === "" && el.hasChildNodes()) el.replaceChildren();
    emit("update:draft", text);
    resize();
  }

  // insertAtCaret writes plain text into the editor at the caret and
  // runs the input path by hand (manual DOM writes fire no input event).
  // The contenteditable's own paste would insert markup and its own
  // newline would insert div nodes - neither is draft-readable; text
  // nodes + pre-wrap carry both, the way native typing does.
  function insertAtCaret(text: string): void {
    const el = ta.value;
    if (!el || text === "") return;
    const sel = window.getSelection();
    let range: Range | null = null;
    if (sel?.rangeCount) {
      const r = sel.getRangeAt(0);
      if (r.collapsed && el.contains(r.startContainer)) range = r;
    }
    if (!range) {
      // no caret in the editor: append at the end
      range = document.createRange();
      range.setStart(el, el.childNodes.length);
      range.collapse(true);
    }
    const node = document.createTextNode(text);
    range.insertNode(node);
    range.setStartAfter(node);
    range.collapse(true);
    sel?.removeAllRanges();
    sel?.addRange(range);
    absorbWrite(el);
  }

  // removeToken: the chip's x - the token dies out of the draft, the
  // caret parks at the cut
  function removeToken(t: RefToken) {
    const next = deleteTokenAt(props.draft, t);
    emit("update:draft", next.draft);
    nextTick(() => {
      const el = ta.value;
      if (el) {
        el.focus();
        placeCaretAt(el, next.caret);
        caretAt.value = next.caret;
      }
    });
  }

  // acceptItem: mention rows insert their completed token at the caret;
  // command rows complete the draft to the command (typed args ride
  // along) and SUBMIT - selecting a command runs it, no agent
  // round-trip ever (the palette's law: commands act locally).
  function acceptItem(item: PaletteItem) {
    if (!paletteOpen.value) return;
    // a SKILL row (t_7f09bd0c) is a reference pill now, not a
    // fire-and-forget dispatch: the completed /<name> stays in the draft
    // (typed args ride along), the pill forms - the send resolves the
    // skill (the host arms the content) AND posts the message, so the
    // pill lives in the transcript too
    if (
      !mention.value.active &&
      item.kind === "skill"
    ) {
      const space = props.draft.indexOf(" ");
      const rest = space === -1 ? "" : props.draft.slice(space);
      const text = item.name + rest;
      closePalette();
      // the fresh swallow (t_e67c372b): the completed token chips
      // immediately - the caret parks at the draft's end, `forced`
      // holds the chip for this span until a dissolve releases it
      const forced = new Set(forcedChip.value);
      forced.add(item.name);
      forcedChip.value = forced;
      caretAt.value = text.length;
      emit("update:draft", text);
      active.value = -1;
      nextTick(() => {
        const el = ta.value;
        if (el) {
          el.focus();
          placeCaretAt(el, text.length);
        }
      });
      return;
    }
    if (!mention.value.active && (item.kind === "shell" || item.kind === "extension")) {
      // preserve any args typed after the command word
      const space = props.draft.indexOf(" ");
      const rest = space === -1 ? "" : props.draft.slice(space);
      closePalette();
      emit("update:draft", item.name + rest);
      emit("send");
      active.value = -1;
      return;
    }
    // the insert (t_07aed3f9's replace law): a file or dir row's name is
    // the entry's own - the completed token carries the token's dir part
    // (t_7f09bd0c), so a drilled pick is the full relative path. A dir
    // row DRILLS: the token keeps its trailing slash, the list refetches
    // and the palette stays open.
    const isDir = item.kind === "dir";
    const dir = mentionDirOf(mention.value.token);
    const insertion = "@" + dir + item.name + (isDir ? "/" : "");
    const next = replaceMentionToken(props.draft, caretAt.value, mention.value.token, insertion);
    if (!isDir) {
      closePalette();
      // the fresh swallow (t_e67c372b): a completed file token chips
      // immediately (dir rows drill; they are never a pill)
      const forced = new Set(forcedChip.value);
      forced.add(insertion);
      forcedChip.value = forced;
    }
    caretAt.value = next.caret;
    emit("update:draft", next.draft);
    active.value = -1;
    nextTick(() => {
      const el = ta.value;
      if (el && next.caret >= 0) {
        el.focus();
        placeCaretAt(el, next.caret);
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
      // the completion parks the caret at the draft's end; a skill
      // completion fresh-swallows into a chip (t_e67c372b)
      caretAt.value = item.name.length;
      if (item.kind === "skill") {
        const forced = new Set(forcedChip.value);
        forced.add(item.name);
        forcedChip.value = forced;
      }
      closePalette();
      const el = ta.value;
      if (el) nextTick(() => placeCaretAt(el, item.name.length));
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
    // Backspace ON a chip dissolves it (t_e67c372b): no selection and
    // the caret immediately behind the token - the chip dies, the RAW
    // token text re-materializes in its place (a reveal, not a delete:
    // the draft string is unchanged, the token was always in it). While
    // the palette is open the Backspace edits the running query char by
    // char - untouched, this branch must not hijack the list. The NEXT
    // Backspace behind a revealed span is the browser's own single-char
    // delete; the chip's x is the outright remove.
    if (e.key === "Backspace" && !paletteOpen.value && dissolveBehindCaret()) {
      e.preventDefault();
      return;
    }
    if (e.key === "Enter") {
      // EDITOR (t_e67c372b): the editor claims every Enter - Shift+Enter
      // inserts the newline as a text node (the browser's contenteditable
      // newline would insert a div node the draft reader cannot read),
      // plain Enter (and Ctrl/Alt+Enter - only the shift modifier is
      // exempt) sends. Same law, new mechanics.
      e.preventDefault();
      if (e.shiftKey) {
        insertAtCaret("\n");
      } else {
        emit("send");
      }
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