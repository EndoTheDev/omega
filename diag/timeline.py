import json, sys, os
d = sys.argv[1]
obs = json.loads(json.loads(open(os.path.join(d,'client-obs.txt')).read()))
t0 = min(f['t'] for f in obs['fetches'])
print('== fetches (s from first fetch) ==')
for f in obs['fetches']:
    print(round((f['t']-t0)/1000.0,2), f['m'], f['u'])
print('== clicks ==')
for c in obs['clicks']:
    print(round((c['t']-t0)/1000.0,2), c)
print('== samples (only when asst changes) ==')
prev = None
for s in obs.get('samples', []):
    if 'asst' not in s:
        continue
    key = json.dumps(s.get('asst'))
    if key != prev:
        print(round((s['t']-t0)/1000.0,2), 'asst=', json.dumps(s['asst']), 'user=', s.get('user'), 'cancel=', s.get('cancel'))
        prev = key