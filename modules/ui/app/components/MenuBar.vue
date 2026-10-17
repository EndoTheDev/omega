<template>
  <header class="menubar">
    <span class="brand">Ωmega</span>
    <span class="menubar-spacer" />
    <button
      class="icon-btn"
      aria-label="Toggle left sidebar"
      :title="sidebarOpen ? 'Close left sidebar' : 'Open left sidebar'"
      @click="$emit('toggle-sidebar')">
      <svg
        v-if="sidebarOpen"
        xmlns="http://www.w3.org/2000/svg"
        width="16"
        height="16"
        viewBox="0 0 24 24">
        <g
          fill="none"
          stroke="currentColor"
          stroke-linecap="round"
          stroke-linejoin="round"
          stroke-width="2">
          <rect
            width="18"
            height="18"
            x="3"
            y="3"
            rx="2" />
          <path d="M9 3v18m7-6l-3-3l3-3" />
        </g>
      </svg>
      <svg
        v-else
        xmlns="http://www.w3.org/2000/svg"
        width="16"
        height="16"
        viewBox="0 0 24 24">
        <g
          fill="none"
          stroke="currentColor"
          stroke-linecap="round"
          stroke-linejoin="round"
          stroke-width="2">
          <rect
            width="18"
            height="18"
            x="3"
            y="3"
            rx="2" />
          <path d="M9 3v18m5-12l3 3l-3 3" />
        </g>
      </svg>
    </button>
    <button
      class="icon-btn"
      aria-label="Toggle right sidebar"
      :title="inspectorOpen ? 'Close right sidebar' : 'Open right sidebar'"
      @click="$emit('toggle-inspector')">
      <svg
        v-if="inspectorOpen"
        xmlns="http://www.w3.org/2000/svg"
        width="16"
        height="16"
        viewBox="0 0 24 24">
        <g
          fill="none"
          stroke="currentColor"
          stroke-linecap="round"
          stroke-linejoin="round"
          stroke-width="2">
          <rect
            width="18"
            height="18"
            x="3"
            y="3"
            rx="2" />
          <path d="M15 3v18M8 9l3 3l-3 3" />
        </g>
      </svg>
      <svg
        v-else
        xmlns="http://www.w3.org/2000/svg"
        width="16"
        height="16"
        viewBox="0 0 24 24">
        <g
          fill="none"
          stroke="currentColor"
          stroke-linecap="round"
          stroke-linejoin="round"
          stroke-width="2">
          <rect
            width="18"
            height="18"
            x="3"
            y="3"
            rx="2" />
          <path d="M15 3v18m-5-6l-3-3l3-3" />
        </g>
      </svg>
    </button>
    <button
      class="icon-btn"
      title="Settings"
      aria-label="Settings"
      @click="$emit('settings')">
      ⚙
    </button>
  </header>
</template>

<script setup lang="ts">
  defineProps<{
    sidebarOpen: boolean;
    inspectorOpen: boolean;
  }>();

  defineEmits<{
    (e: "toggle-sidebar"): void;
    (e: "toggle-inspector"): void;
    (e: "settings"): void;
  }>();
</script>
