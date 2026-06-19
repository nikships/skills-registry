#!/bin/sh
# install.sh — install the skills-registry Go CLI.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/nikships/skills-registry/main/install.sh | sh
#
# Detects the host OS and architecture, downloads the matching tarball
# from the GitHub Releases of nikships/skills-registry, drops the
# skills-registry binary into ~/.local/bin/, and marks it executable.
#
# Supported platforms (POSIX — macOS & Linux):
#   darwin/amd64, darwin/arm64, linux/amd64, linux/arm64
#
# Windows users should use install.ps1 for a native PowerShell install.
# Anything else exits cleanly with a clear error message and code 2 —
# never silently downloads the wrong asset.
#
# Environment overrides (mostly for testing / pinning):
#   SKILLS_REGISTRY_REPO     Override owner/repo (default: nikships/skills-registry)
#   SKILLS_REGISTRY_VERSION  Pin to a tag, e.g. v0.5.1 (default: latest)
#   SKILLS_BIN_DIR           Override install dir (default: $HOME/.local/bin)
#   SKILLS_REGISTRY_OS       Override detected OS (default: $(uname -s))
#   SKILLS_REGISTRY_ARCH     Override detected arch (default: $(uname -m))
#   SKILLS_REGISTRY_URL      Override the full tarball URL
#   SKILLS_REGISTRY_TARBALL  Use a local tarball file instead of downloading
#   SKILLS_REGISTRY_DRY_RUN  If non-empty, print resolved URL/dest and exit
#
# Exit codes:
#   0  success
#   1  generic / IO failure
#   2  unsupported OS/arch

set -eu

REPO=${SKILLS_REGISTRY_REPO:-nikships/skills-registry}
VERSION=${SKILLS_REGISTRY_VERSION:-latest}
BIN_DIR=${SKILLS_BIN_DIR:-$HOME/.local/bin}
BINARY=skills-registry

log()  { printf '%s\n' "$*" >&2; }
warn() { printf 'warning: %s\n' "$*" >&2; }
err()  { printf 'error: %s\n' "$*" >&2; }

detect_os() {
    raw=${SKILLS_REGISTRY_OS:-$(uname -s)}
    case $raw in
        Linux|linux)   printf 'linux'  ;;
        Darwin|darwin) printf 'darwin' ;;
        *)
            err "unsupported OS: $raw"
            err "supported operating systems: Linux, Darwin (macOS)"
            return 2
            ;;
    esac
}

detect_arch() {
    raw=${SKILLS_REGISTRY_ARCH:-$(uname -m)}
    case $raw in
        x86_64|amd64)  printf 'amd64' ;;
        arm64|aarch64) printf 'arm64' ;;
        *)
            err "unsupported architecture: $raw"
            err "supported architectures: x86_64/amd64, arm64/aarch64"
            return 2
            ;;
    esac
}

build_url() {
    os=$1
    arch=$2
    asset="skills-registry_${os}_${arch}.tar.gz"
    if [ "$VERSION" = "latest" ]; then
        printf 'https://github.com/%s/releases/latest/download/%s' "$REPO" "$asset"
    else
        printf 'https://github.com/%s/releases/download/%s/%s' "$REPO" "$VERSION" "$asset"
    fi
}

download_to() {
    url=$1
    out=$2
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL --retry 3 -o "$out" "$url"
    elif command -v wget >/dev/null 2>&1; then
        wget -q -O "$out" "$url"
    else
        err "need curl or wget to download $url"
        return 1
    fi
}

main() {
    os=$(detect_os) || exit $?
    arch=$(detect_arch) || exit $?
    url=${SKILLS_REGISTRY_URL:-$(build_url "$os" "$arch")}
    dest=$BIN_DIR/$BINARY

    log "skills-registry installer"
    log "  platform : $os/$arch"
    log "  url      : $url"
    log "  install  : $dest"

    if [ -n "${SKILLS_REGISTRY_DRY_RUN:-}" ]; then
        printf '%s\n' "$url"
        return 0
    fi

    mkdir -p "$BIN_DIR"
    tmpdir=$(mktemp -d 2>/dev/null || mktemp -d -t skills-registry-install)
    trap 'rm -rf "$tmpdir"' EXIT INT TERM

    tarball=$tmpdir/skills-registry.tar.gz
    if [ -n "${SKILLS_REGISTRY_TARBALL:-}" ]; then
        cp "$SKILLS_REGISTRY_TARBALL" "$tarball"
    else
        log "downloading…"
        download_to "$url" "$tarball" || { err "download failed: $url"; return 1; }
    fi

    log "extracting…"
    tar -xzf "$tarball" -C "$tmpdir" "$BINARY"

    if [ ! -f "$tmpdir/$BINARY" ]; then
        err "binary '$BINARY' not found inside downloaded archive"
        return 1
    fi

    mv "$tmpdir/$BINARY" "$dest"
    chmod +x "$dest"

    log "installed: $dest"

    case :$PATH: in
        *:$BIN_DIR:*) ;;
        *) warn "$BIN_DIR is not on your PATH. Add 'export PATH=\"$BIN_DIR:\$PATH\"' to your shell rc." ;;
    esac

    printf 'Run `skills-registry` to get started.\n'
}

main "$@"
