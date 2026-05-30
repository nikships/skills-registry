#!/usr/bin/env bash
# Generate AppIcon.icns from a drawn-on-the-fly PNG (no external assets).
# Pure macOS tooling: sips + iconutil + a tiny Swift drawing program.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"
OUT="${1:-$ROOT/Resources/AppIcon.icns}"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# Draw a 1024x1024 master PNG: hot-pink rounded square + stacked layers glyph.
cat > "$WORK/draw.swift" <<'SWIFT'
import AppKit

let size = 1024
let img = NSImage(size: NSSize(width: size, height: size))
img.lockFocus()
let ctx = NSGraphicsContext.current!.cgContext

// Background: near-black.
ctx.setFillColor(NSColor(red: 0, green: 0, blue: 0, alpha: 1).cgColor)
ctx.fill(CGRect(x: 0, y: 0, width: size, height: size))

// Rounded pink tile.
let inset: CGFloat = 150
let rect = CGRect(x: inset, y: inset, width: CGFloat(size) - inset*2, height: CGFloat(size) - inset*2)
let path = CGPath(roundedRect: rect, cornerWidth: 150, cornerHeight: 150, transform: nil)
ctx.addPath(path)
ctx.setFillColor(NSColor(red: 1.0, green: 0.302, blue: 0.553, alpha: 1).cgColor)
ctx.fillPath()

// Three stacked diamonds (layers) in cream.
ctx.setFillColor(NSColor(red: 0.961, green: 0.953, blue: 0.933, alpha: 1).cgColor)
let cx = CGFloat(size)/2
func diamond(cy: CGFloat, w: CGFloat, h: CGFloat) {
    ctx.beginPath()
    ctx.move(to: CGPoint(x: cx, y: cy + h))
    ctx.addLine(to: CGPoint(x: cx + w, y: cy))
    ctx.addLine(to: CGPoint(x: cx, y: cy - h))
    ctx.addLine(to: CGPoint(x: cx - w, y: cy))
    ctx.closePath()
    ctx.fillPath()
}
diamond(cy: 640, w: 210, h: 120)
ctx.setFillColor(NSColor(red: 0.961, green: 0.953, blue: 0.933, alpha: 0.75).cgColor)
diamond(cy: 470, w: 210, h: 120)
ctx.setFillColor(NSColor(red: 0.961, green: 0.953, blue: 0.933, alpha: 0.5).cgColor)
diamond(cy: 300, w: 210, h: 120)

img.unlockFocus()

guard let tiff = img.tiffRepresentation,
      let rep = NSBitmapImageRep(data: tiff),
      let png = rep.representation(using: .png, properties: [:]) else {
    fatalError("encode failed")
}
try! png.write(to: URL(fileURLWithPath: CommandLine.arguments[1]))
SWIFT

swift "$WORK/draw.swift" "$WORK/icon_1024.png"

ICONSET="$WORK/AppIcon.iconset"
mkdir -p "$ICONSET"
for s in 16 32 64 128 256 512 1024; do
    sips -z "$s" "$s" "$WORK/icon_1024.png" --out "$ICONSET/icon_${s}x${s}.png" >/dev/null
done
# Retina (@2x) variants.
cp "$ICONSET/icon_32x32.png"   "$ICONSET/icon_16x16@2x.png"
cp "$ICONSET/icon_64x64.png"   "$ICONSET/icon_32x32@2x.png"
cp "$ICONSET/icon_256x256.png" "$ICONSET/icon_128x128@2x.png"
cp "$ICONSET/icon_512x512.png" "$ICONSET/icon_256x256@2x.png"
cp "$ICONSET/icon_1024x1024.png" "$ICONSET/icon_512x512@2x.png"

iconutil -c icns "$ICONSET" -o "$OUT"
echo "Wrote $OUT"
