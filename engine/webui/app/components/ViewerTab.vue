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
          v-if="!binary && skillDesc"
          class="viewer-skill-desc">{{ skillDesc }}</p>
        <div
          v-if="!binary"
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
  // (the loader's own parse: frontmatter description + body). Both render
  // line-numbered monospace text; truncated/binary rides the notice line.
  // Fetch-on-mount: a persisted viewer tab opened later re-fetches -
  // content views refresh, nothing caches stale bytes.
  import { computed, inject, onMounted, ref, watch } from "vue";
  import type { ViewerRef } from "../composables/dockables";
  import {
    FOCUSED_SESSION_KEY,
    fetchSkillContent,
  } from "../composables/viewer";
  import { fetchFilesRead } from "../composables/omega";
  import Icon from "./ui/Icon.vue";
  import { useNarrow } from "~/composables/narrow";
  import { glyphKindFor, glyphIcon } from "./files/rowicon";

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
      }
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    } finally {
      loading.value = false;
    }
  }

  onMounted(() => void load());

  // a trailing newline is structural, not a line: drop the empty tail so
  // the gutter's last number is the file's last line
  const lines = computed(() => {
    if (binary.value) return [];
    const all = content.value.split("\n");
    if (all.length > 1 && all[all.length - 1] === "") all.pop();
    return all;
  });

  const headIcon = computed(() =>
    !props.viewer
      ? "file-generic"
      : props.viewer.kind === "skill"
        ? "skills"
        : glyphIcon(glyphKindFor(props.viewer.name)),
  );
  const kindLabel = computed(() => (props.viewer?.kind === "skill" ? "skill" : ""));
  const notice = computed(() => {
    const parts: string[] = [];
    if (binary.value) parts.push("binary file - no preview");
    if (truncated.value) parts.push("truncated at 64 KiB");
    return parts.join(" - ");
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