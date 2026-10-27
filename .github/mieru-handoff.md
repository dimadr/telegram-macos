# Telegramita Mieru continuation

## State

- CI #9 https://github.com/dimadr/telegram-macos/actions/runs/40468613184 on `f8821b9d85ba64444398f982ca4593fc79fa02e2` also stopped at preflight: seek/truncate on the workspace filesystem reported full allocation (16 MiB). No large cache restore or app build ran. The next fix mounts a dynamically allocated 100 GiB APFS sparse disk image at the existing CAS directory, and performs the same allocation preflight on that volume. Only real written blocks should consume host disk; this must be confirmed by preflight. Image itself is not cached. Source paths, cache paths and compiler flags are unchanged. Do not remove the allocated-block assertion to hide failure.

- CI #8 https://github.com/dimadr/telegram-macos/actions/runs/40467555141 on `161b53b80e08984e237e07a98a9d54222a05be29` stopped cheaply at sparse preflight: Apple's BSD tar `-S` still allocated all 16 MiB. The large cache was not restored. Diagnostics artifact: https://github.com/dimadr/telegram-macos/actions/runs/40467555141/artifacts/12717032897 . The wrapper now delegates extraction to a small standard-library Go tar reader that explicitly seeks over zero blocks, truncates to the exact logical size, restores archive modes/mtimes and bounds entries to the workspace. Creation still uses system tar; original cache paths and compiler flags remain unchanged. Local Go compile and byte-roundtrip PASS. The unchanged macOS preflight must verify actual allocated blocks before any large restore; effectiveness remains NOT TESTED until that passes.

- Authorization update 2026-10-27: the user removed both the remaining aggregate CI limit and the 45-minute per-job limit. The workflow uses a 120-minute operational timeout and a 115-minute build deadline, with five minutes for diagnostics/cache preservation. The 50% context stop remains mandatory; leave any already running CI alone at that stop. A cheap macOS sparse-extraction preflight now verifies both bytes and allocated blocks before restoring the large cache; do not restore it if preflight fails.

