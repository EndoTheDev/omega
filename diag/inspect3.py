import json
def dec(p):
    s = json.load(open(p))
    return json.loads(s) if isinstance(s, str) else s
for d in ['diag/evidence/run-25Oct165539','diag/evidence/run-25Oct165721','diag/evidence/run-25Oct170043']:
    print('='*25, d)
    for name in ['checkpointA-variants.json','checkpointB-variants.json','checkpointC-variants-200ms.json','srv-variants.json']:
        print(f'  {name}:', json.dumps(json.load(open(d+'/'+name))))
    for name in ['checkpointA-canvas.json','checkpointB-canvas.json','checkpointC-canvas-200ms.json','srv-canvas.json']:
        try:
            c = dec(d+'/'+name)
        except Exception as e:
            print(f'  {name}: ERR {e}'); continue
        chat = None
        for p in (c if isinstance(c, list) else []):
            if p.get('kind') == 'chat':
                chat = p; break
        if chat:
            pl = chat.get('payload') or {}
            print(f'  {name}: session={pl.get("session")} rows={[(r.get("id"), r.get("role"), r.get("status")) for r in (pl.get("rows") or [])]}')
        else:
            print(f'  {name}: <no chat panel>')
    # transcript: items ids
    for name in ['srv-transcript.json']:
        t = dec(d+'/'+name)
        items = t.get('items') if isinstance(t, dict) else t
        if items is not None:
            print(f'  {name}: running={t.get("running") if isinstance(t,dict) else "?"} items={[(it.get("kind"), it.get("id"), (it.get("content") or "")[:20]) for it in items]}')
        else:
            print(f'  {name}: keys={list(t.keys()) if isinstance(t,dict) else type(t).__name__}')