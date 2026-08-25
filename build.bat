@echo off
REM Build script for omega. Runs vet + tests, then builds to bin\.
REM Usage: build.bat

cd /d "%~dp0"

if not exist bin mkdir bin

echo ==^> vet
go vet ./agent/... ./ai/... ./cmd/... ./gateway/... ./extensions/...
if errorlevel 1 exit /b 1

echo ==^> test
go test ./agent/... ./ai/... ./cmd/... ./gateway/... ./extensions/...
if errorlevel 1 exit /b 1

echo ==^> build
for /f "delims=" %%v in ('git describe --tags --abbrev=0 2^>nul') do set VERSION=%%v
if not defined VERSION set VERSION=dev
go build -ldflags "-X main.omegaVersion=%VERSION%" -o bin\omega.exe .\cmd\omega
if errorlevel 1 exit /b 1
echo    bin\omega.exe (version: %VERSION%)
echo ==^> done