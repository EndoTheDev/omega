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
            :session-id="sessionId"
            :show-thinking="showThinking"
            @select="onSelect"
            @fork="onFork" />
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
            :session-id="sessionId"
            :show-thinking="showThinking" />
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
              :retryable="retryableOfItem(item)"
              :session-id="sessionId"
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
            :retryable="retryableOfItem(item)"
            :session-id="sessionId"
            :show-thinking="showThinking"
            @select="onSelect"
            @fork="onFork"
            @retry="onRetry" />
        </template>
      </div>
      <!-- the activity line (card t_e9baac4b): phase-aware narration
           above the composer, mode-independent, hidden when idle. The
           phases derive from the rendered items (canvas rows painted
           through the deltas; the restore fallback otherwise). -->
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
      :thinking="thinking"
      :attachments="state.attachments"
      :fork-anchor="forkAnchor"
      @clear-fork="host.setFork(stateKey, null)"
      @update:model="emit('update:model', $event)"
      @update:thinking="emit('update:thinking', $event)"
      @send="onSend"
      @cancel="host.cancel(stateKey)"
      @attach-files="(imgs: never[]) => host.attachFiles(stateKey, imgs)"
      @remove-attachment="(i: number) => host.removeAttachment(stateKey, i)"
      @attach-url="(u: string) => host.attachUrl(stateKey, u)" />
  </main>
</template>

