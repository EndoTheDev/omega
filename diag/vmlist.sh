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
  const app=root&&root.__vue_app__;
  if(!app)return 'NO_VUE_APP';
  const seen=new Set();let n=0;const names=[];
  const walk=(inst,d)=>{
    if(!inst||d>20||seen.has(inst))return;seen.add(inst);n++;
    const nm=inst.type&&(inst.type.name||inst.type.__name||'?');
    const ks=inst.setupState?Object.keys(inst.setupState).slice(0,40).join(','):'<none>';
    if(nm!=='?')names.push(nm+' :: '+ks);
    const visit=(v,depth)=>{if(!v||depth>24)return;if(v.component)walk(v.component,depth+1);if(Array.isArray(v.children))for(const c of v.children){if(c&&typeof c==='object'&&c.type)visit(c,depth+1);}};
    try{if(inst.subTree)visit(inst.subTree,0);}catch(e){}
  };
  try{walk(app._instance,0);}catch(e){return 'ERR '+e}
  return 'total instances: '+n+'\n'+names.slice(0,60).join('\n');
})()
EOF