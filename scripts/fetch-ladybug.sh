#!/usr/bin/env bash
# Fetch the pinned LadybugDB native library.
#
# The version, the URL and the digest come from docs/dependencies/ladybugdb.md.
# Nothing here resolves `latest`: a build that silently changes its database
# engine is not reproducible.
#
# Usage:
#   scripts/fetch-ladybug.sh [destination]
#
# The destination defaults to .tooling/ladybug/<version>. The script prints the
# library directory on stdout so callers can export CGO flags from it.

set -euo pipefail

# The core version must match the binding version: the C API changes shape
# between releases. Core v0.19.0 with binding v0.13.1 adds four fields to
# `lbug_system_config`, which the binding returns by value, and the first call
# segfaults. Verified on 2026-08-05.
readonly VERSION="v0.13.1"
readonly BASE_URL="https://github.com/LadybugDB/ladybug/releases/download/${VERSION}"

repository_root() {
    cd "$(dirname "${BASH_SOURCE[0]}")/.."
    pwd
}

asset_for_platform() {
    local system machine
    system="$(uname -s)"
    machine="$(uname -m)"
    case "${system}/${machine}" in
        Linux/x86_64)
            echo "liblbug-linux-x86_64.tar.gz ce6387bd46a5bcecbf7d59608694c540274b6bfb690ea8e5c92d9cb373d73439"
            ;;
        Linux/aarch64 | Linux/arm64)
            echo "liblbug-linux-aarch64.tar.gz f037a9e237cd0f9182b08fdabd73569b5afdcc3098e4d0b92687e678ce7736e4"
            ;;
        Darwin/arm64 | Darwin/x86_64)
            echo "liblbug-osx-universal.tar.gz 4195a05e42671e5f8d036c5d035617ca05d25a1813b2ed8b46ab6cf9d8f0c426"
            ;;
        *)
            echo "unsupported platform ${system}/${machine}" >&2
            return 1
            ;;
    esac
}

# locate_library prints the directory holding the shared object. The Linux and
# macOS assets extract it flat; other layouts nest it, so it is located instead
# of assumed.
locate_library() {
    local destination object
    destination="$1"
    object="$(find "${destination}" -maxdepth 3 -type f \
        \( -name 'liblbug.so*' -o -name 'liblbug.dylib' \) -print -quit)"
    if [[ -z "${object}" ]]; then
        return 1
    fi
    dirname "${object}"
}

# sha256_of prints the SHA-256 of a file. macOS ships shasum, not the coreutils
# sha256sum this repository uses elsewhere, and a missing checksum tool must
# stop the download rather than let an unverified library through.
sha256_of() {
    local file
    file="$1"
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "${file}" | cut -d' ' -f1
    elif command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "${file}" | cut -d' ' -f1
    else
        echo "no SHA-256 tool found: install coreutils or shasum" >&2
        return 1
    fi
}

main() {
    local root destination asset digest archive library
    root="$(repository_root)"
    destination="${1:-${root}/.tooling/ladybug/${VERSION}}"

    read -r asset digest <<<"$(asset_for_platform)"

    if [[ -f "${destination}/.verified" ]] &&
        [[ "$(cat "${destination}/.verified")" == "${digest}" ]] &&
        library="$(locate_library "${destination}")"; then
        echo "${library}"
        return 0
    fi

    mkdir -p "${destination}"
    archive="${destination}/${asset}"
    echo "fetching ${BASE_URL}/${asset}" >&2
    curl --fail --location --silent --show-error --output "${archive}" "${BASE_URL}/${asset}"

    local observed
    observed="$(sha256_of "${archive}")"
    if [[ "${observed}" != "${digest}" ]]; then
        rm -f "${archive}"
        echo "digest mismatch for ${asset}" >&2
        echo "  expected ${digest}" >&2
        echo "  observed ${observed}" >&2
        return 1
    fi

    tar --extract --file "${archive}" --directory "${destination}"
    rm -f "${archive}"

    if ! library="$(locate_library "${destination}")"; then
        echo "no liblbug shared object inside ${asset}" >&2
        return 1
    fi
    printf '%s' "${digest}" >"${destination}/.verified"
    echo "${library}"
}

main "$@"
