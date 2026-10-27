#!/usr/bin/env bash
set -euo pipefail

# Build script for omega. Runs vet + tests, then builds to bin/.
# Usage: ./build.sh

cd "$(dirname "$0")"

mkdir -p bin

echo "==> vet"
go vet $(go list ./... | grep -v '/bin/')
echo "==> dep law (engine/ai/ and engine/agent/ never import modules/)"
if OMEGA_GATE_FIXTURE=scripts/gate-deps_testdata/bad.txt ./scripts/gate-deps.sh; then
	echo "self-test broken: fixture violation not detected"
	exit 1
fi
./scripts/gate-deps.sh
echo "==> webui bundle gate (no conflict markers in the embedded SPA)"
if grep -rqE '<<<<<<<|Stashed changes' engine/http_channel/static/; then
	echo "conflict markers in the embedded SPA - regenerate via modules/ui/scripts/build.sh"
	exit 1
fi
echo "==> test"
go test $(go list ./... | grep -v '/bin/')
echo "==> build"
# Short stamp shape (card t_8acdfe17): tag + 8-char hash, e.g.
# v0.9.1-nightly.20261022-c268097e - same shape the VCS-derived fallback
# produces for unstamped module builds (see cmd/omega shortBuildVersion).
TAG=$(git describe --tags --abbrev=0 2>/dev/null || echo "")
SHORT=$(git rev-parse --short=8 HEAD 2>/dev/null || echo "")
if [ -n "$TAG" ] && [ -n "$SHORT" ]; then
	VERSION="$TAG-$SHORT"
else
	VERSION="dev"
fi
# Windows needs .exe; macOS/Linux don't.
if [ "$(go env GOOS)" = "windows" ]; then
	BIN=bin/omega.exe
else
	BIN=bin/omega
fi
go build -ldflags "-X main.omegaVersion=$VERSION" -o "$BIN" ./cmd/omega
echo "    $BIN (version: $VERSION)"
echo "==> done"