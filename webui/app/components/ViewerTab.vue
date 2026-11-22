<template>
  <div
    ref="rootEl"
    class="content-viewer"
    :class="{ narrow }">
    <p
      v-if="!viewer"
      class="viewer-status">
      no content
    </p>
    <template v-else>
      <header class="viewer-head">
        <Icon
          :name="headIcon"
          :size="14" />
        <span
          class="viewer-name"
          :title="viewer.name">{{ viewer.name }}</span>
        <span
          v-if="kindLabel"
          class="viewer-kind">{{ kindLabel }}</span>
      </header>
      <p
        v-if="error"
        class="viewer-status viewer-error" role="alert">{{ error }}</p>
      <p
        v-else-if="loading"
        class="viewer-status">loading{{ viewer.kind === "skill" ? " skill" : viewer.kind === "dir" ? " folder" : " file" }}…</p>
      <template v-else>
        <p
          v-if="notice"
          class="viewer-status">{{ notice }}</p>
        <p
          v-if="skillDesc"
          class="viewer-skill-desc">{{ skillDesc }}</p>
        <div
          v-if="isDir"
          class="viewer-dir"
          role="list"
          :aria-label="viewer.name">
          <p
            v-if="!dirEntries.length"
            class="viewer-status">empty folder</p>
          <button
            v-for="e in dirEntries"
            :key="e.name"
            class="viewer-dir-row"
            :class="{ 'is-dir': e.is_dir }"
            type="button"
            role="listitem"
            :title="dirChildPath(e)"
            @click="openEntry(e)">
            <Icon
              :name="e.is_dir ? 'folder' : glyphIcon(glyphKindFor(e.name))"
              :size="13" />
            <span class="viewer-dir-name"
              >{{ e.name
              }}<span
                v-if="e.is_dir"
                class="dir-mark">/</span
              ></span
            >
            <span
              v-if="!e.is_dir"
              class="viewer-dir-size">{{ e.size_bytes }}</span>
          </button>
        </div>
        <!-- ONE body per content class. -->
        <div
          v-if="view === 'image'"
          class="viewer-imgwrap">
          <img
            class="viewer-img"
            :src="rawSrc"
            :alt="viewer.name">
        </div>
        <iframe
          v-else-if="view === 'pdf'"
          class="viewer-pdf"
          :src="rawSrc"
          :title="viewer.name"></iframe>
        <div
          v-else-if="view === 'binary'"
          class="viewer-binstate">
          <p class="viewer-status">binary file - no preview</p>
          <p class="viewer-binmeta">
            <span
              v-if="sizeLabel"
              class="viewer-dl-size">{{ sizeLabel }} - </span>
            <a
              class="viewer-dl"
              :href="downloadUrl"
              :download="leafName">download</a>
          </p>
        </div>
        <!-- the markdown body: the CHAT renderer (same grammar, same sanitizer) -->
        <div
          v-else-if="view === 'markdown'"
          ref="mdEl"
          class="viewer-md md"
          role="document"
          aria-label="markdown preview"
          v-html="renderedMd"></div>
        <div
          v-else-if="!isDir"
          class="viewer-body"
          role="document"
          :aria-label="viewer.name">
          <div
            v-for="(line, i) in lines"
            :key="i"
            class="viewer-line">
            <span class="viewer-no">{{ i + 1 }}</span>
            <span class="viewer-tx">{{ line }}</span>
          </div>
        </div>
      </template>
    </template>
  </div>
</template>

