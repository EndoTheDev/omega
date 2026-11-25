@echo off
REM Build script for omega. Runs vet + tests, then builds to bin\.
REM Usage: build.bat

cd /d "%~dp0"

if not exist bin mkdir bin

echo ==^> vet
go vet ./...
if errorlevel 1 exit /b 1

echo ==^> test
go test ./...
if errorlevel 1 exit /b 1

echo ==^> build
for /f "delims=" %%v in ('git describe --tags --abbrev=0 2^>nul') do set VERSION=%%v
if not defined VERSION set VERSION=dev
REM The icon group lives in the PE resource table: generate it before the build,
REM prove it landed after, or every shell surface shows a faceless exe.
echo ==^> icon resource
go run .\scripts\desktopicon -syso
if errorlevel 1 exit /b 1
REM -H=windowsgui: no console on a shell double-click; AttachConsole at startup decides
REM the terminal launch (CLI loop) from the shell launch (window).
go build -ldflags "-H=windowsgui -X main.omegaVersion=%VERSION%" -o bin\omega.exe .\cmd\omega
if errorlevel 1 exit /b 1
go run .\scripts\desktopicon -check bin\omega.exe
if errorlevel 1 exit /b 1
REM cmd.exe and PowerShell do not wait for a GUI-subsystem exe; the wrapper waits.
copy /y cmd\omega.cmd bin\omega.cmd >nul
echo    bin\omega.exe (version: %VERSION%)
echo ==^> done
