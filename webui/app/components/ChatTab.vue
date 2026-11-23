<template>
  <main class="chat pane">
    <!--
      wrap owns the pill's absolute placement AND the vertical stack: the column keeps the
      scrollport full-pane-width and parks the activity/status line below it, above the
      composer. ONE scrollport, always: the live pin is a STICKY row, and the append follow
      is gated on the pinned flag.
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
        <template v-if="appearance === 'stacked'">
          <!-- a run is one block's subtree: runs never span blocks -->
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

    <!-- the visible queue: sends parked for the live turn, in delivery order, deletable
         until the boundary takes them. A delivered row becomes an ordinary user message -
         the transcript carries no badge. -->
    <div
      v-if="queued.length"
      class="steer-queue">
      <div
        v-for="row in queued"
        :key="row.id"
        class="steer-row">
        <span class="steer-text">{{ row.content }}</span>
        <button
          class="steer-del"
          type="button"
          title="remove from the queue"
          aria-label="remove from the queue"
          @click="onQueueDelete(row.id)">×</button>
      </div>
    </div>
    <div
      v-if="hintText"
      class="send-hint">{{ hintText }}</div>

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

    <ModelSwitchDialog
      v-if="pendingSwitch"
      :model="pendingSwitch.value"
      :tokens="pendingSwitch.tokens"
      :window="pendingSwitch.window"
      :loss="pendingSwitch.loss"
      :busy="switchBusy"
      :error="switchError"
      :notice="switchNotice"
      @compact="chooseModelSwitch('compact')"
      @anyway="chooseModelSwitch('anyway')"
      @cancel="closeModelSwitch" />
  </main>
</template>

