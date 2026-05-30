#!/usr/bin/env bash
# Build a runnable Skills Registry.app bundle (arm64).
#
#   scripts/bundle.sh [--release] [--sign "Developer ID Application: …"]
#                     [--notarize] [--version X.Y.Z]
#
# Signing:
#   --sign "<id>"   sign with a Developer ID Application identity (full nested
#                   Sparkle signing + hardened runtime). Falls back to the
#                   CODESIGN_IDENTITY env var, then to an ad-hoc signature.
# Notarization (requires a real Developer ID signature):
#   --notarize      zip the app, submit to Apple's notary service, staple the
#                   ticket. Reads APPLE_ID / APPLE_TEAM_ID /
#                   APPLE_APP_SPECIFIC_PASSWORD from the environment.
#
# Output: build/Skills Registry.app
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"

CONFIG="debug"
SIGN_ID="${CODESIGN_IDENTITY:-}"
NOTARIZE=0
VERSION="${APP_VERSION:-}"
while [ $# -gt 0 ]; do
    case "$1" in
        --release) CONFIG="release"; shift ;;
        --sign) SIGN_ID="$2"; shift 2 ;;
        --notarize) NOTARIZE=1; shift ;;
        --version) VERSION="$2"; shift 2 ;;
        *) echo "unknown arg: $1" >&2; exit 1 ;;
    esac
done

echo "▸ Building ($CONFIG, arm64)…"
swift build -c "$CONFIG" --arch arm64

BIN_DIR="$(swift build -c "$CONFIG" --arch arm64 --show-bin-path)"
BIN="$BIN_DIR/SkillsRegistry"
[ -x "$BIN" ] || { echo "binary not found at $BIN" >&2; exit 1; }

APP="$ROOT/build/Skills Registry.app"
echo "▸ Assembling $APP"
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources" "$APP/Contents/Frameworks"

cp "$BIN" "$APP/Contents/MacOS/SkillsRegistry"
chmod +x "$APP/Contents/MacOS/SkillsRegistry"
cp "$ROOT/Resources/Info.plist" "$APP/Contents/Info.plist"
echo -n "APPL????" > "$APP/Contents/PkgInfo"

# Sparkle dylibs live under Contents/Frameworks; teach the binary to find them.
install_name_tool -add_rpath "@loader_path/../Frameworks" "$APP/Contents/MacOS/SkillsRegistry" 2>/dev/null || true

# Icon: generate if missing (best-effort; app still runs without it).
if [ ! -f "$ROOT/Resources/AppIcon.icns" ]; then
    echo "▸ Generating app icon…"
    bash "$HERE/make-icon.sh" "$ROOT/Resources/AppIcon.icns" || echo "  (icon generation skipped)"
fi
[ -f "$ROOT/Resources/AppIcon.icns" ] && cp "$ROOT/Resources/AppIcon.icns" "$APP/Contents/Resources/AppIcon.icns"

# MarkdownUI bundles resources next to the binary; copy any .bundle dirs in.
for b in "$BIN_DIR"/*.bundle; do
    [ -e "$b" ] && cp -R "$b" "$APP/Contents/Resources/" || true
done

# Sparkle.framework — required for auto-update.
SPARKLE_FRAMEWORK="$BIN_DIR/Sparkle.framework"
if [ -d "$SPARKLE_FRAMEWORK" ]; then
    echo "▸ Bundling Sparkle.framework"
    cp -R "$SPARKLE_FRAMEWORK" "$APP/Contents/Frameworks/"
else
    echo "  ⚠️  Sparkle.framework not found at $SPARKLE_FRAMEWORK" >&2
fi

# Inject version. Prefer explicit --version / APP_VERSION; else derive from the
# latest macapp-v* git tag; else leave the Info.plist value untouched.
if [ -z "$VERSION" ]; then
    GIT_TAG="$(git -C "$ROOT" describe --tags --abbrev=0 --match 'macapp-v*' 2>/dev/null || true)"
    VERSION="${GIT_TAG#macapp-v}"
fi
if [ -n "$VERSION" ]; then
    BUILD_NUMBER="$(git -C "$ROOT" rev-list --count HEAD 2>/dev/null || echo 1)"
    echo "▸ Setting version $VERSION (build $BUILD_NUMBER)"
    /usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString ${VERSION}" "$APP/Contents/Info.plist"
    /usr/libexec/PlistBuddy -c "Set :CFBundleVersion ${BUILD_NUMBER}" "$APP/Contents/Info.plist" 2>/dev/null \
        || /usr/libexec/PlistBuddy -c "Add :CFBundleVersion string ${BUILD_NUMBER}" "$APP/Contents/Info.plist"
fi

if [ -n "$SIGN_ID" ]; then
    echo "▸ Code-signing with: $SIGN_ID"
    xattr -cr "$APP"

    SPARKLE_ENTITLEMENTS="$ROOT/Resources/sparkle-entitlements.plist"
    FW="$APP/Contents/Frameworks/Sparkle.framework"
    if [ -d "$FW" ]; then
        VER_NAME="$(readlink "$FW/Versions/Current" 2>/dev/null || echo B)"
        VDIR="$FW/Versions/$VER_NAME"
        # Sign deepest → shallowest: XPC services, Autoupdate, Updater.app,
        # then the framework bundle itself.
        for xpc in "$VDIR/XPCServices"/*.xpc; do
            [ -d "$xpc" ] && codesign --force --options runtime --timestamp \
                --entitlements "$SPARKLE_ENTITLEMENTS" --sign "$SIGN_ID" "$xpc"
        done
        [ -e "$VDIR/Autoupdate" ] && codesign --force --options runtime --timestamp \
            --entitlements "$SPARKLE_ENTITLEMENTS" --sign "$SIGN_ID" "$VDIR/Autoupdate"
        [ -d "$VDIR/Updater.app" ] && codesign --force --options runtime --timestamp \
            --entitlements "$SPARKLE_ENTITLEMENTS" --sign "$SIGN_ID" "$VDIR/Updater.app"
        codesign --force --options runtime --timestamp --sign "$SIGN_ID" "$FW"
    fi

    # Main executable, then the whole bundle (no --deep: we signed nested parts).
    codesign --force --options runtime --timestamp --sign "$SIGN_ID" "$APP/Contents/MacOS/SkillsRegistry"
    codesign --force --options runtime --timestamp --sign "$SIGN_ID" "$APP"
    codesign --verify --deep --strict --verbose=2 "$APP"
else
    echo "▸ Ad-hoc signing (unsigned distribution)…"
    codesign --force --deep --sign - "$APP" || echo "  (ad-hoc sign skipped)"
fi

if [ "$NOTARIZE" = "1" ]; then
    : "${APPLE_ID:?APPLE_ID required for notarization}"
    : "${APPLE_TEAM_ID:?APPLE_TEAM_ID required for notarization}"
    : "${APPLE_APP_SPECIFIC_PASSWORD:?APPLE_APP_SPECIFIC_PASSWORD required for notarization}"
    ZIP="$ROOT/build/SkillsRegistry-notarize.zip"
    echo "▸ Submitting to Apple notary service…"
    /usr/bin/ditto -c -k --keepParent "$APP" "$ZIP"
    xcrun notarytool submit "$ZIP" \
        --apple-id "$APPLE_ID" --team-id "$APPLE_TEAM_ID" --password "$APPLE_APP_SPECIFIC_PASSWORD" --wait
    echo "▸ Stapling ticket…"
    xcrun stapler staple "$APP"
    xcrun stapler validate "$APP"
    rm -f "$ZIP"
fi

echo "✓ Built $APP"
echo "  open \"$APP\""
