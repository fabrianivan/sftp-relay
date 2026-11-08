#!/usr/bin/env bash
# Builds "SFTP Relay.app" and a drag-to-Applications DMG in dist/.
#   scripts/build-macos.sh            # version from the latest git tag
#   VERSION=2.1.0 scripts/build-macos.sh
set -euo pipefail
cd "$(dirname "$0")/.."

APP="SFTP Relay"
VERSION="${VERSION:-$(git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//' || true)}"
VERSION="${VERSION:-2.0.0}"
FYNE="$(go env GOPATH)/bin/fyne"

if [ ! -x "$FYNE" ]; then
  echo "[*] Installing the fyne packaging tool"
  go install fyne.io/tools/cmd/fyne@latest
fi

mkdir -p dist
rm -rf "$APP.app" dist/dmg_root "dist/SFTP_Relay_$VERSION.dmg"

echo "[*] Packaging $APP $VERSION"
"$FYNE" package --os darwin --app-version "$VERSION" --icon assets/logo.png

mkdir -p dist/dmg_root
mv "$APP.app" dist/dmg_root/
ln -s /Applications dist/dmg_root/Applications
hdiutil create -volname "$APP" -srcfolder dist/dmg_root -ov -format UDZO "dist/SFTP_Relay_$VERSION.dmg" >/dev/null
rm -rf dist/dmg_root

echo "[✓] dist/SFTP_Relay_$VERSION.dmg"
