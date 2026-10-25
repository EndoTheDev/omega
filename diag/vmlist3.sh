#!/bin/bash
set -u
export JOURNEY_BASE_URL="${2:-http://127.0.0.1:18431}"
export JOURNEY_CHROME_PORT="${DIAG_CDP:-9433}"
export JOURNEY_SESSION="cody-diag30-probe"
export AB="$HOME/.hermes/tools/agent-browser-0.26.0-linux-arm64/bin/agent-browser-linux-arm64"
ab() { "$AB" --session "$JOURNEY_SESSION" "$@"; }
export -f ab
ab eval --stdin <<'EOF'
(()=>{
  const root=document.getElementById('__nuxt');
  const inst=root&&root._vnode&&root._vnode.component;
  if(!inst)return 'NOINST';
  const unref=(v)=>(v&&v.__v_isRef)?v.value:v;
  const seenI=new Set(),seenV=new Set();
  const out=[];let n=0;
  const walkInst=(i,d)=>{if(!i||d>30||seenI.has(i))return;seenI.add(i);n++;
    const nm=i.type&&(i.type.name||i.type.__name||'anon');
    let hits=[];
    try{
      const ss=i.setupState;
      if(ss){
        const keys=Object.keys(ss);
        if(keys.length)out.push('T'+d+' '+nm+' keys='+keys.slice(0,40).join(',').slice(0,240));
      }
    }catch(e){}
    // children instances via parent links: walk ALL instance children at once
    // full sweep through subTree vnodes below
    if(i.subTree)walkV(i.subTree,d+1);
  };
  const walkV=(v,d)=>{
    if(!v||d>34||seenV.has(v))return;seenV.add(v);
    if(v.component){walkInst(v.component,d);}
    const kids=[];
    if(Array.isArray(v.children))for(const c of v.children){if(c&&typeof c==='object'&&c.type)kids.push(c);}
    if(v.suspense){if(v.suspense.subTree)kids.push(v.suspense.subTree);
      if(Array.isArray(v.suspense.effects)||v.suspense.effects){}
      if(v.suspense.pendingBranch)kids.push(v.suspense.pendingBranch);}
    if(v.vnode&&v.vnode!==v)kids.push(v.vnode);
    for(const k of kids)walkV(k,d);
  };
  try{walkInst(inst,0);}catch(e){return 'ERR'+e}
  return 'instances=' + n + '\n'+out.slice(0,90).join('\n');
})()
EOF