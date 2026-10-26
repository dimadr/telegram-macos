#!/bin/bash
set -euo pipefail

APP=".mieru-work/DerivedData/Build/Products/Debug/Telegram.app"
EXECUTABLE="$APP/Contents/MacOS/Telegram"
test -d "$APP"
test -x "$EXECUTABLE"
test "$(lipo -archs "$EXECUTABLE")" == arm64
file "$EXECUTABLE"
otool -L "$EXECUTABLE"
nm -g "$EXECUTABLE" > .mieru-work/logs/app-symbols.log
for symbol in MieruClientNew MieruConnectionDial MieruConnectionRead MieruConnectionWrite mt_engine_require_stream_host; do
    grep -E " _${symbol}$" .mieru-work/logs/app-symbols.log
done
codesign -dv --verbose=2 "$APP" > .mieru-work/logs/codesign.log 2>&1 || true
/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$APP/Contents/Info.plist"
test -f "$APP/Contents/Info.plist"
test -d "$APP/Contents/Resources"
if find "$APP/Contents" -type f \( -name mieru -o -name mieru.exe -o -name libmieru.dylib -o -name libgo.dylib \) -print | grep .; then
    echo "Unexpected external Mieru runtime in app bundle" >&2
    exit 1
fi
mkdir -p .mieru-work/delivery
mv "$APP" .mieru-work/delivery/Telegramita.app
ditto -c -k --sequesterRsrc --keepParent .mieru-work/delivery/Telegramita.app .mieru-work/Telegramita-macOS-arm64-unsigned.zip
shasum -a 256 .mieru-work/Telegramita-macOS-arm64-unsigned.zip > .mieru-work/Telegramita-macOS-arm64-unsigned.sha256
node scripts/mieru-artifact.cjs seal .github/mieru-artifact-public.pem .mieru-work/Telegramita-macOS-arm64-unsigned.zip .mieru-work/Telegramita-macOS-arm64-unsigned.zip.enc
rm .mieru-work/Telegramita-macOS-arm64-unsigned.zip
