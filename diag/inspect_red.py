import json
def dec(p):
    s = json.load(open(p))
    return json.loads(s) if isinstance(s, str) else s
d = 'diag/evidence/run-25Oct174816'
o = dec(d + '/client-obs.txt')
t0 = min(f['t'] for f in o['fetches'])
print('== fetches (canvas/transcript/variants/events):')
for f in o['fetches']:
    if any(x in f.get('u','') for x in ('/ui/canvas','/transcript','/variants','/events')):
        print(round((f['t']-t0)/1000,2), f['m'], f['u'].split('?')[0],
              'resp', round((f.get('respT',0)-t0)/1000,2), (f.get('body') or '')[:180])
print('== clicks:')
for c in o['clicks']:
    print(round((c['t']-t0)/1000,2), 'vArrow' if c.get('vArrow') else c.get('cls'), 'connected=', c.get('connected'), 'dis=', c.get('disabled'))
print('== samples:')
for s in o['samples']:
    print(round((s['t']-t0)/1000,2), json.dumps({k:v for k,v in s.items() if k!='t'})[:200])