#!/bin/bash
set -euo pipefail

[[ "$(uname -s)" == Darwin && "$(uname -m)" == arm64 ]]
xcodebuild -version
sw_vers
df -h .
git config --global url."https://github.com/".insteadOf git@github.com:
git config --global url."https://gitlab.com/".insteadOf git@gitlab.com:
git submodule update --init --depth=1 \
    submodules/libtgvoip submodules/Sparkle submodules/telegram-ios \
    submodules/rlottie submodules/tgcalls submodules/tg_owt \
    submodules/libprisma submodules/regex submodules/tlottie
git -C submodules/tg_owt submodule update --init --depth=1 \
    src/third_party/crc32c/src src/third_party/abseil-cpp \
    src/third_party/libsrtp src/third_party/libyuv
git -C submodules/Sparkle submodule update --init --depth=1 ed25519
git -C submodules/telegram-ios submodule update --init --depth=1 \
    third-party/dav1d/dav1d third-party/td/td \
    third-party/wallet-engine/wallet-engine third-party/mtproto-engine
bash scripts/apply-mieru-patches.sh

for required in \
    submodules/tg_owt/src/third_party/crc32c/src/src/crc32c_config.h.in \
    submodules/tg_owt/src/third_party/abseil-cpp/absl/base/attributes.h \
    submodules/tg_owt/src/third_party/libsrtp/include/srtp.h \
    submodules/tg_owt/src/third_party/libyuv/include/libyuv.h \
    submodules/Sparkle/ed25519/src/verify.c \
    submodules/telegram-ios/third-party/dav1d/dav1d/meson.build \
    submodules/telegram-ios/third-party/td/td/CMakeLists.txt \
    submodules/telegram-ios/third-party/wallet-engine/wallet-engine/Cargo.toml \
    submodules/telegram-ios/third-party/mtproto-engine/Cargo.toml; do
    test -s "$required" || { echo "Missing build dependency: $required" >&2; exit 1; }
done

export HOMEBREW_NO_AUTO_UPDATE=1
brew install cmake ninja meson nasm yasm zlib autoconf libtool automake pkg-config
if ! arch -x86_64 /usr/bin/true >/dev/null 2>&1; then
    softwareupdate --install-rosetta --agree-to-license
fi

python3 - <<'PY'
from pathlib import Path
path = Path("submodules/tg_owt/src/third_party/abseil-cpp/absl/base/attributes.h")
if path.exists():
    source = path.read_text()
    source = source.replace("#define ABSL_ATTRIBUTE_LIFETIME_BOUND [[clang::lifetimebound]]", "#define ABSL_ATTRIBUTE_LIFETIME_BOUND")
    source = source.replace("#define ABSL_ATTRIBUTE_LIFETIME_BOUND __attribute__((lifetimebound))", "#define ABSL_ATTRIBUTE_LIFETIME_BOUND")
    path.write_text(source)
PY