- Latest outcome 2026-10-27: CI #7 https://github.com/dimadr/telegram-macos/actions/runs/40466230308 , job `122062181408`, source `3fc799fc0b76ecd77c51705a83d312577690cb3c`, failed 07:17:29–07:22:31 UTC during expanded Swift cache restore. Exact runner annotation: `System.IO.IOException: No space left on device`. No app compilation occurred. Finalization also failed; job-log download returned BlobNotFound, so there is no new diagnostics artifact. Annotations are saved locally in `.mieru-work/run7-annotations.json` and remain on GitHub.
- Remaining budget is now **33 minutes** (147/180 consumed, including conservatively rounded 6 minutes for CI #7). No active job. No additional CI was started. Current workflow is capped at 33 minutes, internal build work at 28 minutes, reserving five minutes for diagnostics/caches.
- Pending fix: the expanded CAS inventory showed about 68 GB logical data despite its 2.74 GB compressed archive. Sparse/zero-filled CAS files can exhaust the runner when extracted densely. The workflow scopes a BSD tar wrapper to Swift cache restore/save; extraction uses `-S` to retain zero blocks as holes, while creation preserves sparse information. Cache paths and app compiler flags stay unchanged so the existing warm cache remains eligible. The wrapper removes Actions' GNU-only `--delay-directory-restore` when forwarding to BSD tar. Before/after inventory now records `allocatedBytes` as well as logical bytes.
- Validation of pending fix: actionlint, Bash syntax, Node syntax, whitespace checks PASS. A local 16 MiB zero-filled archive with nonzero marker survived wrapper extraction byte-for-byte. Actual macOS sparse allocation, full cache restore, CAS hits and completed app remain NOT TESTED. Do not mistake the Windows byte-roundtrip for proof of macOS disk-space savings. The fix is pushed with `[skip ci]` and does not trigger another run.
- Next step, if continued: verify on macOS that BSD `tar -S` creates sparse outputs and the expanded cache fits disk, then reuse the saved warm cache within the remaining 33 minutes. If it still does not fit, stop before compilation and preserve diagnostics. No repeat architecture/dependency audit is needed. No completed Telegramita.app exists; GPLv2/GPLv3 remains open.

- Update 2026-10-27: user-triggered CI #6 attempt 3 (job `122042345000`) ran on `5f4983e897f3b4544359a9e80c9383e0409af5ee`, 06:16:12–06:59:43 UTC (43:31, charged conservatively as 44 minutes). Go/race, c-archive/C ABI, Swift transport and native reuse PASS. Full app reached GroupCallScreenUI and hit the 40-minute internal deadline (exit 124); no compiler diagnostics classified as errors. Main linkage/startup/package remain NOT TESTED. Diagnostics: https://github.com/dimadr/telegram-macos/actions/runs/40354269306/artifacts/12715621788 .
- Cache compatibility restore worked. CI #5 had 3076 intermediate objects but no restored Products objects/modules. Attempt 3 saved 150 Products objects, 145 Products Swift modules and compilation CAS (1101 cold misses, no hits). The new complete cache `mieru-swift-macOS-ARM64-26.3-5f4983e897f3b4544359a9e80c9383e0409af5ee` exists on `refs/pull/1/merge`, compressed size 2740869058 bytes. Do not change cache paths or app build flags before using it. Node 20 deprecation warning was not the failure cause.
- Budget now: 141 of 180 minutes spent; 39 remain. Next workflow limit is 39 minutes, internal build deadline 34 minutes, leaving five minutes for diagnostics/cache preservation. No increase beyond authorized budget. Earlier state below describes historical attempts.

- Repository: `dimadr/telegram-macos`; branch `codex/mieru-client-api`; Draft PR #1.
- Latest build/cache change: `5f4983e897f3b4544359a9e80c9383e0409af5ee`. CI #6: https://github.com/dimadr/telegram-macos/actions/runs/40354269306 . Both attempt 1 (job `121682373641`) and the user-authorized manual attempt 2 (job `121692176608`) failed to acquire a hosted arm64 runner. Attempt 2 ended at 2026-10-26 16:28:49 UTC. No workflow steps ran; runner ID was 0; no compilation or cache effectiveness checks occurred and no artifacts were created. Exact annotation: `The job was not acquired by Runner of type hosted even after multiple attempts`. GitHub also reported macOS arm64 capacity constraints. This is an infrastructure failure, not a source/compiler failure. No third attempt was started.
- Last tested implementation HEAD: `499204963f090387c46fcfd4438f9bfd12e32fc3`. The handoff commit adds cache output patterns only, without changing app code.
- CI #5: https://github.com/dimadr/telegram-macos/actions/runs/40335099051
- Job: `121614418125`. Completed with build-budget exit 124. No compiler errors were found. Compilation reached UI packages including StarsUI, StickerSettingsUI and AffiliateUI; TelegramShare.appex was produced, but the main app was not completed.
- Go/race, c-archive/C ABI and Swift transport passed on Xcode 26.3. Main app linkage, startup and packaging remain NOT TESTED; no app artifact exists.
- Do not manually rerun the same SHA. Poll active CI at most once per five minutes.

## Authorization and limits

- Autonomous fixes, commits and pushes are authorized only on this branch; continue Draft PR #1.
- Additional macOS CI budget for the autonomous phase: 180 minutes; 45 minutes maximum per job.
- Completed autonomous runs used 97 minutes, rounded conservatively: CI #3 42 min, CI #4 13 min, CI #5 42 min. Remaining budget: 83 minutes. Previous CI #1/#2 predate this budget.
- CI #6 attempts consumed 0 runner execution minutes (queue only, no assigned runner). Remaining budget is still 83 minutes; no job is active.
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
2. Resume only when macOS arm64 runner capacity is available. CI #6 has not tested the cache changes at all; do not alter source code to disguise the infrastructure failure. Its one manual retry was explicitly authorized by the user. After capacity recovers, obtain authorization for another identical-SHA retry if needed. Restore the saved Xcode 26.3 Swift/native caches using the compatibility fallback and expanded output patterns. Inspect the new `swift-cache-restored.json`, `swift-cache-final.json` and compilation-cache remarks before spending further runs. Do not fabricate timestamps or build records. This handoff is committed with `[skip ci]`, without starting another run. Remaining budget is 83 minutes.
3. If the app builds, inspect linker/bundle/architecture/startup reports, retrieve the encrypted artifact, decrypt on E: and verify its SHA-256.
4. One final code audit is still pending after compilation blockers are removed. Do not repeat the previous dependency investigation.
5. No real Mieru server or Telegram test account was supplied. Authorization, messages, files/CDN, reconnection, observed fail-closed behavior, both engines at runtime and leak checks remain NOT TESTED until exercised. Do not collect real user credentials.
