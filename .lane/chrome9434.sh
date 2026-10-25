#!/bin/bash
# long-lived headless chrome for the probe lane (CDP 9434)
UD=/home/endo/.hermes/profiles/cody/cache/scratch/omega-chrome-9434
if [ -e "$UD" ]; then rm -rf "$UD" || true; fi
mkdir -p "$UD"
B=$(ls $HOME/.hermes/tools/chromium-*/chrome-linux/chrome.real | head -1)
LI=$(find ~/.hermes/profiles -maxdepth 2 -type d -name chromium-libs | head -1)
[ -n "$LI" ] || LI=$(dirname "$B")
exec env LD_LIBRARY_PATH="$LI:$(dirname "$B")" "$B" \
  --headless=new --remote-debugging-port=9434 \
  --user-data-dir="$UD" --no-first-run --no-default-browser-check \
  --disable-gpu --window-size=1280,720 --noerrdialogs \
  --use-mock-keychain --password-store=basic