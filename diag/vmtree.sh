#!/bin/bash
set -u
export JOURNEY_CHROME_PORT="${DIAG_CDP:-9433}"
export JOURNEY_SESSION="cody-diag30-probe"
export AB="$HOME/.hermes/tools/agent-browser-0.26.0-linux-arm64/bin/agent-browser-linux-arm64"
ab() { "$AB" --session "$JOURNEY_SESSION" "$@"; }
ab eval --stdin <<'EOF'
(()=>{
  const root=document.getElementById('__nuxt');
  const inst=root&&root._vnode&&root._vnode.component;
  if(!inst)return 'NOINST';
  const describe=(v)=>{
    if(!v)return 'null';
    let t=typeof v.type==='string'?v.type:(v.type&&(v.type.name||v.type.__name||'comp'));
    if(v.type===null)t='NULLTYPE';
    const info=['t:'+t];
    if(v.suspense)info.push('SUSPENSE');
    if(v.branch!=null)info.push('branch');
    if(Array.isArray(v.children))info.push('arrKids='+v.children.length);
    else if(v.children&&typeof v.children==='object')info.push('objKids');
    else if(v.children!=null)info.push('kidsType='+typeof v.children);
    if(v.shapeFlag!=null)info.push('shape='+v.shapeFlag);
    if(v.component)info.push('component='+(v.component.type&&(v.component.type.name||v.component.type.__name||'?')));
    return info.join(' ');
  };
  const v0=inst.subTree||root._vnode;
  const lines=['ROOT: '+describe(v0)];
  const dump=(v,depth)=>{
    if(!v||depth>4)return;
    const kids=[];
    if(Array.isArray(v.children))for(const c of v.children){if(c&&typeof c==='object'&&'type'in c)kids.push(c);}
    if(v.suspense){lines.push('  '.repeat(depth+1)+'SUSP {subTree='+describe(v.suspense.subTree)+', pendingBranch='+(v.suspense.pendingBranch?describe(v.suspense.pendingBranch):'null')+'}');
      if(v.suspense.subTree)kids.push(v.suspense.subTree);
      if(v.suspense.pendingBranch)kids.push(v.suspense.pendingBranch);}
    for(const k of kids.slice(0,25)){lines.push('  '.repeat(depth+1)+describe(k));dump(k,depth+1);}
  };
  dump(v0,0);
  return lines.slice(0,120).join('\n');
})()
EOF