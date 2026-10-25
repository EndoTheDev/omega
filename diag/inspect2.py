import json
def dec(p):
    s = json.load(open(p))
    return json.loads(s) if isinstance(s, str) else s
for d in ['diag/evidence/run-25Oct165539','diag/evidence/run-25Oct165721']:
    print('='*25, d)
    print('-- srv-variants (die):', dec(d+'/srv-variants.json'))
    tr = dec(d+'/srv-transcript.json')
    rows = tr.get('rows') if isinstance(tr, dict) else tr
    print('-- transcript rows:', len(rows) if rows else 0)
    for r in (rows or [])[-6:]:
        if isinstance(r, dict):
            print('  row keys:', list(r.keys())[:12])
            print('  row:', json.dumps(r)[:300])
    print('-- lane log tail:')
    print(open(d+'/lane-log-tail.txt').read()[-2500:])