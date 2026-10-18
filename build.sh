#!/usr/bin/env bash
set -euo pipefail

# Build script for omega. Runs vet + tests, then builds to bin/.
# Usage: ./build.sh

cd "$(dirname "$0")"

mkdir -p bin

echo "==> vet"
go vet $(go list ./... | grep -v '/bin/')
echo "==> dep law (ai/ and agent/ never import modules/)"
if OMEGA_GATE_FIXTURE=gate-deps_testdata/bad.txt ./gate-deps.sh; then
	echo "self-test broken: fixture violation not detected"
	exit 1
fi
./gate-deps.sh
echo "==> test"
go test $(go list ./... | grep -v '/bin/')
echo "==> build"
VERSION=$(git describe --tags --abbrev=0 2>/dev/null || echo "dev")
# Windows needs .exe; macOS/Linux don't.
if [ "$(go env GOOS)" = "windows" ]; then
	BIN=bin/omega.exe
else
	BIN=bin/omega
fi
go build -ldflags "-X main.omegaVersion=$VERSION" -o "$BIN" ./cmd/omega
echo "    $BIN (version: $VERSION)"
echo "==> done"