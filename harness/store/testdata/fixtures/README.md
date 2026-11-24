# Store fixtures (the migration gate's input)

A real pre-JSONL store, sanitized and committed here, is what
`.github/workflows/fixtures.yml` boots on every `v*` tag. The legacy migration
only runs for a store that HAS a `sessions` table and LACKS the `session_files`
watermark, so a fresh CI store never reaches it - which is why v0.18.0's
boot-killing divergence shipped green.

`legacy-v0.18.0.sqlite` + `legacy-v0.18.0.MANIFEST.json` are one pair: the
fixture and the receipt a reviewer reads instead of the content.

## Naming

`.sqlite`, never `.db` (the repo ignores `*.db`), and no `data/` path segment
anywhere in the path (that whole tree is ignored). Prove a new fixture is
committable before adding it:

```bash
git check-ignore -v harness/store/testdata/fixtures/legacy-v0.18.0.sqlite   # prints nothing
git ls-files --error-unmatch harness/store/testdata/fixtures/legacy-v0.18.0.sqlite
```

## Producing one (operator run, read-only, on the host that holds the store)

`scripts/store-fixture-extract.py` opens the source as
`file:<path>?mode=ro&immutable=1`, so it cannot write the store, touch a WAL or
take a lock. It writes only into `--out`.

```bash
python3 scripts/store-fixture-extract.py \
  --source /home/endo/.omega/data/omega.db.2026-11-24.bak \
  --out /tmp/store-fixture --name legacy-v0.18.0
python3 scripts/store-fixture-extract.py --self-check /tmp/store-fixture/legacy-v0.18.0.sqlite
```

Then copy the two files back and commit them together. The self-check is the
machine proof that no source value left the tree; the manifest carries the
source sha256, the row counts, the per-session class table, and the
known-broken session's hashed id with its byte-preserved stamps.

`scripts/store-fixture-extract_test.sh` holds the sanitizer to its own gate
(markers in every content column, zero may survive; the scrubber disabled must
leak AND must turn `--self-check` red).
