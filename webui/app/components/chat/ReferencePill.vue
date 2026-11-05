<!-- omega webui: THE reference pill. One renderer for
     both pill surfaces: the sent message's inline pill and, since card
     t_e67c372b, the composer's inline chip - the editor wraps this
     component non-editable where the token span was (the pill REPLACES
     the text it represents). A pill is pure data (pills.ts RefToken);
     the icon is the iconset's (icons.ts), never an emoji or a library.
     Click opens the target - a file in the Files-pane viewer, a skill's
     SKILL.md: one renderer, skills are files. The component emits
     "open"; the surface decides how (both inject the app's viewer
     open). -->
<template>
  <button
    class="ref-pill"
    :class="pill.kind"
    type="button"
    :title="title"
    @click="emit('open')">
    <Icon
      :name="pill.kind === 'skill' ? 'zap' : 'file-generic'"
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

  // skill pills read "/name" (the invocation form); file pills read the
  // basename - the full relative path is the hover title
  const pillName = computed(() =>
    props.pill.kind === "skill" ? "/" + props.pill.name : props.pill.label,
  );
  const title = computed(() =>
    props.pill.kind === "skill" ?
      "open SKILL.md"
    : props.pill.name + " - open in the Files pane",
  );
</script>