
import { makePane, makeSplit, removePane, splitPane, serialize, deserialize, findComponentTab } from "../app/composables/dock.ts";

// default tree: row[sidebar, main(chat), inspector]
let t = makeSplit("row", [makePane("sidebar", 313), makePane("chat"), makePane("inspector", 318)]);
// close left sidebar via toggleEdge's owned-pane path
const sbTab = findComponentTab(t, "sidebar");
console.log("sidebar tab:", sbTab);
t = removePane(t, sbTab.paneId);
console.log("after sidebar close:", JSON.stringify(serialize(t)).length, "bytes;", t.kind, t.children?.length ?? "n/a");
// close right sidebar
const insTab = findComponentTab(t, "inspector");
t = removePane(t, insTab.paneId);
console.log("after inspector close: kind=", t.kind, "tabs=", t.tabs?.map(x=>x.component||x.kind));
// roundtrip
const rt = deserialize(serialize(t));
console.log("roundtrip:", rt ? "valid" : "NULL (invalid tree!)");

// repro 2: split main right, then close both sidebars, then close one main pane
let t2 = makeSplit("row", [makePane("sidebar", 313), makePane("chat"), makePane("inspector", 318)]);
const main = t2.children[1];
t2 = splitPane(t2, main.id, "row", "after");
console.log("after split: root", t2.kind, t2.children.length, "children kinds:", t2.children.map(c=>c.kind).join(","));
t2 = removePane(t2, findComponentTab(t2, "sidebar").paneId);
t2 = removePane(t2, findComponentTab(t2, "inspector").paneId);
console.log("sidebars gone: root", t2.kind, "children:", (t2.children||[]).map(c=>c.kind).join(","));
const mainPanes = [];
const walk = n => { if (n.kind==="split") n.children.forEach(walk); else mainPanes.push(n.id); };
walk(t2);
t2 = removePane(t2, mainPanes[0]);
console.log("one main pane closed: root kind:", t2.kind, "children:", (t2.children||[]).map(c=>c.kind).join(","), "or tabs:", t2.tabs?.length);
console.log("serialize valid:", !!deserialize(serialize(t2)));
