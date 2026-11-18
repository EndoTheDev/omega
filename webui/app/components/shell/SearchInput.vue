<template>
  <!--
    ONE search input, shared by the sidebar (session search) and the settings pane
    (filter settings).
  -->
  <span class="search-input">
    <input
      ref="inputEl"
      :value="modelValue"
      type="search"
      :placeholder="placeholder"
      :aria-label="placeholder"
      autocomplete="off"
      @input="emit('update:modelValue', ($event.target as HTMLInputElement).value)"
      @keydown.esc.prevent="clear()" />
    <button
      v-if="modelValue.length > 0"
      class="search-clear"
      type="button"
      title="Clear"
      aria-label="Clear search"
      @click="clear()">
      &#215;
    </button>
  </span>
</template>

<script setup lang="ts">
  import { ref } from "vue";

  // clear sends the focus back to the input.
  const inputEl = ref<HTMLInputElement | null>(null);

  const props = defineProps<{
    modelValue: string;
    placeholder: string;
  }>();

  const emit = defineEmits<{
    (e: "update:modelValue", value: string): void;
  }>();

  function clear() {
    emit("update:modelValue", "");
    inputEl.value?.focus();
  }
</script>