import json
def dec(p):
    s = json.load(open(p))
    return json.loads(s) if isinstance(s, str) else s
d = 'diag/evidence/run-25Oct174816'
o = dec(d + '/client-obs.txt')
t0 = min(f['t'] for f in o['fetches'])
print('== sessions/action fetches 5..14s:')
for f in o['fetches']:
    tt = (f['t']-t0)/1000
    if 5 <= tt <= 15 and '/sessions?arch' not in f.get('u','') and '/logs' not in f.get('u',''):
        print(round(tt,2), f['m'], f['u'].split('?')[0])
print('== last 8 samples:')
for s in o['samples'][-8:]:
    tt = (s['t']-t0)/1000
    a = s.get('asst') or []
    print(round(tt,2), 'sw=', [b.get('sw') for b in a], 'cursor=', [b.get('cursor') for b in a], 'n=', len(a))
# dom switcher states
dom = dec(d + '/dom.txt')
for s in dom.get('switchers', []):
    print('DOM switcher:', s['pos'], 'connected', s['connected'])