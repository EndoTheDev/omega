<template>
  <div :class="['msg', role]">
    <div class="bubble">
      {{ content
      }}<span
        v-if="streaming"
        class="cursor" />
    </div>
  </div>
</template>

<script setup lang="ts">
  defineProps<{
    role: "user" | "assistant";
    content: string;
    streaming?: boolean;
  }>();
</script>
