#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")"
MODFILE="$(go run ./cmd/prepare)"
TEST_RACE_FLAG=""
if [[ "${MIERU_TEST_RACE:-0}" == 1 ]]; then TEST_RACE_FLAG="-race"; fi
go test $TEST_RACE_FLAG -modfile="$MODFILE" -mod=readonly -count=1 -timeout=30s ./bridge ./cmd/prepare
go test $TEST_RACE_FLAG -modfile="$MODFILE" -mod=readonly -count=1 -timeout=30s github.com/enfein/mieru/v3/apis/client -run '^TestTelegramita'