<script setup lang="ts">
  // The content viewer: ONE tab shape for the two content kinds.
  import { computed, inject, nextTick, onMounted, ref, watch } from "vue";
  import type { ViewerRef } from "../composables/dockables";
  import {
    DOCK_VIEWER_OPEN_KEY,
    FOCUSED_SESSION_KEY,
    fetchSkillContent,
  } from "../composables/viewer";
  import { decorateCodeblocks } from "../composables/codeblocks";
  import {
    bytesLabel,
    fetchFilesBrowse,
    fetchFilesRead,
    fileViewKind,
    filesRawUrl,
    joinRelPath,
    renderMarkdown,
    sortFileEntries,
    type FileEntry,
    type FileViewKind,
  } from "../composables/omega";
  import Icon from "./ui/Icon.vue";
  import { useNarrow } from "~/composables/narrow";
  import { glyphKindFor, glyphIcon } from "../../../harness/files/ui/rowicon";

  const props = defineProps<{ viewer?: ViewerRef }>();

  // the focused session rides the SAME seam FilesTab reads.
  const focusedSession = inject(FOCUSED_SESSION_KEY, null);

  const rootEl = ref<HTMLElement | null>(null);
  const narrow = useNarrow(rootEl);

  const loading = ref(false);
  const error = ref("");
  const content = ref("");
  const truncated = ref(false);
  const binary = ref(false);
  const mime = ref("");
  const size = ref<number | undefined>(undefined);
  const skillDesc = ref("");
  // the FOLDER body's rows: one directory listing, the same /files/browse read.
  const dirEntries = ref<FileEntry[]>([]);
  const isDir = computed(() => props.viewer?.kind === "dir");

  // the viewer's own opener: a folder row drills to another folder tab.
  const openViewerRef = inject(DOCK_VIEWER_OPEN_KEY, null);
  function dirChildPath(e: FileEntry): string {
    return joinRelPath(props.viewer?.name ?? "", e.name);
  }
  function openEntry(e: FileEntry): void {
    openViewerRef?.({ kind: e.is_dir ? "dir" : "file", name: dirChildPath(e) });
  }

  // a refetch when the same tab instance is handed a DIFFERENT viewer.
  watch(
    () => (props.viewer ? `${props.viewer.kind}:${props.viewer.name}` : ""),
    () => void load(),
  );

  async function load(): Promise<void> {
    if (!props.viewer) return;
    loading.value = true;
    error.value = "";
    content.value = "";
    truncated.value = false;
    binary.value = false;
    mime.value = "";
    size.value = undefined;
    skillDesc.value = "";
    dirEntries.value = [];
    try {
      if (props.viewer.kind === "skill") {
        const data = await fetchSkillContent(props.viewer.name);
        content.value = data.content;
        truncated.value = data.truncated;
        skillDesc.value = data.description;
      } else if (props.viewer.kind === "dir") {
        const data = await fetchFilesBrowse(props.viewer.name, focusedSession?.value ?? undefined);
        dirEntries.value = sortFileEntries(data.entries ?? []);
      } else {
        const data = await fetchFilesRead(props.viewer.name, focusedSession?.value ?? undefined);
        content.value = data.content;
        truncated.value = data.truncated;
        binary.value = data.binary;
        mime.value = data.mime ?? "";
        size.value = data.size;
      }
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    } finally {
      loading.value = false;
    }
  }

  onMounted(() => void load());

  // ONE view class per content kind.
  const view = computed<FileViewKind>(() => {
    if (!props.viewer) return "code";
    if (props.viewer.kind === "skill") return "markdown";
    if (props.viewer.kind === "dir") return "code";
    return fileViewKind(props.viewer.name, mime.value, binary.value);
  });

  // a trailing newline is structural, not a line: drop the empty tail so
  // the gutter's last number is the file's last line (code view only -
  // binary/pdf/markdown never line-split)
  const lines = computed(() => {
    if (view.value !== "code") return [];
    if (!content.value) return [];
    const all = content.value.split("\n");
    if (all.length > 1 && all[all.length - 1] === "") all.pop();
    return all;
  });

  // the markdown branch renders through the CHAT grammar.
  const renderedMd = ref("");
  const mdEl = ref<HTMLElement | null>(null);
  const collapsed = new Set<number>();
  watch(
    [content, view],
    () => {
      renderedMd.value = view.value === "markdown" ? renderMarkdown(content.value) : "";
      if (view.value === "markdown") {
        void nextTick(() => decorateCodeblocks(mdEl.value, collapsed));
      }
    },
    { immediate: true },
  );

  const headIcon = computed(() =>
    !props.viewer
      ? "file-generic"
      : props.viewer.kind === "skill"
        ? "skills"
        : props.viewer.kind === "dir"
          ? "folder"
          : glyphIcon(glyphKindFor(props.viewer.name)),
  );
  const kindLabel = computed(() =>
    props.viewer?.kind === "skill" ? "skill" : props.viewer?.kind === "dir" ? "folder" : "",
  );
  const rawSrc = computed(() =>
    props.viewer && props.viewer.kind === "file"
      ? filesRawUrl(props.viewer.name, focusedSession?.value ?? undefined)
      : "",
  );
  const downloadUrl = computed(() =>
    props.viewer && props.viewer.kind === "file"
      ? filesRawUrl(props.viewer.name, focusedSession?.value ?? undefined, { download: true })
      : "",
  );
  const leafName = computed(() => {
    const name = props.viewer?.name ?? "";
    const slash = name.lastIndexOf("/");
    return slash >= 0 ? name.slice(slash + 1) : name;
  });
  const sizeLabel = computed(() => (view.value === "binary" ? bytesLabel(size.value) : ""));
  const notice = computed(() => {
    if (view.value === "binary") return ""; // the binary state announces itself below
    if (truncated.value) return "truncated at 64 KiB";
    return "";
  });
