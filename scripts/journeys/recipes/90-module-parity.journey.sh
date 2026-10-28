#!/bin/bash
# 90-module-parity - the module-owned surfaces journey (card t_88bb7dcf).
# REST-level on purpose: the law this card pins is "one registration,
# both ends", and the HTTP half of that is observable without a
# browser (the TUI half is Go-gated, see TestModuleOmissionDropsTUICommands).
# Pins:
#
#   inventory  - GET /extensions lists every in-process module, the
#                registry the /extensions TUI command and the webui
#                Extensions tab read; /cron's absence here is the
#                buildPlugins omission showing up at runtime.
#   seams      - the cron module answers GET /cron/jobs (the data
#                source its CronJobs.vue panel and its /cron TUI
#                command both read) - present module, mounted seam.
#
# Residue-free: read-only GETs only.
. "${JROOT:?run must export JROOT}/lib.sh"

# --- lane alive: the inventory endpoint answers a JSON array ---
payload=$(curl -s "${JOURNEY_BASE_URL}/extensions")
printf '%s' "$payload" | head -c1 | grep -q '\[' || \
  die "lane answered non-JSON to /extensions (wrong lane?): ${JOURNEY_BASE_URL}"
step "ok: extensions inventory reachable"

# --- every registered module must show up in the inventory ---
python3 - "$payload" <<'EOF'
import json, sys
names = {m.get("name") for m in json.loads(sys.argv[1])}
missing = [n for n in ("cron", "tools", "logging") if n not in names]
if missing:
    sys.stderr.write("journey RED: registered modules missing from /extensions: %r\n" % (missing,))
    sys.stderr.write("  /extensions lists MOUNTED capsules in mount order; an absent row means the"
                     " module was never mounted at boot in this build OR is toggled off"
                     " (config.yaml disabled: key) - GET /modules names the live state\n")
    sys.exit(2)
EOF
[ $? -eq 0 ] || die "module inventory incomplete (a buildPlugins omission reaches the UI surfaces)"
step "ok: cron/tools/logging inventory rows present"

# --- the cron seam answers, so both its surfaces have a source ---
jobs=$(curl -s "${JOURNEY_BASE_URL}/cron/jobs")
printf '%s' "$jobs" | head -c1 | grep -q '\[' || \
  die "GET /cron/jobs answered non-JSON: $jobs"
step "ok: cron seam mounted (CronJobs.vue + /cron data source)"

step "green: module-owned surfaces agree with one registration"