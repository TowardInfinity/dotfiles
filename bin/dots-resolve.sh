#!/bin/sh
#
# dots-resolve.sh — pick the best available `dots` and print its path.
#
#   TARGET="$(sh bin/dots-resolve.sh)"
#
# Prints exactly ONE path on stdout: the `dots` to run. Everything else —
# progress, warnings, errors — goes to stderr, so the command substitution
# above only ever captures the path.
#
# Building `dots` from source costs ~10s and ~120MB of module downloads, which
# is too much to pay on every machine this repo touches. This resolves, in
# order, falling through to the next tier on ANY failure:
#
#   1. a cached release binary this script already downloaded and verified
#   2. download the release binary for this OS/arch from GitHub Releases,
#      verify its sha256 against checksums.txt, cache it
#   3. `go build` from source into <repo>/bin/dots-bin
#   4. <repo>/bin/dots — the bash fallback, which is always present and never
#      fails to resolve
#
# Env overrides (mainly for testing/CI, none required for normal use):
#   DOTS_FORCE_BUILD=1   skip tiers 1-2, go straight to tier 3
#   DOTS_RELEASE_BASE    override the GitHub "latest" release base URL
#   DOTS_RESOLVE_TIMEOUT curl --max-time in seconds (default 10)
#
# POSIX sh: this is invoked by dots.sh (POSIX sh, runs before anything else is
# set up) and by install.sh (bash), so it cannot assume bash itself.

set -u

TIMEOUT="${DOTS_RESOLVE_TIMEOUT:-10}"
RELEASE_BASE="${DOTS_RELEASE_BASE:-https://github.com/TowardInfinity/dotfiles/releases/latest/download}"

log() {
  printf '%s\n' "$*" >&2
}

# ── Locate the repo ───────────────────────────────────────────
# $0 is this script's own path when invoked as `sh bin/dots-resolve.sh` or
# `sh /some/path/bin/dots-resolve.sh` — which is how every caller in this repo
# uses it. Resolve any symlink hops (readlink with no flags is portable; -f is
# not) and walk one directory up from bin/ to get the repo root.
self="$0"
case "$self" in
  /*) : ;;
  *) self="$PWD/$self" ;;
esac
while [ -L "$self" ]; do
  link=$(readlink "$self" 2>/dev/null) || break
  case "$link" in
    /*) self="$link" ;;
    *) self="$(dirname "$self")/$link" ;;
  esac
done
if [ -f "$self" ]; then
  BIN_DIR="$(cd "$(dirname "$self")" && pwd)"
  REPO="$(cd "$BIN_DIR/.." && pwd)"
else
  # Sourced under a name that isn't this script's own — fall back to cwd.
  # Every real caller passes an explicit path to `sh`, so this is a last resort.
  REPO="$PWD"
fi

CACHE_DIR="${XDG_CACHE_HOME:-$HOME/.cache}/dots"

# ── uname -> Go names ─────────────────────────────────────────
go_os() {
  case "$(uname -s)" in
    Darwin) echo darwin ;;
    Linux)  echo linux ;;
    *) echo "" ;;
  esac
}

go_arch() {
  case "$(uname -m)" in
    x86_64|amd64)  echo amd64 ;;
    aarch64|arm64) echo arm64 ;;
    *) echo "" ;;
  esac
}

# ── sha256, whatever is available ─────────────────────────────
# Prints the hex digest of $1 on stdout, or nothing (with a warning on
# stderr) if neither tool is present.
sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" 2>/dev/null | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" 2>/dev/null | awk '{print $1}'
  else
    return 1
  fi
}

# ── Tier 1: cached release binary ─────────────────────────────
# `try_download` (tier 2) records the version it verified in
# $CACHE_DIR/current-version and the digest it verified in dots-<v>.sha256.
#
# The digest is re-checked here rather than assuming the cache is still what
# was written. Be clear about what that does and does not buy: it catches a
# truncated or corrupted file — an interrupted write, a full disk, a bad
# sector — which would otherwise be cached and reused forever, silently.
#
# It is NOT a security boundary. Anything able to rewrite the binary can
# rewrite the digest beside it, and could just as easily replace the symlink
# in ~/.local/bin. Both live in the user's own home directory. Corruption is
# the realistic failure here, and it is the one this catches.
try_cached() {
  [ -f "$CACHE_DIR/current-version" ] || return 1
  v=$(cat "$CACHE_DIR/current-version" 2>/dev/null) || return 1
  [ -n "$v" ] || return 1
  bin="$CACHE_DIR/dots-$v"
  [ -x "$bin" ] || return 1

  want_file="$CACHE_DIR/dots-$v.sha256"
  if [ -f "$want_file" ]; then
    want=$(cat "$want_file" 2>/dev/null)
    got=$(sha256_of "$bin")
    if [ -n "$want" ] && [ -n "$got" ] && [ "$want" != "$got" ]; then
      log "dots-resolve: cached binary failed its checksum — discarding and refetching"
      rm -f "$bin" "$want_file" "$CACHE_DIR/current-version" 2>/dev/null || true
      return 1
    fi
  fi

  printf '%s\n' "$bin"
}

# ── Tier 2: download + verify from GitHub Releases ────────────
try_download() {
  goos=$(go_os)
  goarch=$(go_arch)
  if [ -z "$goos" ] || [ -z "$goarch" ]; then
    log "dots-resolve: unrecognised platform $(uname -s)/$(uname -m) — skipping release download"
    return 1
  fi

  command -v curl >/dev/null 2>&1 || { log "dots-resolve: curl not found — skipping release download"; return 1; }

  asset="dots_${goos}_${goarch}"
  asset_url="$RELEASE_BASE/$asset"
  sums_url="$RELEASE_BASE/checksums.txt"

  # GitHub's .../latest/download/<asset> is a redirect to
  # .../releases/download/<tag>/<asset>. Reading the redirect target (without
  # following it) is a cheap way to learn the actual version tag without a
  # separate API call. `%{redirect_url}` needs no -L.
  redirect=$(curl -fsS --max-time "$TIMEOUT" -o /dev/null -w '%{redirect_url}' "$asset_url" 2>/dev/null) || redirect=""
  version=$(printf '%s' "$redirect" | sed -n 's#.*/releases/download/\([^/]*\)/.*#\1#p')
  if [ -z "$version" ]; then
    log "dots-resolve: could not reach GitHub Releases (or no release published yet) — skipping"
    return 1
  fi

  mkdir -p "$CACHE_DIR" 2>/dev/null || { log "dots-resolve: cannot create cache dir $CACHE_DIR — skipping"; return 1; }

  # Already have this exact version cached and verified? Use it as-is instead
  # of re-downloading — this is what makes tier 1 and tier 2 agree.
  dest="$CACHE_DIR/dots-$version"
  if [ -x "$dest" ]; then
    printf '%s\n' "$version" > "$CACHE_DIR/current-version" 2>/dev/null || true
    printf '%s\n' "$dest"
    return 0
  fi

  tmp="$CACHE_DIR/.dots-$version.$$"
  log "dots-resolve: downloading $asset ($version)..."
  if ! curl -fsSL --max-time "$TIMEOUT" -o "$tmp" "$asset_url" 2>/dev/null; then
    log "dots-resolve: download of $asset failed"
    rm -f "$tmp"
    return 1
  fi

  sums="$CACHE_DIR/.checksums-$version.$$"
  if curl -fsSL --max-time "$TIMEOUT" -o "$sums" "$sums_url" 2>/dev/null; then
    expected=$(awk -v f="$asset" '$2==f{print $1; exit}' "$sums" 2>/dev/null)
    rm -f "$sums"
    if [ -z "$expected" ]; then
      log "dots-resolve: warning — $asset not listed in checksums.txt, skipping verification"
    else
      actual=$(sha256_of "$tmp") || {
        log "dots-resolve: warning — no sha256sum/shasum available, skipping checksum verification"
        actual="$expected"
      }
      if [ "$actual" != "$expected" ]; then
        log "dots-resolve: checksum mismatch for $asset — discarding download"
        rm -f "$tmp"
        return 1
      fi
    fi
  else
    log "dots-resolve: warning — could not fetch checksums.txt, skipping verification"
  fi

  chmod +x "$tmp" 2>/dev/null || { log "dots-resolve: could not make $tmp executable"; rm -f "$tmp"; return 1; }
  mv -f "$tmp" "$dest" 2>/dev/null || { log "dots-resolve: could not install to $dest"; rm -f "$tmp"; return 1; }
  # Record the digest of what actually landed on disk, so tier 1 can tell a
  # corrupted cache from a good one on the next run.
  sha256_of "$dest" > "$CACHE_DIR/dots-$version.sha256" 2>/dev/null || true
  printf '%s\n' "$version" > "$CACHE_DIR/current-version" 2>/dev/null || true
  printf '%s\n' "$dest"
}

