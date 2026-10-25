(()=> {
  if (window.__diag) return "already";
  const d = { fetches: [], clicks: [], samples: [] };
  window.__diag = d;
  const of = window.fetch;
  window.fetch = function () {
    const a = arguments;
    const u = typeof a[0] === "string" ? a[0] : (a[0] && a[0].url) || "";
    const m = (a[1] && a[1].method) || "GET";
    const r = of.apply(this, arguments);
    const us = String(u);
    if (us.indexOf("/ui/canvas") >= 0 || us.indexOf("/transcript") >= 0 || us.indexOf("/variants") >= 0) {
      const ts = Date.now();
      r.then(resp => {
        try {
          resp.clone().json().then(body => {
            let lite = body;
            try {
              if (Array.isArray(body)) {
                const chat = body.find(p => p && p.kind === "chat");
                if (chat) {
                  const lit = (chat.payload || {}).rows || [];
                  lite = { chat: lit.map(r => ({ id: r.id, role: r.role, status: r.status, types: (r.blocks || []).map(b => b.type), len: (r.blocks || []).map(b => (b.content || "").length) })) };
                } else {
                  lite = "<no-chat-panel>";
                }
              } else if (body && body.variants) { lite = { variants: body.variants.slice(0, 6) }; }
              else if (body && body.items !== undefined) { lite = { items: (body.items || []).map(it => ({ k: it.kind, role: it.role || "", id: (it.id || "") + "" })), running: body.running, variants: (body.variants || []).map(v => ({ msg: v.message_id, siblings: v.siblings, active: v.active })) }; }
            } catch (e2) { lite = "<err" + e2 + ">"; }
            d.fetches.push({ t: ts, respT: Date.now(), m, u: us, body: JSON.stringify(lite) });
          }).catch(() => { d.fetches.push({ t: ts, respT: Date.now(), m, u: us, body: "<nojson>" }); });
        } catch (e) {}
      });
    } else {
      d.fetches.push({ t: Date.now(), m, u: us });
    }
    return r;
  };
  document.addEventListener("click", e => {
    const v = e.target && e.target.closest ? e.target.closest(".v-arrow") : null;
    d.clicks.push({ t: Date.now(), vArrow: !!v, disabled: v ? v.disabled : null, connected: v ? v.isConnected : null, cls: String(e.target.className).slice(0, 60) });
  }, true);
  d.sample = () => {
    const panes = [...document.querySelectorAll(".dock-pane")];
    const p = panes.find(x => [...x.querySelectorAll(".dock-tab")].some(dd => dd.getAttribute("aria-selected") === "true" && (dd.querySelector(".dock-tab-title") || { textContent: "" }).textContent.includes("(journey) retry-single")));
    if (!p) { d.samples.push({ t: Date.now(), noPane: 1 }); return; }
    const bubs = [...p.querySelectorAll(".msg.assistant")].map(m => ({
      txt: (m.querySelector(".md") || m).textContent.trim().slice(0, 24),
      cursor: !!m.querySelector(".cursor"),
      actions: !!m.querySelector(".msg-actions"),
      sw: (m.querySelector(".v-pos") || { textContent: "" }).textContent
    }));
    d.samples.push({ t: Date.now(), asst: bubs, user: p.querySelectorAll(".msg.user").length, cancel: !!p.querySelector(".composer button.send.cancel") });
  };
  d.timer = setInterval(d.sample, 100);
  return "installed";
})()