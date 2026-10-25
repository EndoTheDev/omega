#!/bin/bash
# vmdump.sh - dump selected Vue component state from the live probe page.
# Walks the component tree for setupState carrying variants/panels/state
# and prints: canvas store panels, every chat state (sessionId, restored
# ids, variants blocks), and every ChatTab-like transcript's item ids.
set -u
HROOT="$(cd "$(dirname "$0")/.." && pwd)"
export JOURNEY_BASE_URL="${2:-http://127.0.0.1:18431}"
export JOURNEY_CHROME_PORT="${DIAG_CDP:-9433}"
export JOURNEY_SESSION="cody-diag30-probe"
export AB="$HOME/.hermes/tools/agent-browser-0.26.0-linux-arm64/bin/agent-browser-linux-arm64"
ab() { "$AB" --session "$JOURNEY_SESSION" "$@"; }
ab eval --stdin <<'EOF'
(()=>{
  const root = document.getElementById('__nuxt');
  const app = root && root.__vue_app__;
  if(!app) return 'NO_VUE_APP on __nuxt';
  const out=[];
  const seen=new Set();
  const unref=(v)=>(v && v.__v_isRef)?v.value:v;
  const idsOf=(arr)=>{try{return JSON.stringify((arr||[]).map(x=>x&&x.id!==undefined?[x.kind,x.role||'',x.id,(x.streaming?'LIVE':'')]:x).slice(0,14))}catch(e){return '<err>'}};
  const walk=(inst,d)=>{
    if(!inst||d>18||seen.has(inst))return;
    seen.add(inst);
    const ss=inst.setupState||{};
    let hits=[];
    try{
      for(const k in ss){
        if(k==='canvas'||k==='statesStore'||k==='canvasStore'){
          const v=unref(ss[k]);
          if(v&&v.panels){hits.push('CANVAS panels='+JSON.stringify(v.panels.map(p=>[p.id,p.kind,(p.payload&&p.payload.session),(p.payload&&p.payload.rows||[]).map(r=>[r.id,r.role,r.status])].slice(0,12)).slice(0,6)));}
        }
        if(k==='state'||k==='chatState'){
          const v=unref(ss[k]);
          if(v&&('variants' in v)){hits.push('CHATSTATE sess='+v.sessionId+' variants='+JSON.stringify((v.variants||[]).map(x=>[x.message_id,x.count,x.active]))+' restoredIds='+idsOf(v.restored)+' loaded='+v.loaded+' attached='+v.attached);}
        }
        if(k==='transcript'){
          const t=unref(ss[k]);
          if(Array.isArray(t)&&t.length<=200)hits.push('transcriptIds='+idsOf(t));
        }
        if(k==='canvasItems'){
          const t=unref(ss[k]);
          hits.push('canvasItems='+(t===null?'NULL':idsOf(t)));
        }
        if(k==='panelMatches'){
          hits.push('panelMatches='+JSON.stringify(unref(ss[k])));
        }
      }
    }catch(e){hits.push('ERR '+e)}
    if(hits.length)out.push('INST['+d+'] '+hits.join(' || ').slice(0,900));
    // children: subTree vnode walk
    const visit=(v,depth)=>{
      if(!v||depth>18)return;
      if(v.component)walk(v.component,depth+1);
      if(Array.isArray(v.children))for(const c of v.children){if(c&&typeof c==='object'&&c.type)visit(c,depth+1);}
    };
    try{if(inst.subTree)visit(inst.subTree,0);}catch(e){}
  };
  try{walk(app._instance,0);}catch(e){out.push('WALKERR '+e)}
  return out.join('\n')||'NO_HITS';
})()
EOF