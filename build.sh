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
go build -o bin/omega.exe ./cmd/omega
echo "==> done: bin/omega.exe"