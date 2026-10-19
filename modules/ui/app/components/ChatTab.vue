<template>
  <main class="chat pane">
    <!-- wrap owns the pill's absolute placement: the scroll container
         itself scrolls its content (an absolutely positioned child of
         it would scroll away with it) -->
    <div class="messages-wrap">
      <div
        class="messages"
        :class="{ stacked: appearance === 'stacked' }"
        ref="messagesEl">
        <p
          v-if="transcript.length === 0"
          class="empty">
          say something to
          <span class="brand">Ωmega</span>
        </p>
        <!-- ONE item renderer, two container layouts (card t_84e4653c):
             ChatItem is a fragment - it renders exactly the per-item DOM
             the old flat v-for did, so 'chat' stays pixel-identical.
             'stacked' wraps each turn block: a user message plus its
             full response subtree, separated by the block gap. -->
        <template v-if="appearance === 'stacked'">
          <div
            v-for="(block, bi) in blocks"
            :key="blockKey(block)"
            class="msg-block"
            :class="{ live: bi === blocks.length - 1 && busy }">
            <ChatItem
              v-for="item in block"
              :key="itemKey(item)"
              :item="item"
              :show-thinking="showThinking"
              @retry="onRetry" />
          </div>
        </template>
        <template v-else>
          <ChatItem
            v-for="item in transcript"
            :key="itemKey(item)"
            :item="item"
            :show-thinking="showThinking"
            @retry="onRetry" />
        </template>
        <div
          v-if="statusLine"
          class="status-line">
          {{ statusLine }}…
        </div>
      </div>
      <button
        v-if="jumpPill"
        class="jump-live"
        @click="jumpToLive">
        ⌄ live
      </button>
    </div>

    <AppComposer
      v-model:draft="draft"
      :model="model"
      :models="models"
      :busy="busy"
      :can-retry="canRetry"
      :thinking="thinking"
      :attachments="state.attachments"
      @update:model="emit('update:model', $event)"
      @update:thinking="emit('update:thinking', $event)"
      @send="onSend"
      @cancel="host.cancel(stateKey)"
      @retry="onRetry"
      @attach-files="(imgs: never[]) => host.attachFiles(stateKey, imgs)"
      @remove-attachment="(i: number) => host.removeAttachment(stateKey, i)"
      @attach-url="(u: string) => host.attachUrl(stateKey, u)" />
  </main>
</template>

