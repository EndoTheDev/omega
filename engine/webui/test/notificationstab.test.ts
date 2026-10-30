// omega webui: the notifications dock tab (card t_180db2cb).
//
// Endo's two rulings (2026-10-29): (1) the top-right bell dies entirely
// - the notifications dockable's icon chip carries the unread badge;
// (2) transient toasts STAY for live events - the tab holds the durable
// feed/history. Three layers, following the composition-test
// convention (the node suite never compiles the SFC):
// - the registry: the dockables entry is an inspector view like the
//   other secondary-pane tabs;
// - the badge: the pure badge voice + the render chain from the feed
//   store to the dock tab chip;
// - the structure: the bell is gone from the vocabulary AND from the
//   menubar's rendered chrome (template-level, not a repo-wide grep).
// Run: node --test engine/webui/test/ (node >= 23 runs TS natively)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import {
  DOCKABLES,
  DOCK_COMPONENTS,
  INSPECTOR_TAB_COMPONENTS,
  MENU_DOCKABLES,
  dockableOf,
} from "../app/composables/dockables.ts";
import { ICONS, ICON_NAMES } from "../app/composables/icons.ts";

const root = fileURLToPath(new URL("..", import.meta.url));
const read = (rel: string) => readFileSync(`${root}/${rel}`, "utf8");

// --- the registry entry: an inspector view like its siblings ------------

test("notifications is a registered inspector component: singleton, pinned right, unclosable", () => {
  assert.ok(
    (DOCK_COMPONENTS as readonly string[]).includes("notifications"),
    "the openable union carries it",
  );
  const d = dockableOf("notifications");
  assert.equal(d.title, "Notifications", "Title Case like the shell tabs");
  assert.equal(d.area, "right", "the secondary pane home");
  assert.equal(d.policy, "singleton");
  assert.equal(d.closable, false, "unclosable like every inspector view");
  assert.equal(d.icon, "inbox", "the glyph is the inbox, not the bell");
  assert.ok(ICONS[d.icon], "the icon is an iconset name (the membership law)");
  assert.ok(
    INSPECTOR_TAB_COMPONENTS.includes("notifications"),
    "the loader seats it in the right shell with the rest",
  );
  assert.ok(MENU_DOCKABLES.includes("notifications"), "the '+' menu offers the tab");
  // the icon key exists exactly once - one name one meaning
  const n = DOCKABLES.filter((x) => x.icon === "inbox").length;
  assert.equal(n, 1, "single registry entry owns the icon name");
});

// --- the unread badge: store voice -> dock tab chip ---------------------

test("the badge lives on the dockable's icon chip, not on dead window chrome", () => {
  const pane = read("app/components/DockPane.vue");
  // the chip renders inside the tab when the badge text is non-empty
  assert.match(pane, /v-if="tabBadge\(t\)"/, "zero renders no chip element");
  assert.match(pane, /class="dock-tab-badge"/, "the chip is the tab's badge span");
  assert.match(pane, /tabBadge\(t\) \+ ' unread'/, "an accessible name rides the chip");
  // the badge reads the page-lifetime feed store and ONLY the
  // notifications tab carries one
  assert.match(
    pane,
    /tab\.component === "notifications" \? notificationsBadgeText\.value : ""/,
    "the badge is the notifications tab's, nobody else's",
  );
  // the css companion: inside the chip box (no overflow past the tab's
  // edge, t_aca53fee's lesson), theme vars only (t_11a9e160's lesson)
  const css = read("app/assets/css/main.css");
  const rule = css.match(/\.dock-tab-badge\s*\{[^}]*\}/);
  assert.ok(rule, "the badge rule exists in main.css");
  assert.match(rule[0], /position: absolute/, "anchored in the tab chip's own box");
  assert.match(rule[0], /var\(--accent\)/, "the accent rides the theme variable");
  assert.ok(!rule[0].includes("top: -"), "no negative offset past the chip edge");
  assert.match(rule[0], /pointer-events: none/, "the click is the tab's, never the badge's");
});

