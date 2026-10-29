#!/bin/bash
set -euo pipefail

arguments=()
extract=no
for argument in "$@"; do
    case "$argument" in
        --delay-directory-restore) continue ;;
        -xf|--extract) extract=yes ;;
    esac
    arguments+=("$argument")
done
if [[ "$extract" == yes ]]; then
    exec "${MIERU_SPARSE_TAR:-$PWD/.mieru-work/cache-tools/mieru-sparse-tar}" "${arguments[@]}"
fi
exec "${MIERU_CACHE_BSDTAR:-/usr/bin/tar}" "${arguments[@]}"
