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
        class="viewer-status">loading{{ viewer.kind === "skill" ? " skill" : " file" }}…</p>
      <template v-else>
        <p
          v-if="notice"
          class="viewer-status">{{ notice }}</p>
        <p
          v-if="skillDesc"
          class="viewer-skill-desc">{{ skillDesc }}</p>
        <!-- ONE body per content class (t_42c8aced): the sniffed mime
             decides pdf/image, an .md/.markdown file or the skill body
             renders markdown, a non-UTF-8 file keeps the honest
             no-preview state (name + size + download), everything else
             stays the line-numbered monospace body. -->
        <div
          v-if="view === 'image'"
          class="viewer-imgwrap">
          <img
            class="viewer-img"
            :src="rawSrc"
            :alt="viewer.name">
        </div>
        <!-- pdf rides the browser's native viewer on the raw route - no
             renderer dependency, the bytes are same-origin -->
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
        <!-- the markdown body: the CHAT renderer (same grammar, same
             sanitizer) + the codeblock decoration pass -->
        <div
          v-else-if="view === 'markdown'"
          ref="mdEl"
          class="viewer-md md"
          role="document"
          aria-label="markdown preview"
          v-html="renderedMd"></div>
        <div
          v-else
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
  // The content viewer (card t_36ceb99b): ONE tab shape for the two
  // content kinds. A FILE tab reads GET /files/read (the same surface the
  // retired inline preview fetched); a SKILL tab reads GET /skills/content
  // (the loader's own parse: frontmatter description + body). The body
  // switches on ONE view class per content type (t_42c8aced):
  // markdown -> the chat renderMarkdown grammar + codeblock decoration,
  // pdf -> the browser's native viewer on GET /files/raw,
  // image -> an <img> on the raw route (card t_08d822bb),
  // binary -> the honest no-preview state (name + size + download lane),
  // everything else -> line-numbered monospace text.
  // Fetch-on-mount: a persisted viewer tab opened later re-fetches -
  // content views refresh, nothing caches stale bytes.
  import { computed, inject, nextTick, onMounted, ref, watch } from "vue";
  import type { ViewerRef } from "../composables/dockables";
  import {
    FOCUSED_SESSION_KEY,
    fetchSkillContent,
  } from "../composables/viewer";
  import { decorateCodeblocks } from "../composables/codeblocks";
  import {
    bytesLabel,
    fetchFilesRead,
    fileViewKind,
    filesRawUrl,
    renderMarkdown,
    type FileViewKind,
  } from "../composables/omega";
  import Icon from "./ui/Icon.vue";
  import { useNarrow } from "~/composables/narrow";
  import { glyphKindFor, glyphIcon } from "../../../harness/files/ui/rowicon";

  const props = defineProps<{ viewer?: ViewerRef }>();

  // the focused session rides the SAME seam FilesTab reads (t_a5f9d185):
  // the app always provides; a null fallback keeps the mount harmless.
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
  // a refetch when the same tab instance is handed a DIFFERENT viewer
  // (the outlet reuses the component; only the remount fetches fresh)
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
    try {
      if (props.viewer.kind === "skill") {
        const data = await fetchSkillContent(props.viewer.name);
        content.value = data.content;
        truncated.value = data.truncated;
        skillDesc.value = data.description;
      } else {
        // t_a5f9d185: the read roots on the focused session's resolved
        // root - the SAME resolution the Files tab browsed under, so a
        // workspace-pinned tab's file parses instead of 404ing against
        // the cwd fallback.
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

  // ONE view class per content kind. A skill's body IS markdown (the
  // loader hands the parsed body); files decide by the sniffed mime and
  // the extension (fileViewKind, t_42c8aced).
  const view = computed<FileViewKind>(() => {
    if (!props.viewer) return "code";
    if (props.viewer.kind === "skill") return "markdown";
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

  // the markdown branch renders through the CHAT grammar (markdown-it,
  // html:false sanitizer), then the shared codeblock decoration wraps
  // fences with the header bar - the same pass ChatMessage runs, the
  // v-html replacement tears the wrapper down so decorate re-adds it.
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
        : glyphIcon(glyphKindFor(props.viewer.name)),
  );
  const kindLabel = computed(() => (props.viewer?.kind === "skill" ? "skill" : ""));
  const rawSrc = computed(() =>
    props.viewer && props.viewer.kind === "file"
      ? filesRawUrl(props.viewer.name, focusedSession?.value ?? undefined)
      : "",
  );
  // the no-preview state's affordance (t_42c8aced): the download lane
  // serves any mime as an attachment, and the leaf name lands on the
  // anchor so the browser's save dialog pre-fills it
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
  .viewer-body {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 6px 12px 12px;
    font-family: var(--mono);
    font-size: 11px;
    line-height: 1.5;
  }
  /* the image body (t_08d822bb): fits the pane (contain), scrolls when
     the file is larger than it */
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
  /* the pdf body (t_42c8aced): the browser's native viewer owns the
     frame; the pane bounds the chrome */
  .viewer-pdf {
    flex: 1;
    min-height: 0;
    width: 100%;
    border: none;
    background: var(--z1-raised);
  }
  /* the no-preview state (t_42c8aced): name + size + the download lane */
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
  /* the markdown body rides the bubble's .md grammar (main.css); this
     container only sets the scroll + padding frame */
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
