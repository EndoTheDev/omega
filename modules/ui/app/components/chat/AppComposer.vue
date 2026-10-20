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
        placeholder="message omega"
        rows="1"
        @input="
          $emit('update:draft', ($event.target as HTMLTextAreaElement).value);
          resize();
        "
        @keydown.enter.exact.prevent="$emit('send')"
        @paste="onPaste" />
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
      <ThinkingSelect
        :level="thinking"
        @update:level="$emit('update:thinking', $event)" />
      <!-- ONE morphing button (t_357cbe9c): while a run streams this is
           the stop control; idle, a typed draft or a pending attachment
           means send (disabled when there is nothing to send), and an
           exchange to replay with an empty composer means retry. Draft
           beats retry: typed input is a new message, never a replay.
           The separate paperclip + retry buttons died - the attach
           affordance lives in the composer row's "+" now. -->
      <button
        class="send"
        :class="{ cancel: face === 'stop', retry: face === 'retry' }"
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
  import { dataUrl, mediaTypeFromBase64, type OmegaImage } from "../../composables/omega";
  import { composerFace } from "../../composables/chat";

  const props = defineProps<{
    draft: string;
    model: string;
    models: string[];
    // THIS session's run is streaming: the send button is CANCEL now
    busy: boolean;
    // idle with a last user message in the transcript: retry shows
    canRetry: boolean;
    thinking: string;
    // pending image attachments - owned by the app: the transcript
    // bubble and the interrupted retry both need them
    attachments: OmegaImage[];
  }>();

  const emit = defineEmits<{
    (e: "update:draft", value: string): void;
    (e: "update:model", value: string): void;
    (e: "update:thinking", value: string): void;
    (e: "send"): void;
    (e: "cancel"): void;
    (e: "retry"): void;
    (e: "attach-files", images: OmegaImage[]): void;
    (e: "remove-attachment", index: number): void;
    (e: "attach-url", url: string): void;
  }>();

  // send needs text OR an attachment - image-only messages are valid
  const canSend = computed(() => !!props.draft.trim() || props.attachments.length > 0);

  // the morph (t_357cbe9c): busy -> stop, empty composer + exchange ->
  // retry, else send. retry-face when the transcript has an exchange to
  // replay and the composer carries nothing to send.
  const face = computed(() => composerFace(props.busy, props.canRetry, canSend.value));
  const faceLabel = computed(() =>
    face.value === "stop" ? "■ stop" : face.value === "retry" ? "↻ retry" : "send",
  );
  const faceTitle = computed(() =>
    face.value === "stop" ?
      "cancel the running response (what streamed stays)"
      : face.value === "retry" ?
        "re-run the last user message - the fresh response appends below it"
        : "send",
  );
  function onButton() {
    if (face.value === "stop") emit("cancel");
    else if (face.value === "retry") emit("retry");
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
</script>