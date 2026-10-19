#!/bin/sh
# build the webui and sync it into the go embed dir. Run `go build` after
# this to bake the SPA into the binary (go:embed captures static/ at
# compile time). static/ is tracked in git: after webui changes, run this
# and commit the synced output with your change so go install / go build
# users always get the complete product.
#
# Works from a clean clone: deps are installed when node_modules is
# missing, and the module-UI composition (nuxt.config.ts eval time)
# regenerates app/composables/module-panels.generated.ts itself - there
# are no local-path assumptions, the module scan is relative to the
# checkout.
set -e
cd "$(dirname "$0")/.."
if [ ! -d node_modules ]; then
	echo "==> npm ci (clean clone: node_modules missing)"
	npm ci --no-audit --no-fund
fi
# generate, not build: ssr:false build emits a nitro server, generate emits
# the static SPA (index.html + _nuxt/) that the go binary embeds
npx nuxt generate
rm -rf ../http_channel/static
cp -r dist ../http_channel/static
echo "synced modules/ui/dist -> modules/http_channel/static"