#!/usr/bin/env bash
# Build a runnable Skills Registry.app bundle (arm64, unsigned by default).
#
#   scripts/bundle.sh [--release] [--sign "Developer ID Application: …"]
#
# Output: build/Skills Registry.app
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"

CONFIG="debug"
SIGN_ID=""
while [ $# -gt 0 ]; do
    case "$1" in
        --release) CONFIG="release"; shift ;;
        --sign) SIGN_ID="$2"; shift 2 ;;
        *) echo "unknown arg: $1" >&2; exit 1 ;;
    esac
done

echo "▸ Building ($CONFIG, arm64)…"
swift build -c "$CONFIG" --arch arm64

BIN="$(swift build -c "$CONFIG" --arch arm64 --show-bin-path)/SkillsRegistry"
[ -x "$BIN" ] || { echo "binary not found at $BIN" >&2; exit 1; }

APP="$ROOT/build/Skills Registry.app"
echo "▸ Assembling $APP"
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"

cp "$BIN" "$APP/Contents/MacOS/SkillsRegistry"
cp "$ROOT/Resources/Info.plist" "$APP/Contents/Info.plist"

# Icon: generate if missing (best-effort; app still runs without it).
if [ ! -f "$ROOT/Resources/AppIcon.icns" ]; then
    echo "▸ Generating app icon…"
    bash "$HERE/make-icon.sh" "$ROOT/Resources/AppIcon.icns" || echo "  (icon generation skipped)"
fi
[ -f "$ROOT/Resources/AppIcon.icns" ] && cp "$ROOT/Resources/AppIcon.icns" "$APP/Contents/Resources/AppIcon.icns"

# MarkdownUI bundles resources next to the binary; copy any .bundle dirs in.
BIN_DIR="$(dirname "$BIN")"
for b in "$BIN_DIR"/*.bundle; do
    [ -e "$b" ] && cp -R "$b" "$APP/Contents/Resources/" || true
done

if [ -n "$SIGN_ID" ]; then
    echo "▸ Code-signing with: $SIGN_ID"
    codesign --force --deep --options runtime --sign "$SIGN_ID" "$APP"
    codesign --verify --verbose "$APP"
else
    # Ad-hoc sign so Gatekeeper lets a locally-built app launch.
    echo "▸ Ad-hoc signing (unsigned distribution)…"
    codesign --force --deep --sign - "$APP" || echo "  (ad-hoc sign skipped)"
fi

echo "✓ Built $APP"
echo "  open \"$APP\""
