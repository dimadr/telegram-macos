#!/bin/bash
set -euo pipefail
APP="${1:-.mieru-work/DerivedData/Build/Products/Debug/Telegram.app}"
test -d "$APP/Contents/MacOS"
if [[ -d "$APP/Contents/PlugIns" ]]; then
    rm -rf "$APP/Contents/PlugIns"
fi
find "$APP" -name embedded.provisionprofile -type f -delete
while IFS= read -r -d '' image; do
    if file -b "$image" | grep -q 'Mach-O'; then
        codesign --force --sign - --timestamp=none "$image"
    fi
done < <(find "$APP" -type f -print0)
while IFS= read -r -d '' bundle; do
    [[ "$bundle" == "$APP" ]] && continue
    codesign --force --sign - --timestamp=none "$bundle"
done < <(find "$APP" -depth -type d \( -name '*.framework' -o -name '*.xpc' -o -name '*.app' \) -print0)
codesign --force --sign - --timestamp=none --identifier org.dimadr.Telegramita "$APP"
codesign --verify --deep --strict --verbose=2 "$APP"
codesign --display --entitlements :- "$APP" > .mieru-work/logs/local-entitlements.plist 2> .mieru-work/logs/local-codesign.log
test -f "$APP/Contents/_CodeSignature/CodeResources"
if grep -E '6N38VWS5BX|ru.keepcoder.Telegram|application-groups|keychain-access-groups' .mieru-work/logs/local-entitlements.plist; then
    echo 'Unexpected certificate-dependent entitlement' >&2
    exit 1
fi