test("the badge text is the store's computed over unread + pending questions", () => {
  const store = read("app/components/notifications/store.ts");
  assert.match(
    store,
    /notificationsBadge = computed/,
    "the reactive badge is the store's, one page-lifetime source",
  );
  assert.match(
    store,
    /unread\.value \+ pendingVerdicts\.value\.length/,
    "finished-things unread AND pending questions sum into the chip",
  );
  assert.match(
    store,
    /notificationBadgeText\(notificationsBadge\.value\)/,
    "the chip text rides the pure helper (the 99+ cap, zero renders nothing)",
  );
});

// --- the bell is gone (structure, not a repo grep) -----------------------

test("the bell left the iconset vocabulary with the chrome that carried it", () => {
  const set = new Set(ICON_NAMES as readonly string[]);
  assert.ok(!set.has("bell"), "no bell vocabulary entry survives");
  assert.ok(set.has("inbox"), "the notifications tab's glyph is the inbox");
});

test("the menubar's rendered chrome carries no bell entry point", () => {
  const src = read("app/components/shell/MenuBar.vue");
  // isolate the TEMPLATE: what the menubar would actually render
  // (comments stripped - prose may tell the story the DOM may not have)
  const template = src
    .slice(0, src.indexOf("<script"))
    .replace(/<!--[\s\S]*?-->/g, "");
  assert.ok(!template.includes("bell"), "no bell button, icon or id in the chrome");
  assert.ok(!template.includes("NotificationCenter"), "no bell popover mount");
  // what the menubar DOES host is the toast stack (ruling 2)
  assert.match(template, /<NotificationToasts\b/, "the transient toasts stay in the menubar");
  assert.match(template, /@open="\$emit\('open-notification'/, "open intents still route to app.vue");
});

test("the notifications surface is a dock tab, not window chrome", () => {
  const app = read("app/app.vue");
  // the outlet seats the panel like the other inspector views
  assert.match(app, /#pane-notifications/, "the pane outlet exists");
  const outlet = app.slice(app.indexOf("#pane-notifications"));
  assert.match(outlet, /<NotificationsPanel/, "the outlet renders the feed panel");
  assert.match(outlet, /@open="openNotification"/, "row clicks route through the same router");
  // and the mobile drawer seats it before the module catch-all, like the
  // other shell-owned tabs (t_d7fd3ad3's branch rule)
  const drawer = app.slice(app.indexOf('side="right"'));
  assert.match(
    drawer,
    /inspectorDrawerActiveId === 'notifications'/,
    "the drawer has its own branch (the catch-all would render an empty body)",
  );
});

// --- open/focus marks read (the badge clears on open) --------------------

test("opening or focusing the tab marks the feed read", () => {
  const panel = read("app/components/notifications/NotificationsPanel.vue");
  // the outlet mounts ONLY the active tab, so the panel's mount IS the
  // open/focus event
  assert.match(panel, /onMounted\(async/, "the activation runs in the mount");
  assert.match(panel, /refreshNotifications\(\)/, "the fresh truth pulls first");
  assert.match(
    panel,
    /notificationsUnread\.value > 0[\s\S]*markAllRead\(\)/,
    "unread finished-things clear in one POST (the badge drops on open)",
  );
  const store = read("app/components/notifications/store.ts");
  assert.match(store, /markAllNotificationsRead\(\)/, "markAllRead rides the existing all:true transport");
});

// --- the durable feed reuses the existing rendering ----------------------

test("the feed panel is the bell popover's body rehomed, not a rebuild", () => {
  const panel = read("app/components/notifications/NotificationsPanel.vue");
  // the same row vocabulary the popover rendered: verdict cards on top,
  // feed rows with kind glyph + title + verdict + age, empty voice
  for (const cls of ["tab-band", "notif-verdicts", "notif-list", "notif-row", "notif-empty"]) {
    assert.ok(panel.includes(cls), `the rehomed markup keeps "${cls}"`);
  }
  assert.match(panel, /nothing finished yet/, "the empty voice survives the rehome");
  assert.match(panel, /kindGlyph/, "the kind chip renders (task/cron/chat)");
  // the data source is unchanged: the same fetch wrappers, no new calls
  assert.ok(!panel.includes("fetch("), "the panel touches no transport of its own - the store owns the wire");
});