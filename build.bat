@echo off
REM Build script for omega. Runs vet + tests, then builds to bin\.
REM Usage: build.bat

cd /d "%~dp0"

if not exist bin mkdir bin

echo ==^> vet
go vet ./agent/... ./ai/... ./cmd/... ./gateway/... ./harness/...
if errorlevel 1 exit /b 1

echo ==^> test
go test ./agent/... ./ai/... ./cmd/... ./gateway/... ./harness/...
if errorlevel 1 exit /b 1

echo ==^> build
go build -o bin\omega.exe .\cmd\omega
if errorlevel 1 exit /b 1

echo ==^> done: bin\omega.exe