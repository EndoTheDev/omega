<template>
  <main class="chat pane">
    <!--
      wrap owns the pill's absolute placement AND the vertical stack: the column wrap
      keeps the scrollport full-pane-width (the scrollbar hugs the pane's right edge -
      the row layout stranded a phantom strip beside it) and parks the activity/status
      line BELOW the scroller, above the composer, where it cannot clip.
    -->
    <!--
      ONE scrollport, always.
      The pin is now a STICKY row inside this scrollport: at the tail it holds the
      question against the top while the run streams underneath; scrolling up releases
      it at its place in the flow and the history above is readable, and the append
      follow stays gated on the pinned flag (scrolling up detaches it - new content
      never yanks the pane).
    -->
    <div class="messages-wrap">
      <div
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
        <!--
          ONE item renderer, two container layouts: ChatItem is a fragment - it renders
          exactly the per-item DOM the old flat v-for did, so 'chat' stays
          pixel-identical.
        -->
        <template v-if="appearance === 'stacked'">
          <!--
            the block's own consecutive tool runs group - a run is one block's subtree,
            so runs never span blocks (a user message always breaks both)
          -->
          <div
            v-for="block in groupedBlocks"
            :key="blockKey(block)"
            class="msg-block">
            <template
              v-for="(entry, index) in block"
              :key="entryKey(entry)">
              <ToolGroup
                v-if="isToolRun(entry)"
                :items="entry.items"
                :gate-cards="entry.items.map(gateCardForItem)"
                :session-id="sessionId"
                :fold-key="groupFoldKey(entry.items)"
                :live="busy" />
              <div
                v-else-if="pinnedSlot(block, index)"
                class="live-pin">
                <ChatItem
                  :item="entry"
                  :gate-card="gateCardForItem(entry)"
                  :session-id="sessionId"
                  :fold-key="rowFoldKey(entry)"
                  :show-thinking="showThinking"
                  @select="onSelect"
                  @fork="onFork" />
              </div>
              <ChatItem
                v-else
                :item="entry"
                :variant="variantOfItem(entry)"
                :retryable="retryableOfItem(entry)"
                :gate-card="gateCardForItem(entry)"
                :session-id="sessionId"
                :fold-key="rowFoldKey(entry)"
                :show-thinking="showThinking"
                @select="onSelect"
                @fork="onFork"
                @retry="onRetry" />
            </template>
          </div>
        </template>
        <template v-else>
          <template
            v-for="entry in groupedTranscript"
            :key="entryKey(entry)">
            <ToolGroup
              v-if="isToolRun(entry)"
              :items="entry.items"
              :gate-cards="entry.items.map(gateCardForItem)"
              :session-id="sessionId"
              :fold-key="groupFoldKey(entry.items)"
              :live="busy" />
            <ChatItem
              v-else
              :item="entry"
              :variant="variantOfItem(entry)"
              :retryable="retryableOfItem(entry)"
              :gate-card="gateCardForItem(entry)"
              :session-id="sessionId"
              :fold-key="rowFoldKey(entry)"
              :show-thinking="showThinking"
              @select="onSelect"
              @fork="onFork"
              @retry="onRetry" />
          </template>
        </template>
        <GateCard
          v-for="card in sessionGateBinding.standalone"
          :key="card.id"
          :card="card"
          :session-id="sessionId ?? undefined" />
      </div>
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
      :model="modelValue"
      :models="models"
      :groups="groups"
      :busy="busy"
      :thinking="thinking"
      :attachments="state.attachments"
      :files="state.files"
      :fork-anchor="forkAnchor"
      @clear-fork="host.setFork(stateKey, null)"
      @update:model="onModelChange"
      @update:thinking="emit('update:thinking', $event)"
      @refresh-models="emit('refresh-models')"
      @send="onSend"
      @cancel="host.cancel(stateKey)"
      @attach-files="(imgs: never[]) => host.attachFiles(stateKey, imgs)"
      @attach-file-refs="(files: File[]) => host.attachFileRefs(stateKey, files)"
      @remove-attachment="(i: number) => host.removeAttachment(stateKey, i)"
      @remove-file-attachment="(i: number) => host.removeFileAttachment(stateKey, i)"
      @attach-url="(u: string) => host.attachUrl(stateKey, u)" />
  </main>
</template>

