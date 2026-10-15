#!/bin/sh
# build the webui and sync it into the go embed dir. Run `go build` after
# this to bake the SPA into the binary (go:embed captures static/ at
# compile time). The placeholder index.html stays tracked in git so
# a plain `go build` works without a webui build; the sync replaces it
# with the real SPA - restore it with git checkout when needed.
set -e
cd "$(dirname "$0")/.."
# generate, not build: ssr:false build emits a nitro server, generate emits
# the static SPA (index.html + _nuxt/) that the go binary embeds
npx nuxt generate
rm -rf ../extensions/http_channel/static
cp -r dist ../extensions/http_channel/static
echo "synced webui/dist -> extensions/http_channel/static"