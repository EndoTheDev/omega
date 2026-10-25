#!/bin/bash
set -u
export JOURNEY_CHROME_PORT="${DIAG_CDP:-9433}"
export JOURNEY_SESSION="cody-diag30-probe"
export AB="$HOME/.hermes/tools/agent-browser-0.26.0-linux-arm64/bin/agent-browser-linux-arm64"
ab() { "$AB" --session "$JOURNEY_SESSION" "$@"; }
ab eval --stdin <<'EOF'
(()=>{
  const root=document.getElementById('__nuxt');
  const a=root.__vue_app__;
  const props=['_instance','_container','_context','_uid','_component','_ceVNode'];
  const out={appKeys:Object.keys(a).filter(k=>k.startsWith('_')).slice(0,20)};
  for(const p of props)out[p]=a[p]?(typeof a[p]==='object'?'obj':'val-'+typeof a[p]):'null';
  const inst=a._instance;
  if(inst){out.rootName=inst.type&&(inst.type.name||inst.type.__name||'?');
    out.setupKeys=inst.setupState?Object.keys(inst.setupState).slice(0,30):null;}
  return JSON.stringify(out).slice(0,800);
})()
EOF