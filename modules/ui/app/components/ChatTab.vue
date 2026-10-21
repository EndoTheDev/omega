<template>
  <main class="chat pane">
    <!-- wrap owns the pill's absolute placement: the scroll container
         itself scrolls its content (an absolutely positioned child of
         it would scroll away with it) -->
    <div class="messages-wrap">
      <!-- live-run branch (card t_c40164cf): while a stacked run streams
           the pane stops being one scrolling list and becomes TWO regions
           - the last user message pinned as the header, the run's items
           scrolling in their own body underneath, auto-following the tail.
           Older history rides out of view for the run's duration (a layout
           state, not a scroll position); agent_end unmounts the branch and
           the settled column returns with the run's block in flow. -->
      <template v-if="liveLayout">
        <div class="live-pin">
          <ChatItem
            :key="itemKey(liveRun.user!)"
            :item="liveRun.user!"
            :show-thinking="showThinking"
            @select="onSelect"
            @fork="onFork"
            @retry="onRetry" />
        </div>
        <div
          class="messages live-body stacked"
          ref="liveBodyEl"
          @scroll="onScroll">
          <ChatItem
            v-for="item in liveRun.body"
            :key="itemKey(item)"
            :item="item"
            :variant="variantOfItem(item)"
            :show-thinking="showThinking"
            @select="onSelect"
            @fork="onFork"
            @retry="onRetry" />
        </div>
      </template>
      <div
        v-else
        class="messages"
        :class="{ stacked: appearance === 'stacked' }"
        ref="messagesEl"
        @scroll="onScroll">
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
            v-for="block in blocks"
            :key="blockKey(block)"
            class="msg-block">
            <ChatItem
              v-for="item in block"
              :key="itemKey(item)"
              :item="item"
              :variant="variantOfItem(item)"
              :show-thinking="showThinking"
              @select="onSelect"
              @fork="onFork"
              @retry="onRetry" />
          </div>
        </template>
        <template v-else>
          <ChatItem
            v-for="item in transcript"
            :key="itemKey(item)"
            :item="item"
            :variant="variantOfItem(item)"
            :show-thinking="showThinking"
            @select="onSelect"
            @fork="onFork"
            @retry="onRetry" />
        </template>
      </div>
      <!-- the activity line (card t_e9baac4b): phase-aware narration
           above the composer, mode-independent, hidden when idle. The
           old in-column status line was the always-on "reasoning…"
           label; statusLine now carries only non-phase leftovers
           (the attach-failed error) and renders in its place. -->
      <div
        v-if="activityText"
        class="activity-line">{{ activityText }}</div>
      <div
        v-else-if="statusLine"
        class="status-line">{{ statusLine }}</div>
      <button
        v-if="jumpPill"
        class="jump-live"
        @click="jumpToLive">
        ⌄ live
      </button>
    </div>

    <AppComposer
      ref="composerEl"
      v-model:draft="draft"
      :model="model"
      :models="models"
      :busy="busy"
      :can-retry="canRetry"
      :thinking="thinking"
      :attachments="state.attachments"
      :fork-anchor="state.forkFrom ?? null"
      @clear-fork="host.clearFork(stateKey)"
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
  import { computed, inject, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
  import {
    CHAT_HOST_KEY,
    activityPhase,
    type ChatState,
    type MessageItem,
    type TranscriptItem,
  } from "../composables/chat";
  import { variantOf, type VariantBlock } from "../composables/omega";
  import {
    COMPOSER_FOCUS_KEY,
    focusDelivers,
    type ComposerFocusSignal,
  } from "../composables/focus";
  import {
    createElapsedMeter,
    type ElapsedMeter,
    createItemKeySource,
    isAtBottom,
    pillVisible,
    lastUserText,
    liveRunSlice,
    scrollFlushTick,
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

  // --- stacked live-run layout (card t_c40164cf): derived, no new
  // state - the last user message is the pinned header, the items after
  // it are the run's agent body; the branch mounts for the run's
  // lifetime and the settled column renders the same transcript in flow
  const liveRun = computed(() => liveRunSlice(transcript.value));
  const liveLayout = computed(
    () =>
      props.appearance === "stacked" &&
      busy.value &&
      liveRun.value.user !== null,
  );

  // --- the activity line (card t_e9baac4b): the phase is a pure
  // derivation of the transcript + the session's busy gate (no new
  // state, chat.ts owns it). One elapsed meter per phase stretch -
  // recreated on the phase swap, so the counter restarts exactly on
  // the event that ended one phase and started the next (and a
  // restored still-running tool counts on from the server's
  // elapsed-so-far through initialMs, like the ToolCallLine meter).
  // Idle: the key is "" and the meter stops - the line is hidden.
  const activity = computed(() => activityPhase(transcript.value, busy.value));
  const tickLabel = ref("");
  const activityText = computed(() => {
    const a = activity.value;
    if (!a) return "";
    return tickLabel.value ? `${a.label} · ${tickLabel.value}` : a.label;
  });
  let tick: ElapsedMeter | null = null;
  watch(
    () => activity.value?.key ?? "",
    () => {
      if (tick) {
        tick.stop();
        tick = null;
      }
      tickLabel.value = "";
      const a = activity.value;
      if (!a) return;
      tick = createElapsedMeter(
        (label) => (tickLabel.value = label),
        undefined,
        undefined,
        a.initialMs ?? 0,
      );
      tick.start();
    },
    { immediate: true },
  );
  onBeforeUnmount(() => tick?.stop());

  // --- scroll pinning + jump-to-live pill: PER INSTANCE now - multiple
  // panes may render the same component, each owns its scrollbar ---
  const messagesEl = ref<HTMLElement | null>(null);
  const liveBodyEl = ref<HTMLElement | null>(null);
  const pinned = ref(true);
  const jumpPill = ref(false);

  // the ACTIVE scroll container: the live-run body owns scrolling while
  // the live layout is mounted (.messages listeners suspend for the
  // branch's lifetime - both containers carry the same scroll wiring);
  // otherwise the settled column scrolls as always
  function scrollEl(): HTMLElement | null {
    return liveBodyEl.value ?? messagesEl.value;
  }

  function nearBottom(): boolean {
    const el = scrollEl();
    if (!el) return true;
    return isAtBottom(el.scrollTop, el.clientHeight, el.scrollHeight);
  }

  function onScroll() {
    pinned.value = nearBottom();
    const el = scrollEl();
    if (!el) return;
    jumpPill.value = pillVisible(el.scrollTop, el.clientHeight, el.scrollHeight);
  }

  // live-run branch transitions (card t_c40164cf): mounting the layout
  // starts tail-follow from zero; unmounting it (agent_end) lands the
  // settled column at the run's block - the same post-run feel as the
  // tail-follow the branch just held. Post-flush: the scroll reads the
  // freshly patched settled element (the v-else column remounts on the
  // swap), not the pre-patch world.
  watch(
    liveLayout,
    (live, was) => {
      if (live && !was) {
        pinned.value = true;
        jumpPill.value = false;
      } else if (!live && was) {
        pinned.value = true;
        jumpPill.value = false;
        nextTick(() => {
          const el = messagesEl.value;
          if (el) el.scrollTop = el.scrollHeight;
        });
      }
    },
    { flush: "post" },
  );

  function scrollDown(force = false) {
    if (force) pinned.value = true;
    // rAF only while the document can paint; a hidden document never
    // fires rAF callbacks, which stalled the tail-follow for every run
    // the user wasn't watching. scrollFlushTick documents the trade.
    const tick = scrollFlushTick(document.hidden);
    (
      tick === "raf" ? requestAnimationFrame : (fn: () => void) => setTimeout(fn, 0)
    )(() => {
      const el = scrollEl();
      if (!el) return;
      if (!force && !pinned.value) return;
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

  // --- variants (t_a62eeb43) ---
  //
  // variantOfItem resolves ONE transcript item's branch block: an
  // assistant message whose store id is a sibling of a >=2-count branch
  // point. The live tail (no id), user bubbles and plain sessions
  // resolve to null - the switcher renders only there.
  function variantOfItem(item: TranscriptItem): VariantBlock | null {
    const st = state.value;
    if (!item || item.kind !== "message" || item.role !== "assistant") return null;
    return variantOf(item as MessageItem, st?.variants);
  }
  function onSelect(messageId: string, index: number) {
    void host.selectVariant(stateKey, messageId, index);
  }
  function onFork(messageId: string) {
    void host.fork(stateKey, messageId, props.model, thinkingLevelWire(props.thinking));
  }

  // --- composer auto-focus (card t_b40131ac): the app signals chat
  // selections and type-to-capture keystrokes through the focus signal;
  // each pane renders only its ACTIVE tab, so the instance whose state
  // key matches the request is by construction the visible one - the
  // focused tab's composer receives, hidden tabs never match. sync
  // flush keeps type-to-capture inside the keydown's task, so the
  // character lands in the newly focused textarea (the insertion event
  // targets whatever owns focus after keydown). The mount check covers
  // a freshly minted tab: openChatTab sets focusedKey before this
  // instance exists, so the pending signal delivers on mount instead.
  const composerEl = ref<{ focus: () => void } | null>(null);
  const focusSignal = inject<ComposerFocusSignal | null>(COMPOSER_FOCUS_KEY, null);
  function deliverFocus() {
    if (focusDelivers(focusSignal!, stateKey)) composerEl.value?.focus();
  }
  if (focusSignal) {
    watch(
      () => [focusSignal.focusKey.value, focusSignal.seq.value] as const,
      deliverFocus,
      { flush: "sync" },
    );
  }

  // --- mount: the tab's restore + live-attach entry ---
  onMounted(async () => {
    // scroll wiring is the template's @scroll on both containers - it
    // re-binds on every branch swap (the live-run v-if/v-else replaces
    // the settled column element; an onMounted-bound listener would die
    // with the first swap and leave the settled column deaf)
    // pending selection focus for a tab minted after the request (the
    // new-chat / fresh-session open path)
    if (focusSignal && focusSignal.focusKey.value !== null) deliverFocus();
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
    if (state.value) state.value.notify = undefined;
  });
</script>