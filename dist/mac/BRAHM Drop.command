#!/bin/bash
# BRAHM Drop launcher for macOS — picks the right build for your Mac's chip.
cd "$(dirname "$0")" || exit 1

if [ "$(uname -m)" = "arm64" ]; then
  BIN="./brahmdrop-mac-apple"   # Apple Silicon (M1/M2/M3/M4)
else
  BIN="./brahmdrop-mac-intel"   # Intel Macs
fi

# Clear the "downloaded from internet" quarantine flag so it runs.
xattr -dr com.apple.quarantine "$BIN" 2>/dev/null
chmod +x "$BIN" 2>/dev/null

exec "$BIN"
