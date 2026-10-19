<template>
  <header class="menubar">
    <span class="brand">Ωmega</span>
    <div
      class="view-switch"
      role="tablist"
      aria-label="main view">
      <button
        class="view-tab"
        :class="{ on: view === 'chat' }"
        role="tab"
        :aria-selected="view === 'chat'"
        @click="$emit('view', 'chat')">
        chat
      </button>
      <button
        class="view-tab"
        :class="{ on: view === 'board' }"
        role="tab"
        :aria-selected="view === 'board'"
        @click="$emit('view', 'board')">
        board
      </button>
    </div>
    <span class="menubar-spacer" />
    <!-- the sidebars are FIXED panes (card t_4ae27e0d): no desktop
         toggle exists. On mobile the dock panes turn into slide-in
         drawers below 768px - these buttons surface ONLY there, as the
         drawers' handles. -->
    <button
      class="icon-btn drawer-btn"
      aria-label="Open the sessions drawer"
      title="sessions"
      @click="$emit('toggle-sidebar')">
      <svg
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
          <path d="M9 3v18" />
        </g>
      </svg>
    </button>
    <button
      class="icon-btn drawer-btn"
      aria-label="Open the inspector drawer"
      title="inspector"
      @click="$emit('toggle-inspector')">
      <svg
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
          <path d="M15 3v18" />
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
  // desktop: the toggles are gone - the sidebars are fixed panes. The
  // drawer buttons surface on mobile only (the CSS hides them above
  // 768px); `view` drives the chat/board switch chips.
  defineProps<{
    view: "chat" | "board";
  }>();

  defineEmits<{
    (e: "toggle-sidebar"): void;
    (e: "toggle-inspector"): void;
    (e: "settings"): void;
    (e: "view", v: "chat" | "board"): void;
  }>();
</script>