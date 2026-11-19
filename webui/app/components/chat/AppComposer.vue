<template>
  <div
    class="composer"
    :class="{
      expanded: expanded,
      'stage-icon': stage === 'icon',
      'stage-stacked': stage === 'stacked',
    }"
    ref="root"
    @click="onCanvasClick">
    <div
      v-if="attachments.length || files.length"
      class="attach-strip">
      <div
        v-for="(img, i) in attachments"
        :key="'img-' + i"
        class="attach-thumb"
        :title="'attached image ' + (i + 1)">
        <img :src="dataUrl(img)" alt="" />
        <button
          class="attach-remove"
          type="button"
          title="remove attachment"
          @click="$emit('remove-attachment', i)">&times;</button>
      </div>
      <!--
        non-image references: the bytes never reach the browser, so there is nothing.
      -->
      <div
        v-for="(f, i) in files"
        :key="'file-' + i"
        class="attach-file"
        :title="f.name + ' - ' + attachmentSizeLabel(f.size)">
        <Icon
          :name="attachmentIcon(f.kind)"
          :size="14" />
        <span class="attach-file-name">{{ f.name }}</span>
        <span class="attach-file-size">{{ attachmentSizeLabel(f.size) }}</span>
        <button
          class="attach-remove attach-file-remove"
          type="button"
          :title="'remove ' + f.name"
          :aria-label="'remove ' + f.name"
          @click="$emit('remove-file-attachment', i)">&times;</button>
      </div>
    </div>
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
    <!--
      the reference CHIPS: the draft's /skill and @file / @folder tokens render.
    -->
    <div class="composer-row">
      <button
        v-if="!expanded"
        class="plus-btn"
        type="button"
        title="attach files"
        aria-label="attach files"
        @click="fileInput?.click()">+</button>
      <!--
        THE EDITOR: a contenteditable div, not a textarea - chips must render inline.
        The div's children are NEVER Vue-patched (the user's typed DOM is the live
        truth; the template deliberately leaves the element empty) - the sync module
        below rebuilds children from the draft only when the plan and the DOM disagree,
        so native typing never churns the caret.
      -->
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
    <!--
      the fullscreen bottom control row.
      v-if keeps the two + seats exclusive: compact keeps the row seat, fullscreen the
      toolbar seat - the compact DOM never changes.
    -->
    <div
      class="toolbar"
      :class="{ 'composer-bottom': expanded }">
      <button
        v-if="expanded"
        class="plus-btn"
        type="button"
        title="attach files"
        aria-label="attach files"
        @click="fileInput?.click()">+</button>
      <!--
        control row order: the model name, then the effort pill.
        model + effort are ONE wrap unit: the toolbar wraps wherever it runs out of
        room, but the model chip must never strand the effort chip on a second row - as
        one flex item (.model-group, nowrap) the pair crosses any wrap boundary
        together or not at all.
      -->
      <div class="model-group">
        <ModelSelect
          :model="model"
          :models="models"
          :groups="groups"
          @update:model="$emit('update:model', $event)"
          @refresh-models="$emit('refresh-models')" />
        <!-- the effort chip rides the shared BandSelect (v0.14.1 review lens. -->
        <BandSelect
          class="thinking-select"
          label="Reasoning effort"
          drop="up"
          :options="thinkingOptions"
          :model-value="thinking"
          @update:model-value="$emit('update:thinking', $event)" />
      </div>
      <!--
        ONE morphing button: while a run streams this is the stop control.
        The replay affordance renders in the newest response's action row
        (ChatMessage's msg-retry chip), never here.
      -->
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
  import BandSelect from "../ui/BandSelect.vue";
  import Icon from "../ui/Icon.vue";
  import ReferencePill from "./ReferencePill.vue";
  import { computed, getCurrentInstance, h, render } from "vue";
  import {
    dataUrl,
    mediaTypeFromBase64,
    fetchFilesBrowse,
    fetchSessions,
    fetchWorkspaceFiles,
    MAX_CHAT_FILE_BYTES,
    THINKING_UI_LEVELS,
    type FileEntry,
    type OmegaAttachment,
    type OmegaImage,
  } from "../../composables/omega";
  import {
    attachmentIcon,
    attachmentSizeLabel,
    isImageFile,
  } from "../../composables/attachments";
  import { composerFace } from "../../composables/chat";
  import { useComposerLadder } from "../../composables/composerladder";
  import {
    paletteCommands,
    filterCommands,
    mentionQueryAt,
    mentionPrefixOf,
    replaceMentionToken,
    slashTokenAt,
    replaceSlashToken,
    clampIndex,
    fetchPaletteCommands,
    fetchPaletteSkills,
  } from "../../composables/commands";
  import type { PaletteItem } from "../../composables/commands";
  // the pill token model: the strip scan, the Backspace token surgery, and the @ path.
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
  // The composer law lives in.agents/DESIGN.md.
  const props = defineProps<{
    draft: string;
    model: string;
    models: string[];
    groups?: { name: string; models: string[]; capabilities?: Record<string, string[]>; error?: string }[];
    busy: boolean;
    thinking: string;
    attachments: OmegaImage[];
    files: OmegaAttachment[];
    forkAnchor?: string | null;
  }>();

  const emit = defineEmits<{
    (e: "update:draft", value: string): void;
    (e: "update:model", value: string): void;
    (e: "update:thinking", value: string): void;
    (e: "send"): void;
    (e: "cancel"): void;
    (e: "refresh-models"): void;
    (e: "attach-files", images: OmegaImage[]): void;
    // the picked NON-image files, pre-upload.
    (e: "attach-file-refs", files: File[]): void;
    (e: "remove-attachment", index: number): void;
    (e: "remove-file-attachment", index: number): void;
    (e: "attach-url", url: string): void;
    (e: "clear-fork"): void;
  }>();

  // send needs text OR an attachment - image-only and file-only messages are valid
  const canSend = computed(
    () => !!props.draft.trim() || props.attachments.length > 0 || props.files.length > 0,
  );

  const thinkingOptions = computed(() =>
    THINKING_UI_LEVELS.map((level) => ({ value: level, label: level })),
  );

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

  // grow with content: 1 row at rest, up to 4 rows (COMPOSER_MAX), then the textarea.
  const COMPOSER_MAX = 84;

  const ta = ref<HTMLElement | null>(null);
  const fileInput = ref<HTMLInputElement | null>(null);
  const expanded = ref(false);
  const overflowing = ref(false);

  const root = ref<HTMLElement | null>(null);
  const ladderTouch = computed(
    () => `${props.model}|${props.thinking}|${props.busy}|${expanded.value}`,
  );
  const stage = useComposerLadder(root, ladderTouch);

  function resize() {
    const el = ta.value;
    if (!el) return;
    el.style.height = "auto";
    const full = el.scrollHeight;
    overflowing.value = full > COMPOSER_MAX + 1;
    if (expanded.value) {
      // expanded: the composer fills the chat pane (CSS flex).
      el.style.height = "";
      return;
    }
    el.style.height = `${Math.min(full, COMPOSER_MAX)}px`;
  }

  function toggleExpand() {
    expanded.value = !expanded.value;
    // the height math reads state-dependent layout: the class must land in the DOM.
    nextTick(() => {
      resize();
      if (expanded.value) ta.value?.focus();
    });
  }

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


  // fileToImage reads one File into the ai.ImageContent shape. Returns {img: null,
  // reason} for unsniffable content - handleFiles reports the first reason instead of
  // silently dropping. The size cap is the caller's gate.
  async function fileToImage(
    file: File,
  ): Promise<{ img: OmegaImage | null; reason?: string }> {
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

  // handleFiles routes one pick into the two lanes. ONE cap for both -
  // MAX_CHAT_FILE_BYTES, the server's own number - so an oversize pick never leaves the
  // browser.
  async function handleFiles(files: FileList | File[]) {
    const images: OmegaImage[] = [];
    const refs: File[] = [];
    let firstReason = "";
    for (const file of Array.from(files)) {
      if (file.size > MAX_CHAT_FILE_BYTES) {
        if (!firstReason) {
          firstReason = `${file.name || "file"} too large: ${(file.size / 1048576).toFixed(1)} MB (max ${MAX_CHAT_FILE_BYTES / 1048576} MB)`;
        }
        continue;
      }
      if (!isImageFile(file)) {
        refs.push(file);
        continue;
      }
      const { img, reason } = await fileToImage(file);
      if (img) images.push(img);
      else if (!firstReason && reason) firstReason = reason;
    }
    if (images.length) emit("attach-files", images);
    if (refs.length) emit("attach-file-refs", refs);
    if (firstReason) window.alert(firstReason);
  }

  function onFilesPicked(e: Event) {
    const files = (e.target as HTMLInputElement).files;
    if (files?.length) void handleFiles(files);
    if (fileInput.value) fileInput.value.value = "";
  }

  function onPaste(e: ClipboardEvent) {
    const files = Array.from(e.clipboardData?.files ?? []);
    if (files.length) {
      void handleFiles(files);
      return;
    }
    // A lone plain-text URL offers attach-as-image - the SERVER fetches the bytes
    // (never cross-origin from the browser).
    const text = (e.clipboardData?.getData("text/plain") ?? "").trim();
    if (/^https?:\/\/\S+$/i.test(text)) {
      emit("attach-url", text);
    }
  }

  // the / command palette and @ mention picker --- Derived from draft + caret. NOTE:
  // the mode is DERIVED (mention.value.active), never stored in an imperative ref
  // flipped from onInput - that flip raced the draft prop update (the handler sees the
  // PREVIOUS emit's draft) and the bare-@ list rendered empty one beat too late.

  const active = ref(-1);
  const dismissed = ref(false);
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

  // the @ file rows: ONE directory's listing at a time.
  const fileDir = ref<string | null>(null);
  const fileLoading = ref(false);
  async function loadFileRows(dir: string) {
    const clean = dir.replace(/\/+$/, "");
    if (fileDir.value === clean || fileLoading.value) return;
    fileDir.value = clean;
    fileLoading.value = true;
    try {
      const payload = props.workspace ?
        await fetchWorkspaceFiles(props.workspace + (clean ? "/" + clean : ""))
      : await fetchFilesBrowse(clean);
      fileRows.value = (payload.entries ?? []) as FileEntry[];
    } catch {
      fileRows.value = [];
    } finally {
      fileLoading.value = false;
    }
  }

  const mention = computed(() => mentionQueryAt(props.draft, caretAt.value));

  // the caret's "/" token: the slash surface is TOKEN-based like the mention surface.
  const slash = computed(() => slashTokenAt(props.draft, caretAt.value));

  const paletteOpen = computed(() => {
    if (dismissed.value) return false;
    if (slash.value.active) return true;
    return mention.value.active;
  });

  // allItems branches on the DERIVED mode (see the note above).
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
      const prefixRows: PaletteItem[] = [
        { name: "session:", description: "inject a session's history", kind: "session" },
        { name: "skill:", description: "inject a skill's content", kind: "skill" },
      ];
      const rows: PaletteItem[] = [...prefixRows, ...mentionRows(fileRows.value)];
      return filterMentionRows(tok, rows);
    }
    if (!slash.value.active) return [];
    const query = slash.value.token;
    return filterCommands(query, paletteCommands(extCmds.value, skillRows.value));
  });

  // the dropdown scrolls in CSS (.palette-list max-height + overflow-y).
  const PALETTE_MAX = 100;
  const paletteItems = computed(() => allItems.value.slice(0, PALETTE_MAX));

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

  // the lazy loads ride mode transitions reactively (same race note as above).
  const mentionActive = computed(() => mention.value.active);
  const slashSurface = computed(() => !mentionActive.value && slash.value.active);
  watch(mentionActive, (on) => {
    if (on) {
      void loadMentionRows();
      void loadPaletteData();
    }
  });
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
    if (scanRefTokens(props.draft, CHIP_SCAN).some((t) => t.kind === "skill")) {
      void loadPaletteData();
    }
    syncEditor();
  });

  function closePalette() {
    dismissed.value = true;
  }

  // the chip editor: a pill REPLACES the text it -- -- represents, inline in the flow
  // -- The editor is a contenteditable div (a textarea cannot render markup in place).
  // Its children are owned by this module, never by Vue's patcher: native typing
  // mutates the DOM freely, and the sync rewrites the children ONLY when the plan (the
  // chip law over the draft) and the DOM disagree - a rebuild restores the caret at the
  // same draft-space index, so the cursor never visibly jumps.

  // chip eligibility: a file or dir token stands as-is; a skill token chips as soon as
  // its name is a loaded skill's (a shell verb like "/theme" is a command, never a pill
  // - the slash surface has already loaded the skills snapshot for any "/"-draft) the
  // editor's chip scan: a skill token chips at ANY word boundary here, while the
  // transcript keeps the leading-invocation law - one token model, the composer passes
  // the one surface switch (pills.ts ScanOpts)
  const CHIP_SCAN = { inlineSkills: true } as const;
  const skillNames = computed(() => new Set(skillRows.value.map((s) => s.name)));
  const isPill = (t: RefToken): boolean =>
    t.kind === "file" || t.kind === "dir" || skillNames.value.has(t.name);

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
      CHIP_SCAN,
    ),
  );

  // caret arithmetic (draft space <-> editor DOM) --- A chip contributes its token's
  // full length to draft space but offers exactly two caret seats: before it (the
  // token's start index) and after it (the span's end index). A token's interior
  // indexes exist in draft space but never in the chip'd DOM - a chip there means the
  // span is raw (dissolved or being typed), an ordinary text node the caret can sit
  // inside.

  // draftTextOf reads the editor's text. Stray browser elements walk into (they carry
  // no draft text that matters; the input paths never create them).
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

  // capTail: the trailing-newline sentinel. One sentinel <br> after the draft's final
  // newline forces the line box AND moves the caret's paint seat onto the new line (a
  // collapsed caret before a final <br> lands on the empty line - the classic
  // trailing-br device, draft-safe here: a non-chip element contributes nothing to
  // draftTextOf, caretFromDom or placeCaretAt, so the draft string never learns of the
  // sentinel). <text> is the caller's fresh draft read (absorbWrite and syncEditor walk
  // once).
  function capTail(el: HTMLElement, text: string): void {
    const brs: Node[] = [];
    for (
      let n = el.lastChild;
      n instanceof Element && n.tagName === "BR";
      n = n.previousSibling
    ) {
      brs.push(n);
    }
    const want = text.endsWith("\n") ? 1 : 0;
    for (let i = want; i < brs.length; i++) el.removeChild(brs[i]);
    if (want === 1 && brs.length === 0) el.appendChild(document.createElement("br"));
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
      if (anchor === el) {
        if (range.startOffset === i) return idx;
      } else if (c === anchor) {
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

  // placeCaretAt puts the caret at a draft-space index in the DOM.
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

  // the chip DOM + the sync

  const appContext = getCurrentInstance()?.appContext ?? null;

  // buildChip mounts ONE renderer's instance into a non-editable span.
  function buildChip(t: RefToken): HTMLElement {
    const wrap = document.createElement("span");
    wrap.className = "ref-chip";
    wrap.contentEditable = "false";
    wrap.dataset.refchip = "";
    wrap.dataset.token = t.token;
    wrap.addEventListener("mousedown", (e) => e.preventDefault());
    wrap.addEventListener("dragstart", (e) => e.preventDefault());
    // chip click: a file opens in the Files pane, a skill opens its SKILL.md,.
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

  // sync brings the editor's DOM to the plan and rebuilds ONLY on disagreement (the
  // DOM's text out of parity with the draft, or the chip sequence drifted) - typing and
  // caret moves take the no-op path and never churn the caret.
  function syncEditor(): void {
    const el = ta.value;
    if (!el) return;
    const live = new Set(scanRefTokens(props.draft, CHIP_SCAN).map((t) => t.token));
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
      capTail(el, domText);
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
    capTail(el, props.draft);
    resize();
  }

  watch(editorPlan, () => nextTick(syncEditor));

  // dissolveBehindCaret runs the Backspace law.
  function dissolveBehindCaret(): boolean {
    const el = ta.value;
    if (!el) return false;
    const caret = caretFromDom(el);
    if (caret === -1) return false;
    const tok = tokenBeforeCaret(props.draft, caret, CHIP_SCAN);
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
    caretAt.value = tok.start + tok.token.length;
    return true;
  }

  // absorbWrite: the shared tail of every text write.
  function absorbWrite(el: HTMLElement): void {
    const text = draftTextOf(el);
    dismissed.value = false;
    const at = caretFromDom(el);
    if (at >= 0) caretAt.value = Math.min(at, text.length);
    if (text === "" && el.hasChildNodes()) el.replaceChildren();
    emit("update:draft", text);
    capTail(el, text); // the newline sentinel rides every write's tail
    resize();
  }

  // insertAtCaret writes plain text into the editor at the caret and runs the input.
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

  // removeToken: the chip's x.
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

  // acceptItem: mention rows insert their completed token at the caret.
  function acceptItem(item: PaletteItem) {
    if (!paletteOpen.value) return;
    // a SKILL row is a reference pill now, not a fire-and-forget dispatch.
    if (
      !mention.value.active &&
      item.kind === "skill"
    ) {
      // token surgery: replace ONLY the "/" word under the caret.
      const next = replaceSlashToken(props.draft, caretAt.value, slash.value.token, item.name);
      closePalette();
      const forced = new Set(forcedChip.value);
      forced.add("/" + item.name);
      forcedChip.value = forced;
      caretAt.value = next.caret;
      emit("update:draft", next.draft);
      active.value = -1;
      nextTick(() => {
        const el = ta.value;
        if (el) {
          el.focus();
          placeCaretAt(el, next.caret);
        }
      });
      return;
    }
    if (!mention.value.active && (item.kind === "shell" || item.kind === "extension")) {
      const next = replaceSlashToken(props.draft, caretAt.value, slash.value.token, item.name);
      closePalette();
      emit("update:draft", next.draft);
      emit("send");
      active.value = -1;
      return;
    }
    const isDir = item.kind === "dir";
    const dir = mentionDirOf(mention.value.token);
    const insertion = "@" + dir + item.name + (isDir ? "/" : "");
    const next = replaceMentionToken(props.draft, caretAt.value, mention.value.token, insertion);
    closePalette();
    const forced = new Set(forcedChip.value);
    forced.add(insertion);
    forcedChip.value = forced;
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

  // Tab completes without submitting (TUI acceptMatch parity).
  function acceptActive(submit: boolean) {
    const item = paletteItems.value[active.value];
    if (!item) return closePalette();
    if (submit) return acceptItem(item);
    if (!mention.value.active) {
      const next = replaceSlashToken(props.draft, caretAt.value, slash.value.token, item.name);
      emit("update:draft", next.draft);
      caretAt.value = next.caret;
      if (item.kind === "skill") {
        const forced = new Set(forcedChip.value);
        forced.add("/" + item.name);
        forcedChip.value = forced;
      }
      closePalette();
      const el = ta.value;
      if (el) nextTick(() => placeCaretAt(el, next.caret));
      return;
    }
    // a mention Tab completes too (TUI acceptMatch parity).
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
        e.preventDefault();
        acceptActive(true);
        return;
      }
    }
    // Backspace ON a chip dissolves it. While the palette is open the Backspace edits
    // the running query char by char - untouched, this branch must not hijack the list.
    if (e.key === "Backspace" && !paletteOpen.value && dissolveBehindCaret()) {
      e.preventDefault();
      return;
    }
    if (e.key === "Enter") {
      // EDITOR: the editor claims every Enter - Shift+Enter inserts the newline as a
      // text node (the browser's contenteditable newline would insert a div node the
      // draft reader cannot read), plain Enter (and Ctrl/Alt+Enter - only the shift
      // modifier is exempt) sends.
      e.preventDefault();
      if (e.shiftKey) {
        insertAtCaret("\n");
      } else {
        emit("send");
      }
    }
  }

  // composer auto-focus: the ChatTab parent delivers focus-on-select.
  defineExpose({
    focus: () => {
      ta.value?.focus();
    },
  });
</script>

<style scoped>
  /*
   * the fullscreen OVERLAY mount.
   * Out of flow now:.messages keeps the pane's whole height underneath and the card
   * floats over it, so collapsing needs no refetch and no scroll repair (the scrollport
   * is never resized).
   */
  .composer.expanded {
    position: absolute;
    inset: 0;
    z-index: 20;
  }
</style>