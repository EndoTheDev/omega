#!/bin/bash
set -u
export JOURNEY_BASE_URL="${2:-http://127.0.0.1:18431}"
export JOURNEY_CHROME_PORT="${DIAG_CDP:-9433}"
export JOURNEY_SESSION="cody-diag30-probe"
export AB="$HOME/.hermes/tools/agent-browser-0.26.0-linux-arm64/bin/agent-browser-linux-arm64"
ab() { "$AB" --session "$JOURNEY_SESSION" "$@"; }
ab eval --stdin <<'EOF'
(()=>{
  const root=document.getElementById('__nuxt');
  if(!root)return 'NO __nuxt';
  const vn=root._vnode;
  const out={hasVnode:!!vn, comp:!!(vn&&vn.component), appOnRoot:!!root.__vue_app__};
  const inst=vn&&vn.component;
  if(!inst)return JSON.stringify(out);
  const seen=new Set();let n=0;const names=[];
  const walk=(i,d)=>{if(!i||d>22||seen.has(i))return;seen.add(i);n++;
    const nm=i.type&&(i.type.name||i.type.__name||'?');
    if(nm!=='?')names.push(nm);
    const visit=(v,depth)=>{if(!v||depth>26)return;if(v.component)walk(v.component,depth+1);if(Array.isArray(v.children))for(const c of v.children){if(c&&typeof c==='object'&&c.type)visit(c,depth+1);}};
    try{if(i.subTree)visit(i.subTree,0);}catch(e){}};
  try{walk(inst,0);}catch(e){out.err=String(e)}
  out.n=n; out.names=names.slice(0,80);
  return JSON.stringify(out);
})()
EOF