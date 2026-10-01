#!/bin/sh
# Build the macOS tray .app bundle: a universal binary wrapped in the
# structure pkgbuild can install into /Applications.

set -e
cd "$(dirname "$0")/../.."

VERSION="${VERSION:-0.0.0}"
APP=build/ThaiSmartcardTray.app

rm -rf build
mkdir -p "$APP/Contents/MacOS"

# Universal: both architectures, lipo-ed. cgo compiles against the runner's
# Xcode for each arch (decision 20).
CGO_ENABLED=1 GOARCH=amd64 go build -trimpath -ldflags "-s -w -X main.version=$VERSION" \
    -o build/tray.amd64 ./cmd/tray
CGO_ENABLED=1 GOARCH=arm64 go build -trimpath -ldflags "-s -w -X main.version=$VERSION" \
    -o build/tray.arm64 ./cmd/tray
lipo -create -output "$APP/Contents/MacOS/thai-smartcard-tray" build/tray.amd64 build/tray.arm64

cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleExecutable</key>
    <string>thai-smartcard-tray</string>
    <key>CFBundleIdentifier</key>
    <string>com.thaismartcard.tray</string>
    <key>CFBundleName</key>
    <string>ThaiSmartcardTray</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleShortVersionString</key>
    <string>$VERSION</string>
    <key>CFBundleVersion</key>
    <string>$VERSION</string>
    <key>LSMinimumSystemVersion</key>
    <string>11.0</string>
    <key>LSUIElement</key>
    <true/>
</dict>
</plist>
PLIST

echo "built $APP"
