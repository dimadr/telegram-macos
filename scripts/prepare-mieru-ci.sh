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
git -C submodules/telegram-ios submodule update --init --depth=1 \
    third-party/dav1d/dav1d third-party/td/td \
    third-party/wallet-engine/wallet-engine third-party/mtproto-engine
bash scripts/apply-mieru-patches.sh

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
