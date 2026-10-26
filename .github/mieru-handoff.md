# Telegramita Mieru continuation

## State

- Repository: `dimadr/telegram-macos`; branch `codex/mieru-client-api`; Draft PR #1.
- Last tested implementation HEAD: `499204963f090387c46fcfd4438f9bfd12e32fc3`. The handoff commit adds cache output patterns only, without changing app code.
- CI #5: https://github.com/dimadr/telegram-macos/actions/runs/40335099051
- Job: `121614418125`. Completed with build-budget exit 124. No compiler errors were found. Compilation reached UI packages including StarsUI, StickerSettingsUI and AffiliateUI; TelegramShare.appex was produced, but the main app was not completed.
- Go/race, c-archive/C ABI and Swift transport passed on Xcode 26.3. Main app linkage, startup and packaging remain NOT TESTED; no app artifact exists.
- Do not manually rerun the same SHA. Poll active CI at most once per five minutes.

## Authorization and limits

- Autonomous fixes, commits and pushes are authorized only on this branch; continue Draft PR #1.
- Additional macOS CI budget for the autonomous phase: 180 minutes; 45 minutes maximum per job.
- Completed autonomous runs used 97 minutes, rounded conservatively: CI #3 42 min, CI #4 13 min, CI #5 42 min. Remaining budget: 83 minutes. Previous CI #1/#2 predate this budget.
- No master/other branch changes, merge, force-push, releases, user-data deletion or secret/certificate publication.
- Stop at 50% context usage, safely save work and hand off. Do not start another CI after reaching that threshold. Leave an already running job alone.
- Windows checkout, caches, artifacts and temporary files remain on D:/E:; existing workspace is `E:\Telegram_macOS`.

## Resolved blockers; do not re-investigate

- Inaccessible root `twidget` and `widget-sandbox` are excluded. Nine public root submodules and four required telegram-ios submodules are selected explicitly.
- Initialize four direct tg_owt dependencies (`crc32c`, `abseil-cpp`, `libsrtp`, `libyuv`) and Sparkle `ed25519`. Critical dependency files are checked before compilation. Do not create placeholders.
- All native frameworks compiled successfully on CI #3. CI #4 confirmed their cache reuse.
- CI #3 reached Postbox compilation and hit the 40-minute build budget, not a compiler error.
- CI #4 failed because default Xcode 16.4 lacks NSGlassEffectView/NSGlassEffectContainerView.
- Current workflow uses Xcode 26.3 for Swift/app, and Xcode 16.4 for validated C/Objective-C/Rust dependencies. Native cache fingerprints remain honest; no SDK version is spoofed. Swift caches are separated by app Xcode version.
- Compiler indexing is disabled; CI reserves five minutes for logs/artifacts/caches. Cache restore/save runs preserve work after failures. Plaintext app bundles are excluded from caches.
- CI #5 confirmed native reuse and saved `mieru-swift-macOS-ARM64-26.3-499204963f090387c46fcfd4438f9bfd12e32fc3`. This handoff also adds ready product `.o`/Swift module outputs to restore/save. This cache-only change passed actionlint but has not run on macOS. Do not cache an entire plaintext `.app`.

## Artifacts and secrets

- GPLv2/GPLv3 compatibility remains unresolved. Do not publish a plaintext app publicly.
- Successful packaging produces `Telegramita-macOS-arm64-encrypted`: authenticated AES-256-GCM ciphertext and the plaintext ZIP SHA-256. Public RSA recipient key is `.github/mieru-artifact-public.pem`.
- Private key exists only locally: `.mieru-work/delivery/recipient-private.pem` (ignored by git, restricted ACL). Never commit, print or upload it.
- Decrypt locally on E: with `node scripts/mieru-artifact.cjs open PRIVATE_KEY INPUT.zip.enc OUTPUT.zip`; verify the SHA-256 against the artifact before handing the ZIP to the user.
- GitHub connector write calls return 403. Git push works through configured credentials. Ignored local helpers `.mieru-work/github-api.ps1` and `github-download.ps1` use those credentials without printing them; downloads go to E:.

## Local records

- `.mieru-work/ci-budget.json`: active/completed autonomous runs and remaining minutes.
- `.mieru-work/run3-logs`, `run4-logs`, `run5-logs`: complete downloaded diagnostics.
- CI #3 logs artifact `12653294411`; CI #4 logs artifact `12655981508`.
- CI #5 logs: https://github.com/dimadr/telegram-macos/actions/runs/40335099051/artifacts/12659206974 .
- The artifact encryption helper passed roundtrip and tamper rejection; YAML/actionlint and shell syntax passed before pushes.

## Next operation

Continuation after CI #5: the expanded cache path list changes the Actions cache version, so prefix keys alone cannot restore the old cache. The workflow now falls back to the exact CI #5 key with its original three paths, failing cheaply if neither cache exists. Expanded saves add headers, static archives, interfaces and ABI metadata. Xcode compilation caching is enabled with an explicit cached CAS directory and diagnostic remarks to validate reuse despite fresh checkout timestamps, without changing file timestamps or fabricating dependency records. Before/after inventory reports are uploaded with diagnostics. Actionlint, Node syntax/inventory and Bash syntax checks passed; effectiveness remains untested until the next run. Native paths/toolchain and the 45-minute limit are unchanged.

1. Resume in a new session from this handoff. CI #5 analysis is complete: the remaining blocker is cold full-app Swift compilation exceeding the build time budget, without compiler errors. Do not repeat dependency/SDK research.
2. Validate/resume using the saved Xcode 26.3 Swift/native caches and the expanded product output patterns. If unchanged sources rebuild again, inspect source timestamps/cache completeness before spending another full run. A changed SHA is required before new CI; the handoff was pushed with `[skip ci]` deliberately to stop this session without a new run. Remaining budget is 83 minutes.
3. If the app builds, inspect linker/bundle/architecture/startup reports, retrieve the encrypted artifact, decrypt on E: and verify its SHA-256.
4. One final code audit is still pending after compilation blockers are removed. Do not repeat the previous dependency investigation.
5. No real Mieru server or Telegram test account was supplied. Authorization, messages, files/CDN, reconnection, observed fail-closed behavior, both engines at runtime and leak checks remain NOT TESTED until exercised. Do not collect real user credentials.