</script>

<style scoped>
  .content-viewer {
    display: flex;
    flex-direction: column;
    height: 100%;
    overflow: hidden;
    padding: 8px 0 0;
  }
  .viewer-head {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 2px 12px 8px;
    border-bottom: 1px solid color-mix(in srgb, currentcolor 10%, transparent);
  }
  .viewer-name {
    font-family: var(--mono);
    font-size: 12px;
    font-weight: 600;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .viewer-kind {
    font-size: 10px;
    opacity: 0.5;
    border: 1px solid color-mix(in srgb, currentcolor 25%, transparent);
    border-radius: var(--r-chip);
    padding: 0 5px;
  }
  .viewer-status {
    margin: 0;
    padding: 6px 12px;
    font-size: 11px;
    opacity: 0.6;
  }
  .viewer-error {
    color: var(--text-dim);
    opacity: 1;
  }
  .viewer-skill-desc {
    margin: 0;
    padding: 6px 12px 0;
    font-size: 11px;
    opacity: 0.65;
  }
  .viewer-dir {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 4px 6px 12px;
  }
  .viewer-dir-row {
    display: flex;
    align-items: center;
    gap: 6px;
    width: 100%;
    padding: 3px 6px;
    border: 0;
    border-radius: var(--r-chip);
    background: transparent;
    color: var(--text-dim);
    font-family: var(--mono);
    font-size: 11px;
    text-align: left;
    cursor: pointer;
  }
  .viewer-dir-row:hover {
    background: var(--z1-raised);
    color: var(--text);
  }
  .viewer-dir-name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .viewer-dir-size {
    flex-shrink: 0;
    opacity: 0.55;
  }
  .viewer-body {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 6px 12px 12px;
    font-family: var(--mono);
    font-size: 11px;
    line-height: 1.5;
  }
  /*
   * the image body: fits the pane (contain), scrolls when the file is larger than it.
   */
  .viewer-imgwrap {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 8px 12px 12px;
  }
  .viewer-img {
    display: block;
    max-width: 100%;
    object-fit: contain;
  }
  .viewer-pdf {
    flex: 1;
    min-height: 0;
    width: 100%;
    border: none;
    background: var(--z1-raised);
  }
  .viewer-binstate {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 8px 12px;
  }
  .viewer-binmeta {
    display: flex;
    align-items: center;
    gap: 2px;
    padding: 0 12px;
    font-size: 11px;
    opacity: 0.8;
  }
  .viewer-dl {
    color: var(--accent);
    text-decoration: none;
  }
  .viewer-dl:hover {
    text-decoration: underline;
  }
  /* the markdown body rides the bubble's.md grammar (main.css). */
  .viewer-md {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 6px 12px 12px;
    font-size: 12px;
  }
  .viewer-md :deep(a) {
    color: var(--accent);
  }
  .viewer-line {
    display: flex;
  }
  .viewer-no {
    flex: 0 0 4ch;
    text-align: right;
    padding-right: 14px;
    opacity: 0.35;
    user-select: none;
  }
  .viewer-tx {
    white-space: pre-wrap;
    word-break: break-word;
    min-width: 0;
    opacity: 0.9;
  }
</style>
