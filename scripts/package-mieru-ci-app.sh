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
ditto -c -k --sequesterRsrc --keepParent "$APP" .mieru-work/Telegramita-macOS-arm64-unsigned.zip
shasum -a 256 .mieru-work/Telegramita-macOS-arm64-unsigned.zip > .mieru-work/Telegramita-macOS-arm64-unsigned.sha256