# ── Tier 3: build from source ──────────────────────────────────
try_build() {
  go_bin=""
  if command -v go >/dev/null 2>&1; then
    go_bin=go
  elif [ -x /usr/local/go/bin/go ]; then
    go_bin=/usr/local/go/bin/go
  fi
  [ -n "$go_bin" ] || { log "dots-resolve: go not found — skipping source build"; return 1; }

  out="$REPO/bin/dots-bin"
  log "dots-resolve: building dots from source ($go_bin)..."
  # Capture output+status through a variable rather than a pipe: POSIX sh has
  # no `pipefail`, so `cmd | sed ... ` would report sed's exit status, not
  # the build's, and a failed build would look like a success.
  build_log=$(cd "$REPO" && "$go_bin" build -o "$out" ./cmd/dots/ 2>&1)
  build_status=$?
  if [ -n "$build_log" ]; then
    printf '%s\n' "$build_log" | while IFS= read -r line; do
      log "dots-resolve:   $line"
    done
  fi
  if [ "$build_status" -eq 0 ] && [ -x "$out" ]; then
    printf '%s\n' "$out"
    return 0
  fi
  log "dots-resolve: go build failed"
  return 1
}

# ── Tier 4: the bash fallback ──────────────────────────────────
# Always present in this repo — nothing to check, nothing to fail.
try_fallback() {
  printf '%s\n' "$REPO/bin/dots"
}

# ── Resolve ─────────────────────────────────────────────────────
if [ "${DOTS_FORCE_BUILD:-}" != "1" ]; then
  result=$(try_cached) && { printf '%s\n' "$result"; exit 0; }
  result=$(try_download) && { printf '%s\n' "$result"; exit 0; }
else
  log "dots-resolve: --build requested — skipping the release tiers"
fi

result=$(try_build) && { printf '%s\n' "$result"; exit 0; }

try_fallback
exit 0