<script setup lang="ts">
  // ONE chat tab: the chat component is dockable now.
  // chat-last 2a: the transcript renders FROM the canvas view-model -
  // the chat panel's snapshot + deltas, item-mapped with identity so a
  // repaint does not remount the pane. The per-tab transcript copies
  // and the SSE-composed live view are gone; the state keeps the
  // composer transport side (draft, attach chips, the REST restore's
  // fallback for sessions the single panel does not cover, variants).
  import { computed, inject, nextTick, onBeforeUnmount, onMounted, provide, ref, watch } from "vue";
  import {
    CHAT_FOLD_PINS_KEY,
    CHAT_HOST_KEY,
    EPHEMERAL_DRAFT_REF,
    activityPhase,
    createCanvasTranscript,
    findMessageItem,
    type ChatFoldPins,
    type ChatViewState,
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
    bindGateQuestions,
    pillVisible,
    resumesFollow,
    normalizeChatPayload,
    lastUserText,
    liveRunSlice,
    scrollFlushTick,
    splitBlocks,
    thinkingLevelWire,
    variantAnchors,
    type Appearance,
    type Session,
    type TranscriptItem,
    type VariantBlock,
    type ModelProviderSection,
    type ToolItem,
  } from "../composables/omega";
  import type { DockTab } from "../composables/dock";
  //
  // block (render-site view, the transcript itself stays flat)
  import { groupToolRuns, isToolRun, foldAddress, type ChatEntry } from "../composables/toolGroup";
  import { pendingVerdictList, refreshNotifications } from "./shell/notificationFeed";
  import AppComposer from "./chat/AppComposer.vue";
  import ChatItem from "./ChatItem.vue";
  import GateCard from "./chat/GateCard.vue";
  import ToolGroup from "./chat/ToolGroup.vue";

  const props = defineProps<{
    tab: DockTab;
    showThinking: boolean;
    model: string;
    models: string[];
    groups: ModelProviderSection[];
    session?: Session | null;
    thinking: string;
    appearance: Appearance;
  }>();

  const emit = defineEmits<{
    (e: "update:model", value: string): void;
    (e: "update:thinking", value: string): void;
    (e: "refresh-models"): void;
  }>();

  const host = inject(CHAT_HOST_KEY)!;

  const stateKey = props.tab.key ?? "";
  // the tab owns the binding: unsaved (no sessionRef) -> composer-first.
  const sessionId = computed(() =>
    props.tab.sessionRef === EPHEMERAL_DRAFT_REF ? null : props.tab.sessionRef ?? null,
  );
  // the ephemeral bit: the draft marker, or a bound `eph-` id (the registry's namespace
  // - a reload re-mints the state, so the no-persist contract must ride the binding)
  const isEphemeral =
    props.tab.sessionRef === EPHEMERAL_DRAFT_REF ||
    (props.tab.sessionRef?.startsWith("eph-") ?? false);

  // ensure BEFORE the computeds read the map: setup runs pre-render
  host.ensureState(stateKey, sessionId.value, isEphemeral || undefined);
  const state = computed(() => host.states.get(stateKey));

  // --- the tab's VIEW record -------------------------
  //
  // DESIGN, settled: the view state lives on the store record keyed by the
  // tab (ChatState.view), never in this instance and never behind
  // <KeepAlive>. DockPane renders ONE outlet for the ACTIVE tab and the tree
  // holds no KeepAlive on purpose (a closed Files/Settings tab must not keep
  // its DOM alive); a keep-alive cache also cannot survive a reload, a
  // session rebind or the DnD move that deliberately re-keys this instance.
  // The record is small by construction - the chat panel's honest window is
  // 40 rows (agent/api/chat_panel.go) - and it is keyed by the tab because
  // this state IS the tab.
  //
  // Three faces of it are wired here: the scroll place + follow latch, the
  // fold pins (provided to the rows below through CHAT_FOLD_PINS_KEY) and
  // the activity line's phase anchor.
  function viewRecord(): ChatViewState | null {
    return state.value?.view ?? null;
  }
  // the fold-pin face the tool rows reach through inject: the reader's
  // manual open/close, keyed by the row's durable address (toolGroup.ts
  // foldAddress). Reads go through state.value on every call, so the rows
  // track the reactive record like any other store read.
  const foldPins: ChatFoldPins = {
    get: (key) => viewRecord()?.folds.get(key),
    set: (key, open) => {
      const view = viewRecord();
      if (view) view.folds.set(key, open);
    },
  };
  provide(CHAT_FOLD_PINS_KEY, foldPins);
  // the row address a tool line pins under (its own call/row identity)
  function rowFoldKey(item: TranscriptItem): string | undefined {
    return item.kind === "tool" ? foldAddress(item) : undefined;
  }
  // the group address: the container is keyed by its HEAD row (entryKey
  // below), so the pin rides that same head - a growing run keeps both the
  // element and the reader's pin
  function groupFoldKey(items: ToolItem[]): string | undefined {
    const head = items[0];
    const address = head ? foldAddress(head) : undefined;
    return address ? `group:${address}` : undefined;
  }

  // --- the session's remembered context ---
  //
  // The pinned model belongs to the SESSION, not to the app: reopening
  // restores what it last used, a mid-session change updates the row (the
  // app PATCHes on our emit). A bound tab adopts the row's pin.
  //
  // The session's WORKSPACE pin left this component with its chip (card
  //): the workdir selector lives in the status bar's left zone
  // now, and app.vue writes the same two targets it used to - the row
  // (bound) or the draft intent (unbound). ChatTab only carries the state
  // the send consumes.
  const modelValue = ref(props.session?.model || props.model);
  function restorePins() {
    const pinnedModel = props.session?.model ?? "";
    if (sessionId.value) {
      if (pinnedModel) modelValue.value = pinnedModel;
    } else {
      modelValue.value = props.model;
    }
  }
  watch(
    () => [sessionId.value, props.session?.model ?? ""] as const,
    restorePins,
  );
  watch(
    () => props.model,
    (m) => {
      if (!sessionId.value || !props.session?.model) modelValue.value = m;
    },
  );
  function onModelChange(v: string) {
    modelValue.value = v;
    emit("update:model", v);
  }

  const canvasTranscript = createCanvasTranscript();
  const panelPayload = computed(() => {
    const panel = host.chatPanel();
    if (!panel) return null;
    return normalizeChatPayload(panel.payload);
  });
  const panelMatches = computed(
    () => !!panelPayload.value && panelPayload.value.session === sessionId.value,
  );
  const canvasItems = computed(() => {
    if (!(panelMatches.value && panelPayload.value)) return null;
    return canvasTranscript.apply(panelPayload.value);
  });
  // the live chip tail: a running call's streamed
  // output rides the row whose tool call id matches - read AT THE ROW
  // (ToolCallLine, keyed by the row's own call id. The
  // store is reactive, so a landed chunk repaints the one row that owns
  // the call. The derivation below is a PURE projection: no write, no
  // chip read - so a chunk never re-derives the transcript, its blocks or
  // the activity line, and no assignment of ours can re-arm the pane
  //.
  const restoreItems = computed(() => state.value?.restored ?? []);
  const transcript = computed(() => canvasItems.value ?? restoreItems.value);
  const statusLine = computed(() => state.value?.statusLine ?? "");
  const itemKey = createItemKeySource();
  const blocks = computed(() => splitBlocks(transcript.value));
  // the outer key rides the block's first ENTRY.
  function blockKey(block: ChatEntry[]): number {
    const first = block[0];
    return first ? entryKey(first) : -1;
  }
  const draft = computed({
    get: () => state.value?.draft ?? "",
    set: (v: string) => {
      if (state.value) state.value.draft = v;
    },
  });

  // composer busy = the PER-SESSION run slot.
  const busy = computed(() => {
    const id = sessionId.value;
    if (id) return host.busyAll.value.has(id);
    return host.busyKeys.value.has(stateKey);
  });
  const canRetry = computed(
    // ephemeral: nothing persisted to replay - the affordance never renders,.
    () =>
      !busy.value &&
      sessionId.value !== null &&
      lastUserText(transcript.value) !== null &&
      !state.value?.ephemeral,
  );

  // the retry affordance lives on the message action row: the transcript owner derives
  // BOTH signals - the run must be idle (canRetry above; the row also hides while the
  // row's bubble streams) and this item is the NEWEST assistant response, the one the
  // retry's sibling replay appends.
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

  const liveRun = computed(() => liveRunSlice(transcript.value));
  const liveLayout = computed(
    () =>
      props.appearance === "stacked" &&
      busy.value &&
      liveRun.value.user !== null,
  );
  // the head row of the LAST block, while that block is the live turn: every other row
  // renders in flow exactly as it always did, so the scrollport keeps the full history
  // and a mid-stream scroll-up reaches it (the old branch rendered the run body alone -
  // scrollTop 0 was the top of the world until agent_end)
  function pinnedSlot(block: ChatEntry[], index: number): boolean {
    if (!liveLayout.value || index !== 0) return false;
    return block === groupedBlocks.value[groupedBlocks.value.length - 1];
  }

  const sessionGateBinding = computed(() =>
    bindGateQuestions(transcript.value, pendingVerdictList.value, sessionId.value ?? null),
  );
  const gateByItem = computed(() => {
    const m = new Map<TranscriptItem, NonNullable<ReturnType<typeof bindGateQuestions>["assigned"][number]["card"]>>();
    for (const a of sessionGateBinding.value.assigned) m.set(a.item, a.card);
    return m;
  });
  function gateCardForItem(item: TranscriptItem) {
    return gateByItem.value.get(item) ?? null;
  }

  // the freeze cadence: the feed is pulled on EVENT HINTS since the push channel.
  watch(busy, (live) => {
    if (live) void refreshNotifications();
  });
  // a standalone gate card mounts at the tail OUTSIDE the transcript (the verdict poll
  // binding) - its mount height is growth the RO must see
  watch(
    () => sessionGateBinding.value.standalone.length,
    () => nextTick(resizeContentRO),
  );

  // the activity line: the phase is a pure derivation of the rendered items + the. One
  // elapsed meter per phase stretch - recreated on the phase swap, so the counter
  // restarts exactly on the event that ended one phase and started the next (a running
  // tool counts on from the panel's elapsed-so-far through initialMs, like the
  // ToolCallLine meter).
  const activity = computed(() => activityPhase(transcript.value, busy.value));
  const tickLabel = ref("");
  const activityText = computed(() => {
    const a = activity.value;
    if (!a) return "";
    return tickLabel.value ? `${a.label} · ${tickLabel.value}` : a.label;
  });
  let tick: ElapsedMeter | null = null;

  // THE PHASE ANCHOR IS STORED: the activity line is
  // narration of the RUN, so a dock-tab switch - which unmounts this pane -
  // must not restart its clock. The anchor (which run, which phase, when it
  // started) lives on the tab's view record; a mount into the SAME run's
  // SAME phase resumes from it (the label keeps counting), and anything
  // else mints a fresh one. Two guards make a stale resume impossible: the
  // run counter (ChatState.runSeq bumps at every transport start, so an
  // earlier run's "thinking" can never be read as this one's) and the phase
  // key itself (the watcher below re-anchors on every phase move and clears
  // the anchor when the run idles).
  function startActivityMeter(a: ReturnType<typeof activityPhase>): void {
    if (tick) {
      tick.stop();
      tick = null;
    }
    tickLabel.value = "";
    const view = viewRecord();
    if (!a) {
      if (view) view.activity = null;
      return;
    }
    const st = state.value;
    let consumed = a.initialMs ?? 0;
    const stored = view?.activity;
    if (view && stored && st && stored.run === st.runSeq && stored.key === a.key) {
      // the same stretch, seen by a pane that had unmounted: count on
      consumed = Math.max(0, Date.now() - stored.startedAt);
    }
    if (view && st) view.activity = { run: st.runSeq, key: a.key, startedAt: Date.now() - consumed };
    tick = createElapsedMeter(
      (label) => (tickLabel.value = label),
      undefined,
      undefined,
      consumed,
    );
    tick.start();
  }
  watch(
    () => activity.value?.key ?? "",
    () => startActivityMeter(activity.value),
    { immediate: true },
  );
  onBeforeUnmount(() => tick?.stop());

  const messagesEl = ref<HTMLElement | null>(null);
  const pinned = ref(true);
  const jumpPill = ref(false);
  // the scroller's height at the LAST scroll event. RO callbacks never touch it: a
  // fold's clamped arrival at the bottom fires its scroll event with this value still
  // pre-fold, so the event reads as a layout event, not a user scroll.
  let lastScrollHeight = 0;
  // the scrollTop OUR last follow write landed at (the browser clamps it).
  let followWrite = -1;

  // the transcript has ONE scroll writer ---------- The follow.

  let contentRO: ResizeObserver | null = null;
  const roTargets = new WeakSet<HTMLElement>();

  function resizeContentRO(): void {
    const el = messagesEl.value;
    if (!el) return;
    if (typeof ResizeObserver === "undefined") return;
    if (contentRO === null) contentRO = new ResizeObserver(onContentResize);
    // the container itself: a pane drag or the wrap reflowing changes the box and can.
    if (!roTargets.has(el)) {
      roTargets.add(el);
      contentRO.observe(el);
    }
    for (const child of Array.from(el.children)) {
      if (roTargets.has(child)) continue;
      roTargets.add(child);
      contentRO.observe(child);
    }
  }

  function onContentResize(): void {
    const el = messagesEl.value;
    if (!el) return;
    scrollDown();
    jumpPill.value = pillVisible(el.scrollTop, el.clientHeight, el.scrollHeight);
  }

  onBeforeUnmount(() => {
    // the reader's place leaves with the pane; write it back one last time
    // (a switch can land between the last scroll event and the unmount)
    recordScroll();
    contentRO?.disconnect();
    contentRO = null;
  });

  function onScroll() {
    const el = messagesEl.value;
    if (!el) return;
    if (followWrite >= 0 && el.scrollTop === followWrite) {
      followWrite = -1;
      lastScrollHeight = el.scrollHeight;
      jumpPill.value = pillVisible(el.scrollTop, el.clientHeight, el.scrollHeight);
      recordScroll();
      return;
    }
    followWrite = -1;
    pinned.value = resumesFollow(
      pinned.value,
      lastScrollHeight,
      el.scrollTop,
      el.clientHeight,
      el.scrollHeight,
    );
    lastScrollHeight = el.scrollHeight;
    jumpPill.value = pillVisible(el.scrollTop, el.clientHeight, el.scrollHeight);
    recordScroll();
  }

  // --- the view record's scroll half -----------------
  //
  // The scrollport is per INSTANCE (two panes may render the same session),
  // but the reader's PLACE is per TAB: a dock-tab switch unmounts this
  // component and the remount used to land at the tail, yanking a reader
  // who was reading history. The values live on the tab's view record:
  // every scroll event writes the position and the follow latch, and the
  // mount after a switch reads them back.
  function recordScroll(): void {
    const el = messagesEl.value;
    const view = viewRecord();
    if (!el || !view) return;
    view.scrollTop = el.scrollTop;
    view.followPinned = pinned.value;
  }

  function restoreScroll(): void {
    const el = messagesEl.value;
    const view = viewRecord();
    if (!el || !view) return;
    // the shrink-clamp guard's fuel (resumesFollow) is the height BEFORE the
    // restore write: without this priming the restore's own scroll event
    // would compare against a stale height and could read as a reader
    // decision (the followWrite law,
    lastScrollHeight = el.scrollHeight;
    pinned.value = view.followPinned;
    jumpPill.value = pillVisible(el.scrollTop, el.clientHeight, el.scrollHeight);
    if (view.followPinned) {
      // a followed pane LIVES at the tail: the stored offset is stale the
      // moment content grew while the tab was away, and the latch's promise
      // is "new content lands on screen"
      scrollDown();
      return;
    }
    const max = Math.max(el.scrollHeight - el.clientHeight, 0);
    const target = Math.min(Math.max(view.scrollTop, 0), max);
    if (Math.abs(el.scrollTop - target) < 1) return;
    el.scrollTop = target;
    // OUR write, not a reader scroll: the event it raises must not be read
    // as "the reader moved" (exactly what followWrite exists for)
    followWrite = el.scrollTop;
    jumpPill.value = pillVisible(el.scrollTop, el.clientHeight, el.scrollHeight);
  }

  // a run starting: the send path lands the reader at the tail and re-arms.
  watch(liveLayout, (live) => {
    if (!live) return;
    pinned.value = true;
    jumpPill.value = false;
    scrollDown();
    nextTick(resizeContentRO);
  });

  function scrollDown(force = false) {
    if (force) pinned.value = true;
    // rAF only while the document can paint; a hidden document never fires rAF.
    const tick = scrollFlushTick(document.hidden);
    (
      tick === "raf" ? requestAnimationFrame : (fn: () => void) => setTimeout(fn, 0)
    )(() => {
      const el = messagesEl.value;
      if (!el) return;
      if (!force && !pinned.value) return;
      const target = Math.max(el.scrollHeight - el.clientHeight, 0);
      if (el.scrollTop >= target - 1) return;
      el.scrollTop = el.scrollHeight;
      followWrite = el.scrollTop;
    });
  }

  function jumpToLive() {
    jumpPill.value = false;
    scrollDown(true);
  }

  // the painted transcript changes (canvas deltas, restore fetches or streaming. The
  // pinned flag only changes on real scroll events - never measured after the append
  // (the scroll-pinning law). The RO target list rides each paint too: new items and
  // branch swaps must enter the observation set.
  watch(transcript, () => {
    scrollDown();
    nextTick(resizeContentRO);
  });

  function onSend() {
    const text = draft.value.trim();
    if (!text) return;
    const fork = forkAnchor.value;
    // slash commands run locally and never hit the agent.
    if (text.startsWith("/")) {
      draft.value = "";
      void host.command(stateKey, text, {
        model: modelValue.value,
        thinking: thinkingLevelWire(props.thinking),
        fork: fork ?? undefined,
      });
      return;
    }
    draft.value = "";
    // the fork anchor rides a validated target (resolved against the rendered
    // transcript); a stale anchor degrades to a plain send, exactly as before - and
    // consumes on send (the next send is a plain one unless the chip is armed again)
    if (fork) host.setFork(stateKey, null);
    host.submit(stateKey, text, modelValue.value, thinkingLevelWire(props.thinking), fork ?? undefined);
  }
  function onRetry() {
    void host.retry(stateKey);
  }

  // variants --- variantOfItem resolves ONE rendered item's switcher block.
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

  // tool-call grouping: consecutive tool rows --- groupToolRuns folds each render
  // list's runs of consecutive tool items into collapsible group blocks; a
  // variant-anchored row stays in flow (the switcher must stay visible).
  const groupedTranscript = computed(() => groupToolRuns(transcript.value, variantOfItem));
  const groupedBlocks = computed(() =>
    blocks.value.map((block) => groupToolRuns(block, variantOfItem)),
  );
  // entry keys ride ITEM identity.
  function entryKey(entry: ChatEntry): number {
    return isToolRun(entry) ? itemKey(entry.items[0]!) : itemKey(entry);
  }

  function onSelect(messageId: string, index: number) {
    void host.selectVariant(stateKey, messageId, index);
  }
  function onFork(messageId: string) {
    // a USER target reruns that turn immediately (replay semantics, native /chat fork).
    const target = findMessageItem(transcript.value, messageId);
    if (!target) return;
    if (target.role === "user") {
      void host.replayFork(stateKey, messageId, modelValue.value, thinkingLevelWire(props.thinking));
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

  // composer auto-focus: the app signals chat selections and type-to-capture keystrokes
  // through the focus signal; each pane renders only its ACTIVE tab, so the instance
  // whose state key matches the request is by construction the visible one - the
  // focused tab's composer receives, hidden tabs never match.
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

  onMounted(async () => {
    // scroll wiring is the template's @scroll on both containers - it
    // re-binds on every branch swap (the live-run v-if/v-else replaces
    // the settled column element; an onMounted-bound listener would die
    // with the first swap and leave the settled column deaf)
    // the RO targets start observing after the first paint batch - a
    // registration fire rides the pinned gate, so priming this way never
    // forces a mount-time jump. The restored view comes
    // FIRST: the latch it sets is what the registration fire reads (card
    // - a reader parked in the history is not yanked to the tail
    // by their own return to the tab).
    nextTick(() => {
      restoreScroll();
      resizeContentRO();
    });
    // pending selection focus for a tab minted after the request (the
    // new-chat / fresh-session open path)
    if (focusSignal && focusSignal.focusKey.value !== null) deliverFocus();
    if (sessionId.value) host.load(stateKey, sessionId.value);
  });

  watch(sessionId, (id) => {
    if (!id) return;
    if (!host.states.get(stateKey)?.loaded) host.load(stateKey, id);
  });

  // a run starting for the session AFTER this tab mounted.
  watch(
    () => sessionId.value ? host.busyAll.value.has(sessionId.value) : false,
    (live) => {
      if (!live) return;
      const st = host.states.get(stateKey);
      // a run this tab never subscribed.
      if (!st || st.attached || host.busyKeys.value.has(stateKey)) return;
      if (!st.loaded) {
        host.load(stateKey, sessionId.value!);
        return;
      }
      host.attach(stateKey);
    },
  );
</script>

<style scoped>
  .chat.pane {
    position: relative;
  }
</style>