<script setup lang="ts">
  // ONE chat tab (card t_4f0e88f2): the chat component is dockable now.
  // chat-last 2a: the transcript renders FROM the canvas view-model -
  // the chat panel's snapshot + deltas, item-mapped with identity so a
  // repaint does not remount the pane. The per-tab transcript copies
  // and the SSE-composed live view are gone; the state keeps the
  // composer transport side (draft, attach chips, the REST restore's
  // fallback for sessions the single panel does not cover, variants).
  import { computed, inject, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
  import {
    CHAT_HOST_KEY,
    EPHEMERAL_DRAFT_REF,
    activityPhase,
    createCanvasTranscript,
    findMessageItem,
  } from "../composables/chat";
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
    normalizeChatPayload,
    lastUserText,
    liveRunSlice,
    scrollFlushTick,
    splitBlocks,
    thinkingLevelWire,
    variantAnchors,
    type Appearance,
    type TranscriptItem,
    type VariantBlock,
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
  // the tab owns the binding: unsaved (no sessionRef) -> composer-first.
  // EPHEMERAL_DRAFT_REF is a marker, not an id - a composer-first
  // EPHEMERAL tab's binding (the state stays unbound until the send).
  const sessionId = computed(() =>
    props.tab.sessionRef === EPHEMERAL_DRAFT_REF ? null : props.tab.sessionRef ?? null,
  );
  // the ephemeral bit: the draft marker, or a bound `eph-` id (the
  // registry's namespace - a reload re-mints the state, so the
  // no-persist contract must ride the binding)
  const isEphemeral =
    props.tab.sessionRef === EPHEMERAL_DRAFT_REF ||
    (props.tab.sessionRef?.startsWith("eph-") ?? false);

  // ensure BEFORE the computeds read the map: setup runs pre-render
  host.ensureState(stateKey, sessionId.value, isEphemeral || undefined);
  // the reactive read through the wrapped map is the view
  const state = computed(() => host.states.get(stateKey));

  // --- the transcript's render source (chat-last 2a) ---
  //
  // The chat panel (kind 'chat' on the canvas) is the streaming source:
  // when its payload covers THIS tab's session, the rows paint the pane
  // (snapshot at load, deltas after - identity-kept per delta). Any
  // other session falls back to the REST restore's rendered items.
  const canvasTranscript = createCanvasTranscript();
  const panelPayload = computed(() => {
    const panel = host.chatPanel();
    if (!panel) return null;
    return normalizeChatPayload(panel.payload);
  });
  const panelMatches = computed(
    () => !!panelPayload.value && panelPayload.value.session === sessionId.value,
  );
  const canvasItems = computed(() =>
    panelMatches.value && panelPayload.value ? canvasTranscript.apply(panelPayload.value) : null,
  );
  const transcript = computed(() => canvasItems.value ?? state.value?.restored ?? []);
  const statusLine = computed(() => state.value?.statusLine ?? "");
  const itemKey = createItemKeySource();
  // stacked layout (card t_84e4653c): the transcript cut at user
  // messages - user message + response subtree = one visual block.
  // The canvas item identity survives deltas, so the inner keys stay
  // stable through streaming repaints.
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
    // ephemeral (card t_d96a5cd5): nothing persisted to replay - the
    // affordance never renders, the server refuses the flag combination
    () =>
      !busy.value &&
      sessionId.value !== null &&
      lastUserText(transcript.value) !== null &&
      !state.value?.ephemeral,
  );

  // the retry affordance lives on the message action row (t_95d2bc34):
  // the transcript owner derives BOTH signals - the run must be idle
  // (canRetry above; the row also hides while the row's bubble streams)
  // and this item is the NEWEST assistant response, the one the retry's
  // sibling replay appends under. Older responses show fork/copy only.
  const lastAssistantId = computed(() => {
    const items = transcript.value;
    for (let i = items.length - 1; i >= 0; i--) {
      const it = items[i]!;
      if (it.kind === "message" && it.role === "assistant") return it.id ?? null;
    }
    return null;
  });
  function retryableOfItem(item: TranscriptItem): boolean {
    return (
      canRetry.value &&
      item.kind === "message" &&
      item.role === "assistant" &&
      !!item.id &&
      item.id === lastAssistantId.value
    );
  }

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
  // derivation of the rendered items + the session's busy gate (no new
  // state). One elapsed meter per phase stretch - recreated on the
  // phase swap, so the counter restarts exactly on the event that ended
  // one phase and started the next (a running tool counts on from the
  // panel's elapsed-so-far through initialMs, like the ToolCallLine
  // meter). Idle: the key is "" and the meter stops - the line is
  // hidden.
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

  // the painted transcript changes (canvas deltas, restore fetches or
  // streaming repaints): follow the tail when the view is pinned. The
  // pinned flag only changes on real scroll events - never measured
  // after the append (the scroll-pinning law).
  watch(transcript, () => scrollDown());

  // --- send / retry ---
  function onSend() {
    const text = draft.value.trim();
    if (!text) return;
    draft.value = "";
    // the fork anchor rides a validated target (resolved against the
    // rendered transcript); a stale anchor degrades to a plain send,
    // exactly as before - and consumes on send (the next send is a
    // plain one unless the chip is armed again)
    const fork = forkAnchor.value;
    if (fork) host.setFork(stateKey, null);
    host.submit(stateKey, text, props.model, thinkingLevelWire(props.thinking), fork ?? undefined);
  }
  function onRetry() {
    // the message action row's retry chip (t_95d2bc34): the chat
    // panel's retry action through the action pipe - the server replays
    // the last user turn as a sibling variant
    void host.retry(stateKey);
  }

  // --- variants (t_a62eeb43, anchor fix t_0df4efd8) ---
  //
  // variantOfItem resolves ONE rendered item's switcher block: the
  // branch anchor item, resolved once per transcript pass by
  // variantAnchors - a branch's ACTIVE head row anchors, whatever kind
  // it renders as (a multi-row replay's head is a thinking or tool
  // call row; the run's stored text bubble anchors instead, and a
  // text-less run anchors the head block itself). Canvas-mapped stored
  // rows carry the ids; live rows and user bubbles resolve to null -
  // the switcher renders only on stored rows.
  const variantAnchorMap = computed(() =>
    variantAnchors(transcript.value, state.value?.variants),
  );
  function variantOfItem(item: TranscriptItem): VariantBlock | null {
    const st = state.value;
    if (!st?.variants?.length) return null;
    const id =
      item.kind === "message" || item.kind === "thinking" || item.kind === "tool" ?
        item.id
      : undefined;
    if (!id) return null;
    return variantAnchorMap.value.get(id) ?? null;
  }
  function onSelect(messageId: string, index: number) {
    void host.selectVariant(stateKey, messageId, index);
  }
  function onFork(messageId: string) {
    // a USER target reruns that turn immediately (replay semantics,
    // native /chat fork); anything else anchors the composer
    const target = findMessageItem(transcript.value, messageId);
    if (!target) return;
    if (target.role === "user") {
      void host.replayFork(stateKey, messageId, props.model, thinkingLevelWire(props.thinking));
      return;
    }
    host.setFork(stateKey, messageId);
  }
  // the anchor renders in the composer chip only while its target stays
  // rendered (a refetch swap retires it silently)
  const forkAnchor = computed(() => {
    const id = state.value?.forkFrom;
    if (!id) return null;
    return findMessageItem(transcript.value, id) ? id : null;
  });

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
  // on the same session, or a detached run another window began): attach
  // this tab's own SSE copy - the deltas ride it into the canvas store
  // the pane repaints from. The mount's load() handles the already-live
  // case; this catches the later-arriving truth through refreshSessions'
  // server-active feed.
  watch(
    () => sessionId.value ? host.busyAll.value.has(sessionId.value) : false,
    (live) => {
      if (!live) return;
      const st = host.states.get(stateKey);
      // a run this tab never subscribed to: a virgin state loads (its
      // restore attaches via the detail's live flag); a loaded one
      // attaches its own copy - both tabs feed the canvas
      if (!st || st.attached || host.busyKeys.value.has(stateKey)) return;
      if (!st.loaded) {
        host.load(stateKey, sessionId.value!);
        return;
      }
      host.attach(stateKey);
    },
  );
</script>