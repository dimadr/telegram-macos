#!/bin/bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
IOS="$ROOT/submodules/telegram-ios"
RUST="$IOS/third-party/mtproto-engine"

check_revision() {
    local directory="$1" revision="$2"
    [[ -e "$directory/.git" ]] || { echo "Initialise submodule: $directory" >&2; exit 1; }
    [[ "$(git -C "$directory" rev-parse HEAD)" == "$revision" ]] || {
        echo "Mieru patches require $revision at $directory" >&2
        exit 1
    }
}

patch_state() {
    if git -C "$1" apply --reverse --check "$2" 2>/dev/null; then
        echo applied
    elif git -C "$1" apply --check "$2" 2>/dev/null; then
        echo pending
    else
        echo "Mieru patch conflicts with local changes: $2" >&2
        exit 1
    fi
}

check_revision "$IOS" fb0e62ba8a147f0821da6aa63c09da7d79a10111
check_revision "$RUST" da4b2170210aab0430c4ff18c032991f3030cf45
IOS_PATCH="$ROOT/patches/mieru/telegram-ios.patch"
RUST_PATCH="$ROOT/patches/mieru/mtproto-engine.patch"
IOS_STATE="$(patch_state "$IOS" "$IOS_PATCH")"
RUST_STATE="$(patch_state "$RUST" "$RUST_PATCH")"
if [[ "$IOS_STATE" == pending ]]; then git -C "$IOS" apply "$IOS_PATCH"; fi
if [[ "$RUST_STATE" == pending ]]; then git -C "$RUST" apply "$RUST_PATCH"; fi
echo "Mieru submodule patches ready"
