#!/usr/bin/env bash
set -euo pipefail

# Build script for omega. Runs vet + tests, then builds to bin/.
# Usage: ./build.sh

cd "$(dirname "$0")"

mkdir -p bin

echo "==> vet"
go vet $(go list ./... | grep -v '/bin/')
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