<script setup lang="ts">
  // ONE chat tab: the transcript renders FROM the canvas view-model (panel snapshot +
  // deltas, item-mapped with identity so a repaint does not remount the pane); the state
  // keeps the composer transport side (draft, chips, variants).
  import { computed, inject, nextTick, onBeforeUnmount, onMounted, provide, ref, watch } from "vue";
  import {
    CHAT_FOLD_PINS_KEY,
    CHAT_HOST_KEY,
    EPHEMERAL_DRAFT_REF,
    activityPhase,
    createCanvasTranscript,
    findMessageItem,
    sendHint,
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
    transcriptHasPending,
    variantAnchors,
    fetchSessionContext,
    type Appearance,
    type Session,
    type TranscriptItem,
    type VariantBlock,
    type ModelProviderSection,
    type ToolItem,
  } from "../composables/omega";
  import {
    closeModelSwitchDialog,
    openModelSwitchDialog,
    planModelSwitch,
    runModelSwitchChoice,
    type ModelSwitchPlan,
    type ModelSwitchChoice,
  } from "../composables/modelSwitch";
  import { dispatchCommand } from "../composables/commands";
  import type { DockTab } from "../composables/dock";
  import { groupToolRuns, isToolRun, foldAddress, type ChatEntry } from "../composables/toolGroup";
  import { pendingVerdictList, refreshNotifications } from "./shell/notificationFeed";
  import AppComposer from "./chat/AppComposer.vue";
  import ChatItem from "./ChatItem.vue";
  import GateCard from "./chat/GateCard.vue";
  import ModelSwitchDialog from "./chat/ModelSwitchDialog.vue";
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
  // the ephemeral bit: the draft marker or a bound `eph-` id - a reload re-mints the
  // state, so the no-persist contract rides the binding
  const isEphemeral =
    props.tab.sessionRef === EPHEMERAL_DRAFT_REF ||
    (props.tab.sessionRef?.startsWith("eph-") ?? false);

  // ensure BEFORE the computeds read the map: setup runs pre-render
  host.ensureState(stateKey, sessionId.value, isEphemeral || undefined);
  const state = computed(() => host.states.get(stateKey));

  // The tab's VIEW record lives on the store record (ChatState.view), never in this
  // instance and never behind <KeepAlive>: DockPane renders ONE outlet for the ACTIVE tab,
  // a closed tab must not keep its DOM alive, and no cache survives a reload, a session
  // rebind or the DnD re-key. Small by construction - the panel's honest window is 40 rows
  // (agent/api/chat_panel.go). Faces wired here: the scroll place + follow latch, the fold
  // pins (CHAT_FOLD_PINS_KEY), the activity phase anchor.
  function viewRecord(): ChatViewState | null {
    return state.value?.view ?? null;
  }
  // the fold pins the rows reach through inject, keyed by the row's durable address
  // (toolGroup.ts foldAddress)
  const foldPins: ChatFoldPins = {
    get: (key) => viewRecord()?.folds.get(key),
    set: (key, open) => {
      const view = viewRecord();
      if (view) view.folds.set(key, open);
    },
  };
  provide(CHAT_FOLD_PINS_KEY, foldPins);
  function rowFoldKey(item: TranscriptItem): string | undefined {
    return item.kind === "tool" ? foldAddress(item) : undefined;
  }
  // the container is keyed by its HEAD row (entryKey below), so the group pin rides that
  // same head: a growing run keeps both the element and the reader's pin
  function groupFoldKey(items: ToolItem[]): string | undefined {
    const head = items[0];
    const address = head ? foldAddress(head) : undefined;
    return address ? `group:${address}` : undefined;
  }

  // The pinned model belongs to the SESSION: reopening restores what it last used, and the
  // app PATCHes the row on our emit. The workspace pin lives in the status bar (app.vue).
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
  // The model-switch guard: a pick whose target window is smaller than the live context is
  // a BLOCKING decision (compact and switch / switch anyway / cancel). Every pick that fits
  // applies exactly as it always did. The numbers come from the server's own estimate, never
  // a client-side guess, so the dialog can never quote a figure the compactor disagrees with.
  interface PendingSwitch {
    value: string;
    tokens: number;
    window: number;
    loss: number;
  }
  const pendingSwitch = ref<PendingSwitch | null>(null);
  const switchBusy = ref(false);
  const switchError = ref("");
  const switchNotice = ref("");
  let switchProbeSeq = 0;

  function applyModel(v: string) {
    modelValue.value = v;
    emit("update:model", v);
  }

  async function onModelChange(v: string) {
    if (v === modelValue.value) return;
    const id = sessionId.value;
    if (!id) {
      applyModel(v);
      return;
    }
    // one probe per pick; a later pick supersedes an in-flight one
    const seq = ++switchProbeSeq;
    let plan: ModelSwitchPlan = { kind: "apply" };
    try {
      plan = planModelSwitch(await fetchSessionContext(id, v));
    } catch {
      // an unmeasured pick applies: nothing was measured, nothing is lost
      plan = { kind: "apply" };
    }
    if (seq !== switchProbeSeq) return;
    if (plan.kind === "apply") {
      applyModel(v);
      return;
    }
    switchError.value = "";
    switchNotice.value = "";
    pendingSwitch.value = {
      value: v,
      tokens: plan.tokens,
      window: plan.window,
      loss: plan.loss,
    };
    openModelSwitchDialog();
  }

  function closeModelSwitch() {
    pendingSwitch.value = null;
    switchError.value = "";
    switchNotice.value = "";
    closeModelSwitchDialog();
  }

  async function chooseModelSwitch(choice: ModelSwitchChoice) {
    if (!pendingSwitch.value || switchBusy.value) return;
    switchBusy.value = true;
    switchError.value = "";
    switchNotice.value = "";
    const target = pendingSwitch.value.value;
    const result = await runModelSwitchChoice(choice, {
      compact: async () => {
        const id = sessionId.value;
        if (!id) throw new Error("no session to compact");
        const res = await dispatchCommand("/compact", "", id);
        return res.text ?? "";
      },
      // the compact's answer lands in the pane's status line BEFORE the switch emits
      report: (line) => {
        if (state.value) {
          state.value.statusLine = line || "compacted this conversation";
        }
      },
      // a compact that folded nothing still answers - read the context again
      // so the switch is HELD instead of truncating silently
      probe: async () => {
        const id = sessionId.value;
        if (!id) return null;
        try {
          return await fetchSessionContext(id, target);
        } catch {
          return null;
        }
      },
      apply: () => applyModel(target),
    });
    switchBusy.value = false;
    if (!result.applied) {
      if (result.error) {
        switchError.value = `compact failed: ${result.error}`;
        return;
      }
      if (result.stillOver) {
        pendingSwitch.value = {
          value: target,
          tokens: result.stillOver.tokens,
          window: result.stillOver.window,
          loss: result.stillOver.loss,
        };
        switchNotice.value = "the compact freed too little - the context still overflows. switch anyway accepts the loss, or cancel.";
        return;
      }
    }
    closeModelSwitch();
  }
  onBeforeUnmount(() => {
    // an unmounted pane gives back only its own gate slot - another tab's dialog may be up
    if (pendingSwitch.value) closeModelSwitchDialog();
  });

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
  // the live chip tail is read AT THE ROW (ToolCallLine, keyed by call id); this projection
  // is PURE, so a landed chunk repaints one row and never re-derives the transcript.
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

  // The steer queue the surface renders, and what the next send will do. A SLEEPING agent
  // is an idle session still holding unsettled calls (a yielded turn): a send wakes it now
  // rather than waiting for the wake turn.
  const queued = computed(() => state.value?.queued ?? []);
  const sleeping = computed(() => !busy.value && transcriptHasPending(transcript.value));
  const hintText = computed(() =>
    sendHint({ busy: busy.value, sleeping: sleeping.value, hasDraft: draft.value.trim() !== "" }),
  );
  function onQueueDelete(id: string) {
    void host.deleteQueued(stateKey, id);
  }
  const canRetry = computed(
    // ephemeral: nothing persisted to replay, so the affordance never renders
    () =>
      !busy.value &&
      sessionId.value !== null &&
      lastUserText(transcript.value) !== null &&
      !state.value?.ephemeral,
  );

  // the retry affordance lives on the message action row. BOTH signals are derived here:
  // the run must be idle (canRetry), and the item must be the NEWEST assistant response.
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
  // the head row of the LAST block while that block is the live turn. Every other row
  // renders in flow, so the scrollport keeps the full history: a mid-stream scroll-up
  // reaches it, where rendering the run body alone made scrollTop 0 the top of the world.
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

  // the feed is pulled on EVENT HINTS, never on a timer
  watch(busy, (live) => {
    if (live) void refreshNotifications();
  });
  // a standalone gate card mounts at the tail OUTSIDE the transcript: its height is
  // growth the RO must see
  watch(
    () => sessionGateBinding.value.standalone.length,
    () => nextTick(resizeContentRO),
  );

  // one elapsed meter per phase stretch, recreated on the phase swap: the counter restarts
  // on the event that ended one phase and began the next, with a running tool's initialMs.
  const activity = computed(() => activityPhase(transcript.value, busy.value));
  const tickLabel = ref("");
  const activityText = computed(() => {
    const a = activity.value;
    if (!a) return "";
    return tickLabel.value ? `${a.label} · ${tickLabel.value}` : a.label;
  });
  let tick: ElapsedMeter | null = null;

  // THE PHASE ANCHOR IS STORED: the line narrates the RUN and a dock-tab switch unmounts this
  // pane, so the clock must survive it. The anchor (run, phase, startedAt) lives on the tab's
  // view record; a mount into the SAME run's SAME phase resumes, anything else mints fresh -
  // the run counter and the phase key block a stale resume.
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
  // the scroller's height at the LAST scroll event, plus the scrollTop OUR last follow
  // write landed at (the browser clamps it) - the follow-write sentinel. RO callbacks never
  // touch the height: a fold's clamped arrival at the bottom fires its scroll event with
  // this value still pre-fold, so that event reads as layout, not as a user scroll.
  let lastScrollHeight = 0;
  let followWrite = -1;

  // the transcript has ONE scroll writer: the follow.

  let contentRO: ResizeObserver | null = null;
  const roTargets = new WeakSet<HTMLElement>();

  function resizeContentRO(): void {
    const el = messagesEl.value;
    if (!el) return;
    if (typeof ResizeObserver === "undefined") return;
    if (contentRO === null) contentRO = new ResizeObserver(onContentResize);
    // the container itself: a pane drag or the wrap reflow changes the box height
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
    // the reader's place leaves with the pane; write it back one last time, since a switch
    // can land between the last scroll event and the unmount
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

  // The scrollport is per INSTANCE (two panes may render one session) but the reader's PLACE
  // is per TAB: a switch unmounts this component, so each scroll event writes the position and
  // the follow latch to the tab's view record, and the remount reads them back.
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
    // the shrink-clamp guard's fuel (resumesFollow) is the height BEFORE the restore write,
    // else the restore's own scroll event reads as a reader decision (the followWrite law)
    lastScrollHeight = el.scrollHeight;
    pinned.value = view.followPinned;
    jumpPill.value = pillVisible(el.scrollTop, el.clientHeight, el.scrollHeight);
    if (view.followPinned) {
      // a followed pane LIVES at the tail: the stored offset went stale while it was away
      scrollDown();
      return;
    }
    const max = Math.max(el.scrollHeight - el.clientHeight, 0);
    const target = Math.min(Math.max(view.scrollTop, 0), max);
    if (Math.abs(el.scrollTop - target) < 1) return;
    el.scrollTop = target;
    // OUR write, not a reader scroll: followWrite absorbs the event it raises
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

  // the painted transcript changed: follow the tail, then re-prime the RO targets. The pinned
  // flag moves only on real scroll events, never after an append (the scroll-pinning law).
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
    // the fork anchor is consumed here and rides a validated target; a stale one degrades
    // to a plain send (the next send is plain until the chip is armed again)
    if (fork) host.setFork(stateKey, null);
    host.submit(stateKey, text, modelValue.value, thinkingLevelWire(props.thinking), fork ?? undefined);
  }
  function onRetry() {
    void host.retry(stateKey);
  }

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

  // consecutive tool rows fold into collapsible group blocks; a variant-anchored row stays
  // in flow so its switcher stays visible
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
  // the anchor renders in the composer chip only while its target stays rendered (a
  // refetch swap retires it silently)
  const forkAnchor = computed(() => {
    const id = state.value?.forkFrom;
    if (!id) return null;
    return findMessageItem(transcript.value, id) ? id : null;
  });

  // composer auto-focus: each pane renders only its ACTIVE tab, so the instance whose state
  // key matches the request is by construction the visible one - hidden tabs never match.
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
    // scroll wiring stays the template's @scroll: it re-binds on every branch swap, where an
    // onMounted listener would die with the first swap and leave the column deaf. The
    // restored view comes FIRST - the latch it sets is what the RO registration fire reads,
    // so priming never forces a mount-time jump and a parked reader stays put.
    nextTick(() => {
      restoreScroll();
      resizeContentRO();
    });
    // pending selection focus for a tab minted after the request (new-chat open path)
    if (focusSignal && focusSignal.focusKey.value !== null) deliverFocus();
    if (sessionId.value) host.load(stateKey, sessionId.value);
  });

  watch(sessionId, (id) => {
    if (!id) return;
    if (!host.states.get(stateKey)?.loaded) host.load(stateKey, id);
  });

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