import json, sys, os
d = sys.argv[1]
obs = json.loads(json.loads(open(os.path.join(d,'client-obs.txt')).read()))
t0 = min(f['t'] for f in obs['fetches']) if obs['fetches'] else 0
for f in obs['fetches']:
    print(round((f['t']-t0)/1000.0,2), f['m'], f['u'])
print('clicks:')
for c in obs['clicks']:
    print(round((c['t']-t0)/1000.0,2), c)

for name in ['checkpointA','checkpointB']:
    fn = None
    for suf in ['-variants.json','-canvas.json']:
        p = os.path.join(d, name+suf)
        if os.path.exists(p):
            try:
                c = json.load(open(p))
            except Exception as e:
                print(name+suf, 'unparseable', e); continue
            if suf.startswith('-canvas'):
                for pan in c:
                    if pan.get('kind')=='chat':
                        pay=pan.get('payload') or {}
                        print(name, 'rows:')
                        for r in (pay.get('rows') or []):
                            b=(r.get('blocks') or [{}])[0]
                            print('   ',r.get('role'),r.get('id'),b.get('type'),'streaming' if r.get('status')=='streaming' else '', repr((b.get('content') or ''))[:40])
            else:
                print(name, 'variants:', json.dumps(c))