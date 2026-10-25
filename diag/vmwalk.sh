#!/bin/bash
# vmwalk.sh - dump Vue internals of the live probe page (session arg)
set -u
HROOT="$(cd "$(dirname "$0")/.." && pwd)"
export JROOT="$HROOT/scripts/journeys"
export JOURNEY_BASE_URL="${2:-http://127.0.0.1:18431}"
export JOURNEY_CHROME_PORT="${DIAG_CDP:-9433}"
export JOURNEY_SESSION="cody-diag30-probe"
export AB="$HOME/.hermes/tools/agent-browser-0.26.0-linux-arm64/bin/agent-browser-linux-arm64"
ab() { "$AB" --session "$JOURNEY_SESSION" "$@"; }
jsq() { ab eval "$1"; }
jsq "
(()=>{
  const a=document.querySelector('#app') && document.querySelector('#app').__vue_app__;
  if(!a)return 'NO_VUE_APP';
  const out=[];
  const walk=(inst,depth,path)=>{
    if(!inst||depth>14||out.length>60)return;
    let ss=inst.setupState;
    if(ss){
      const keys=[];
      for(const k in ss){
        try{
          const v=ss[k];
          const t=typeof v;
          if(t==='object'&&v){
            let j='';
            try{j=JSON.stringify(v,(kk,vv)=>(vv&&String(kk).length>4&&(Array.isArray(vv)||typeof vv!=='function'))?(Array.isArray(vv)?'['+vv.length+'arr]':typeof vv):vv).slice(0,220);}catch(e){j='<ns>'}
            keys.push(k+'='+j);
          } else if(t==='function'){keys.push(k+'=fn');}
        }catch(e){keys.push(k+'=<e>')}
      }
      if(keys.length)out.push('INST '+path+' : '+keys.join(' | ').slice(0,800));
    }
    const s=inst.subTree;
    const visit=(v,d)=>{
      if(!v||out.length>60||d>14)return;
      if(v.component){walk(v.component,d+1,path+'/C')}
      if(v.children){
        if(Array.isArray(v.children)||v.suspense) for(const c of v.children||[]){}
      }
      if(Array.isArray(v.dynamicChildren))for(const c of v.dynamicChildren)visit(c,d+1);
      // full vnode tree walk
      const kids=[];
      if(Array.isArray(v.children))kids.push(...v.children.filter(c=>c&&typeof c==='object'&&c.type));
      for(const k of kids)visit(k,d+1);
    };
    if(s)visit(s,depth);
  };
  try{walk(a._instance,0,'root')}catch(e){out.push('WALK ERR '+e)}
  return out.join('\n');
})()
"