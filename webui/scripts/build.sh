#!/bin/sh
# build the webui and sync it into the go embed dir. Run `go build` after
# this to bake the SPA into the binary (go:embed captures static/ at
# compile time). static/ (api/static/) is tracked in git, but commits to it come ONLY
# from scripts/embed-lane.sh (the embed lane) after a source merge lands
# - webui cards and branches commit SOURCE ONLY and never touch static/,
# so parallel webui cards can never collide on it. go install / go build
# users always get the complete product because the lane keeps static/
# true before the next build.
#
# Works from a clean clone: deps are installed when node_modules is
# missing, and the module-UI composition (nuxt.config.ts eval time)
# regenerates app/composables/module-panels.generated.ts itself - there
# are no local-path assumptions, the module scan is relative to the
# checkout.
set -e
cd "$(dirname "$0")/.."
# The empty-dir trap (winbtw, 2026-11-06, twice): the node_modules
# DIRECTORY can exist-but-EMPTY and a dir-only check then skips install -
# nuxt generate dies with ERR_MODULE_NOT_FOUND. Check a marker FILE npm ci
# always leaves behind (node_modules/.package-lock.json) instead: a missing
# marker means install, whether the dir is missing entirely or wiped empty.
if [ ! -f node_modules/.package-lock.json ]; then
	echo "==> npm ci (node_modules absent or empty: no .package-lock.json marker)"
	npm ci --no-audit --no-fund
fi
# generate, not build: ssr:false build emits a nitro server, generate emits
# the static SPA (index.html + _nuxt/) that the go binary embeds
npx nuxt generate
rm -rf ../api/static
cp -r dist ../api/static
echo "synced webui/dist -> api/static"