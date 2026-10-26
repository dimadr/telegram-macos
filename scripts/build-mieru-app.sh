#!/bin/bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
[[ "$(uname -s)" == Darwin && "$(uname -m)" == arm64 ]] || {
    echo "Build the app on an Apple Silicon Mac with Xcode" >&2
    exit 1
}
mkdir -p .mieru-work/logs
bash scripts/apply-mieru-patches.sh
if [[ "$(xcodebuild -version | head -1 | awk '{print $2}' | cut -d. -f1)" -ge 27 ]]; then
    bash scripts/xcode27.sh apply
fi
bash scripts/configure_frameworks.sh >.mieru-work/logs/frameworks.log 2>&1
xcodebuild -workspace Telegram-Mac.xcworkspace -scheme Telegram -configuration Debug \
    -arch arm64 -derivedDataPath .mieru-work/DerivedData \
    MACOSX_DEPLOYMENT_TARGET=12.0 CODE_SIGNING_ALLOWED=NO build \
    >.mieru-work/logs/xcodebuild.log 2>&1
echo "$ROOT/.mieru-work/DerivedData/Build/Products/Debug/Telegram.app"
