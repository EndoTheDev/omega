<!--
  omega webui: THE reference pill.
  A pill is pure data (pills.ts RefToken); the icon is the iconset's (icons.ts), never
  an emoji or a library.
-->
<template>
  <button
    class="ref-pill"
    :class="pill.kind"
    type="button"
    :title="title"
    @click="emit('open')">
    <Icon
      :name="pill.kind === 'skill' ? 'zap' : pill.kind === 'dir' ? 'folder' : 'file-generic'"
      :size="12" />
    <span class="ref-pill-name">{{ pillName }}</span>
  </button>
</template>

<script setup lang="ts">
  import Icon from "../ui/Icon.vue";
  import { computed } from "vue";
  import type { RefToken } from "../../composables/pills";

  const props = defineProps<{ pill: RefToken }>();
  const emit = defineEmits<{ (e: "open"): void }>();

  const pillName = computed(() =>
    props.pill.kind === "skill" ? "/" + props.pill.name : props.pill.label,
  );
  const title = computed(() => {
    if (props.pill.kind === "skill") return "open SKILL.md";
    if (props.pill.kind === "dir") return props.pill.name + " - open the folder";
    return props.pill.name + " - open in the Files pane";
  });
</script>