<script setup lang="ts">
  // ONE chat tab (card t_4f0e88f2): the chat component is dockable now.
  // Its identity is the tab it renders (pane + tab object) and its state
  // lives in the app-level host keyed by the tab's instance key - so a
  // stream keeps feeding an inactive/unmounted tab and two tabs on one
  // session each hold their own transcript while the busy gate stays
  // per session (the runHub run slot).
  import { computed, inject, onBeforeUnmount, onMounted, ref, watch } from "vue";
  import {
    CHAT_HOST_KEY,
    type ChatState,
    type TranscriptItem,
  } from "../composables/chat";
  import {
    createItemKeySource,
    isAtBottom,
    pillVisible,
    lastUserText,
    splitBlocks,
    thinkingLevelWire,
    type Appearance,
  } from "../composables/omega";
  import type { DockTab } from "../composables/dock";
  import AppComposer from "./chat/AppComposer.vue";
  import ChatItem from "./ChatItem.vue";

  const props = defineProps<{
    /** the tab this instance renders (its `key` is the state key) */
    tab: DockTab;
    /** show thinking blocks (app setting) */
    showThinking: boolean;
    model: string;
    models: string[];
    /** reasoning-effort chip selection ("default" = no field sent) */
    thinking: string;
    /** chat appearance: chronolog | stacked blocks (card t_84e4653c) */
    appearance: Appearance;
  }>();

  const emit = defineEmits<{
    (e: "update:model", value: string): void;
    (e: "update:thinking", value: string): void;
  }>();

  const host = inject(CHAT_HOST_KEY)!;

  // the tab key: minted at creation, present on every persisted chat tab
  // (dock boot-time normalization) - the host's state key
  const stateKey = props.tab.key ?? "";
  // the tab owns the binding: unsaved (no sessionRef) -> composer-first
  const sessionId = computed(() => props.tab.sessionRef ?? null);

  // ensure BEFORE the computeds read the map: setup runs pre-render
  host.ensureState(stateKey, sessionId.value);
  // the reactive read through the wrapped map is the view
  const state = computed<ChatState | undefined>(() =>
    (host.states.get(stateKey) as ChatState | undefined),
  );

  const transcript = computed(() => state.value?.transcript ?? []);
  const statusLine = computed(() => state.value?.statusLine ?? "");
  const itemKey = createItemKeySource();
  // stacked layout (card t_84e4653c): the transcript cut at user
  // messages - user message + response subtree = one visual block.
  // Blocks recompute from the SAME item objects, so the inner keys stay
  // stable through streaming appends.
  const blocks = computed(() => splitBlocks(transcript.value));
  function blockKey(block: TranscriptItem[]): number {
    const first = block[0];
    return first ? itemKey(first) : -1;
  }
  // draft lives per state (survives tab switches)
  const draft = computed({
    get: () => state.value?.draft ?? "",
    set: (v: string) => {
      if (state.value) state.value.draft = v;
    },
  });

  // composer busy = the PER-SESSION run slot (either this tab's stream,
  // another tab's on the same session, or a detached run another window
  // started) - the runHub truth, not a per-tab one
  const busy = computed(() => {
    const id = sessionId.value;
    if (id) return host.busyAll.value.has(id);
    // unsaved tab: only its own create window gates it
    return host.busyKeys.value.has(stateKey);
  });
  const canRetry = computed(
    () => !busy.value && lastUserText(transcript.value) !== null,
  );

  // --- scroll pinning + jump-to-live pill: PER INSTANCE now - multiple
  // panes may render the same component, each owns its scrollbar ---
  const messagesEl = ref<HTMLElement | null>(null);
  const pinned = ref(true);
  const jumpPill = ref(false);

  function nearBottom(): boolean {
    const el = messagesEl.value;
    if (!el) return true;
    return isAtBottom(el.scrollTop, el.clientHeight, el.scrollHeight);
  }

  function onMessagesScroll() {
    pinned.value = nearBottom();
    const el = messagesEl.value;
    if (!el) return;
    jumpPill.value = pillVisible(el.scrollTop, el.clientHeight, el.scrollHeight);
  }

  // stacked live turn (Endo 2026-10-20): the USER MESSAGE pins to the
  // top of the pane while its answer streams below - so the follow
  // logic targets the live block's top, not the content tail. Pushing
  // the block top to the pane top on send is what "pushes everything
  // else up"; the sticky CSS holds it there while the run is busy.
  function liveBlockGap(el: HTMLElement): number | null {
    if (props.appearance !== "stacked") return null;
    const live = el.querySelector<HTMLElement>(".msg-block.live");
    if (!live) return null;
    return live.getBoundingClientRect().top - el.getBoundingClientRect().top;
  }

  function scrollDown(force = false) {
    if (force) pinned.value = true;
    requestAnimationFrame(() => {
      const el = messagesEl.value;
      if (!el) return;
      if (!force && !pinned.value) return;
      const gap = liveBlockGap(el);
      // pinned-then-follow (Endo 2026-10-20): the FIRST appearance of
      // the live block (gap > 1, its top still below the viewport top)
      // scrolls it to the pane top - older content rides up; once the
      // sticky CSS holds it there (gap 0), the follow returns to the
      // streaming tail so the newest agent output shows underneath
      if (gap !== null && gap > 1) {
        el.scrollTop += gap;
        return;
      }
      el.scrollTop = el.scrollHeight;
    });
  }

  function jumpToLive() {
    jumpPill.value = false;
    scrollDown(true);
  }

  // --- send / retry ---
  function onSend() {
    const text = draft.value.trim();
    if (!text) return;
    draft.value = "";
    host.submit(stateKey, text, props.model, thinkingLevelWire(props.thinking));
  }
  function onRetry() {
    host.retry(stateKey, props.model, thinkingLevelWire(props.thinking));
  }

  // --- mount: the tab's restore + live-attach entry ---
  onMounted(async () => {
    messagesEl.value?.addEventListener("scroll", onMessagesScroll);
    // the driver-notified follower: stream writes reach a mounted tab
    state.value!.notify = scrollDown;
    if (sessionId.value) host.load(stateKey, sessionId.value);
    // unsaved: composer-first, nothing to restore
  });

  watch(sessionId, (id) => {
    if (!id) return;
    // the tab's binding moved (unsaved chat's create path, or a session
    // a shared tab reopened): bind + load when this state is virgin
    if (!host.states.get(stateKey)?.loaded) host.load(stateKey, id);
  });

  // a run starting for the session AFTER this tab mounted (another tab
  // on the same session, or a detached run another window began): the
  // ruling keeps both tabs subscribed - attach this tab's own copy.
  // The mount's load() handles the already-live case; this catches the
  // later-arriving truth through refreshSessions' server-active feed.
  watch(
    () => sessionId.value ? host.busyAll.value.has(sessionId.value) : false,
    (live) => {
      if (!live) return;
      const st = host.states.get(stateKey);
      // a run this tab never subscribed to and has no driver for: a
      // virgin state loads (its restore attaches via the detail's live
      // flag); a loaded one attaches its own copy - both tabs subscribe
      if (!st || st.attached || host.busyKeys.value.has(stateKey)) return;
      if (!st.loaded) {
        host.load(stateKey, sessionId.value!);
        return;
      }
      host.attach(stateKey);
    },
  );

  onBeforeUnmount(() => {
    messagesEl.value?.removeEventListener("scroll", onMessagesScroll);
    if (state.value) state.value.notify = undefined;
  });
</script>