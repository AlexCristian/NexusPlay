#!/bin/sh
# Downloads the pinned Chrome for Testing headless shell used by the
# integration and screenshot tests. Screenshots are compared pixel by pixel,
# so everyone (CI included) must render with exactly this build.
set -eu
VERSION=155.0.8059.84
DEST="${1:-.cache/chrome}"
BIN="$DEST/chrome-headless-shell-linux64/chrome-headless-shell"
if [ -x "$BIN" ] && [ "$(cat "$DEST/VERSION" 2>/dev/null)" = "$VERSION" ]; then
  echo "$BIN"
  exit 0
fi
rm -rf "$DEST"
mkdir -p "$DEST"
curl -fsSL -o "$DEST/chrome.zip" \
  "https://storage.googleapis.com/chrome-for-testing-public/$VERSION/linux64/chrome-headless-shell-linux64.zip"
unzip -q "$DEST/chrome.zip" -d "$DEST"
rm "$DEST/chrome.zip"
echo "$VERSION" > "$DEST/VERSION"
echo "$BIN"
