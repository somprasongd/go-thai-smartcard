#!/bin/sh
# Build the macOS installer package with pkgbuild/productbuild, which ship
# with macOS (decision 20).
#
#   VERSION=3.3.0 packaging/macos/build-pkg.sh
#
# The tray must live in a signed, notarized .app before it reaches users; run
# codesign/notarytool on the .app and the .pkg when the Apple Developer
# credentials are available. The CI workflow does this when its secrets are
# set.

set -e
cd "$(dirname "$0")/../.."

VERSION="${VERSION:-0.0.0}"

packaging/macos/make-app.sh

rm -rf build/pkgroot
mkdir -p build/pkgroot/usr/local/bin build/pkgroot/Applications

go build -trimpath -ldflags "-s -w -X main.version=$VERSION" \
    -o build/pkgroot/usr/local/bin/thai-smartcard-agent ./cmd/agent
cp -R build/ThaiSmartcardTray.app build/pkgroot/Applications/

pkgbuild \
    --root build/pkgroot \
    --scripts packaging/macos/scripts \
    --identifier com.thaismartcard.agent \
    --version "$VERSION" \
    --install-location / \
    build/thai-smartcard-agent-$VERSION.pkg

echo "built build/thai-smartcard-agent-$VERSION.pkg"
