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

# ── Release signatures ────────────────────────────────────────
# checksums.txt is fetched from the SAME release as the binary, so verifying
# one against the other only proves the download was not corrupted in transit
# — it proves nothing about who published it. Anything able to replace the
# binary can replace its checksum line in the same breath.
#
# The detached Ed25519 signature closes that: the private key never touches
# GitHub (it lives encrypted on one machine), so compromising the repo or a
# token yields a release the fleet will not execute. The public key is
# committed here, which makes this checkout the root of trust rather than the
# release page.
#
# openssl is the verifier because it is the one tool present on every machine
# here — the low-memory boxes have no gh, no cosign and no minisign. Verified
# working on OpenSSL 3.0.2 (Ubuntu) and 3.6.3 (macOS).
#
# DOTS_SIGNATURE_MODE:
#   warn     verify when a signature is present, allow unsigned (the migration
#            release must be installable by resolvers that predate signing)
#   require  a valid signature or no binary
PUBKEY="${DOTS_RELEASE_PUBKEY:-$REPO/keys/release.pub}"
SIGNATURE_MODE="${DOTS_SIGNATURE_MODE:-warn}"

# verify_signature <file> <sig-url>. Returns 0 when the file is proven, 1 when
# it is proven WRONG, and 2 when the question could not be asked (no signature
# published, no openssl, no public key). Callers decide what 2 means; that is
# the whole difference between warn and require mode.
verify_signature() {
  _f="$1"; _sig_url="$2"
  [ -f "$PUBKEY" ] || { log "dots-resolve: no public key at $PUBKEY"; return 2; }
  command -v openssl >/dev/null 2>&1 || { log "dots-resolve: openssl not found"; return 2; }

  _sig="$CACHE_DIR/.sig.$$"
  if ! curl -fsSL --max-time "$TIMEOUT" -o "$_sig" "$_sig_url" 2>/dev/null; then
    rm -f "$_sig"
    return 2  # unsigned release, not a bad one
  fi

  # -rawin is required for Ed25519: it signs the message itself, not a digest.
  if openssl pkeyutl -verify -pubin -inkey "$PUBKEY" \
       -rawin -in "$_f" -sigfile "$_sig" >/dev/null 2>&1; then
    rm -f "$_sig"
    return 0
  fi
  rm -f "$_sig"
  return 1
}

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

# ── Is a cached binary still the file we verified? ────────────
# $1 the binary, $2 the .sha256 recorded beside it when it was verified.
# True only when the digest is present, readable, computable and equal — a
# missing or unreadable digest is "cannot prove", which counts as no.
#
# Be clear about what this buys. It catches a truncated or corrupted file —
# an interrupted write, a full disk, a bad sector — which would otherwise be
# cached and reused forever, silently. It is NOT a security boundary:
# anything able to rewrite the binary can rewrite the digest beside it, and
# could just as easily replace the symlink in ~/.local/bin. Both live in the
# user's own home directory. Corruption is the realistic failure here.
#
# Failing on a *missing* digest is safe because it only ever means "download
# it again and verify properly", which is tier 2's job.
cached_digest_ok() {
  [ -f "$2" ] || return 1
  want=$(cat "$2" 2>/dev/null) || return 1
  [ -n "$want" ] || return 1
  got=$(sha256_of "$1") || return 1
  [ -n "$got" ] || return 1
  [ "$want" = "$got" ]
}

# ── Tier 1: cached release binary ─────────────────────────────
# `try_download` (tier 2) records the version it verified in
# $CACHE_DIR/current-version and the digest it verified in dots-<v>.sha256.
try_cached() {
  [ -f "$CACHE_DIR/current-version" ] || return 1
  v=$(cat "$CACHE_DIR/current-version" 2>/dev/null) || return 1
  [ -n "$v" ] || return 1
  bin="$CACHE_DIR/dots-$v"
  [ -x "$bin" ] || return 1

  want_file="$CACHE_DIR/dots-$v.sha256"
  if ! cached_digest_ok "$bin" "$want_file"; then
    log "dots-resolve: cached binary is unverified or corrupt — discarding and refetching"
    rm -f "$bin" "$want_file" "$CACHE_DIR/current-version" 2>/dev/null || true
    return 1
  fi

  # Tier 1 is the path that runs on every single invocation of dots, so this
  # is where require mode actually bites. Without it the flip would only take
  # effect on machines that happened to have a cold cache.
  if ! cache_provenance_ok "$v"; then
    log "dots-resolve: cached binary predates signature checking — discarding and refetching"
    rm -f "$bin" "$want_file" "$CACHE_DIR/current-version" 2>/dev/null || true
    return 1
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

  # Already have this exact version cached? Re-check its digest rather than
  # trusting that an executable of the right name is the file we verified —
  # the same check try_cached does, for the same reason (a truncated or
  # corrupted cache would otherwise be reused forever). If it fails, fall
  # through and re-download rather than returning a bad binary.
  dest="$CACHE_DIR/dots-$version"
  if [ -x "$dest" ] && cached_digest_ok "$dest" "$CACHE_DIR/dots-$version.sha256"; then
    if cache_provenance_ok "$version"; then
      printf '%s\n' "$version" > "$CACHE_DIR/current-version" 2>/dev/null || true
      printf '%s\n' "$dest"
      return 0
    fi
    log "dots-resolve: cached $version was not signature-verified — refetching to check it"
    rm -f "$dest" "$CACHE_DIR/dots-$version.sha256" 2>/dev/null || true
  fi
  if [ -x "$dest" ]; then
    log "dots-resolve: cached $version failed its digest — refetching"
    rm -f "$dest" "$CACHE_DIR/dots-$version.sha256" 2>/dev/null || true
  fi

  tmp="$CACHE_DIR/.dots-$version.$$"
  log "dots-resolve: downloading $asset ($version)..."
  if ! curl -fsSL --max-time "$TIMEOUT" -o "$tmp" "$asset_url" 2>/dev/null; then
    log "dots-resolve: download of $asset failed"
    rm -f "$tmp"
    return 1
  fi

  # Verification is mandatory: every path that cannot PROVE the download
  # matches its published digest discards it and returns 1.
  #
  # This tier used to warn and run the binary anyway when checksums.txt was
  # unreachable, when the asset was absent from it, or when no sha256 tool
  # existed (that last case assigned actual="$expected", disguising a skip as
  # a match). The README promised verification unconditionally, so the code
  # has to mean it.
  #
  # Failing closed is cheap here precisely because this is tier 2 of four:
  # returning 1 falls through to a source build, then to bin/dots, so the
  # machine still ends up with a working `dots`. Refusing to run an
  # unverified binary costs a slower tier, not a broken install.
  sums="$CACHE_DIR/.checksums-$version.$$"
  if ! curl -fsSL --max-time "$TIMEOUT" -o "$sums" "$sums_url" 2>/dev/null; then
    log "dots-resolve: could not fetch checksums.txt — refusing to run an unverified binary"
    rm -f "$tmp" "$sums"
    return 1
  fi
  # Signature first: checksums.txt is only worth reading once it is known to
  # be the file that was signed. Verifying the digest against an unverified
  # checksums.txt would be checking the download against the attacker's own
  # arithmetic.
  verify_signature "$sums" "$RELEASE_BASE/checksums.txt.sig"
  case $? in
    0) sig_verified=1 ;;  # proven
    1)
      log "dots-resolve: checksums.txt FAILS its signature — discarding download"
      rm -f "$tmp" "$sums"
      return 1
      ;;
    2)
      if [ "$SIGNATURE_MODE" = "require" ]; then
        log "dots-resolve: no verifiable signature for checksums.txt — refusing (DOTS_SIGNATURE_MODE=require)"
        rm -f "$tmp" "$sums"
        return 1
      fi
      log "dots-resolve: release is unsigned — continuing on checksum alone"
      sig_verified=0
      ;;
  esac

  expected=$(awk -v f="$asset" '$2==f{print $1; exit}' "$sums" 2>/dev/null)
  rm -f "$sums"
  if [ -z "$expected" ]; then
    log "dots-resolve: $asset is not listed in checksums.txt — refusing to run it unverified"
    rm -f "$tmp"
    return 1
  fi
  if ! actual=$(sha256_of "$tmp"); then
    log "dots-resolve: no sha256sum/shasum on this machine — cannot verify, refusing the download"
    rm -f "$tmp"
    return 1
  fi
  if [ "$actual" != "$expected" ]; then
    log "dots-resolve: checksum mismatch for $asset — discarding download"
    rm -f "$tmp"
    return 1
  fi

  chmod +x "$tmp" 2>/dev/null || { log "dots-resolve: could not make $tmp executable"; rm -f "$tmp"; return 1; }
  mv -f "$tmp" "$dest" 2>/dev/null || { log "dots-resolve: could not install to $dest"; rm -f "$tmp"; return 1; }
  # Record the digest of what actually landed on disk, so tier 1 can tell a
  # corrupted cache from a good one on the next run.
  sha256_of "$dest" > "$CACHE_DIR/dots-$version.sha256" 2>/dev/null || true
  # Record HOW it was verified, not just that it was. dots-<v>.sha256 is
  # computed from the file that landed here, so it proves the cache has not
  # rotted — it says nothing about provenance, and a binary admitted on
  # checksum alone during the warn window is indistinguishable from a signed
  # one once cached. This marker is what lets require mode tell them apart.
  rm -f "$CACHE_DIR/dots-$version.sig-ok" 2>/dev/null || true
  [ "${sig_verified:-0}" = "1" ] && : > "$CACHE_DIR/dots-$version.sig-ok" 2>/dev/null
  printf '%s\n' "$version" > "$CACHE_DIR/current-version" 2>/dev/null || true
  printf '%s\n' "$dest"
}

# cache_provenance_ok <version>. In require mode a cached binary counts only if
# it was admitted on a verified signature. Anything else is re-downloaded and
# re-checked, which is the whole point: flipping to require has to re-examine
# what warn mode already let in, or the flip changes nothing on exactly the
# machines it was meant to protect.
cache_provenance_ok() {
  [ "$SIGNATURE_MODE" != "require" ] && return 0
  [ -f "$CACHE_DIR/dots-$1.sig-ok" ]
}

# ── Tier 3: build from source ──────────────────────────────────
# Refuse to build on a box that cannot afford it.
#
# `go build` of this program peaks around a gigabyte. v1 and v2 have 956 MB
# total, under 500 MB available, and no swap — so a build there does not fail
# politely, it invokes the OOM killer, which may take sshd or a service with it
# rather than the compiler. Downloading an 11 MB binary is the correct answer on
# such a machine, and if that is unreachable the shell fallback still works.
#
# Set DOTS_ALLOW_LOW_MEM_BUILD=1 to override.
enough_memory_to_build() {
  [ "${DOTS_ALLOW_LOW_MEM_BUILD:-}" = 1 ] && return 0

  # A recorded light profile settles it, regardless of what the numbers say.
  #
  # The memory check below counts swap, so adding a swapfile to a 956 MB box
  # makes it pass — and then a build "succeeds" by thrashing for ten minutes
  # against network-backed disk. That is worse than fetching an 11 MB binary.
  # The profile records that this machine is deliberately constrained, which is
  # a statement of intent and does not stop being true when swap appears.
  profile="${XDG_CONFIG_HOME:-$HOME/.config}/dots/profile"
  if [ -r "$profile" ] && [ "$(cat "$profile" 2>/dev/null)" = light ]; then
    log "dots-resolve: light profile — not building from source"
    return 1
  fi

  [ -r /proc/meminfo ] || return 0   # not Linux: no cheap check, assume fine

  avail_kb=$(awk '/^MemAvailable:/{print $2; exit}' /proc/meminfo 2>/dev/null)
  [ -n "$avail_kb" ] || return 0

  swap_kb=$(awk '/^SwapTotal:/{print $2; exit}' /proc/meminfo 2>/dev/null)
  [ -n "$swap_kb" ] || swap_kb=0

  # 900 MB of headroom counting swap. Under that, do not risk it.
  total_kb=$((avail_kb + swap_kb))
  [ "$total_kb" -ge 921600 ] && return 0

  log "dots-resolve: only $((total_kb / 1024)) MB available (incl. swap) — skipping the source build"
  log "dots-resolve: override with DOTS_ALLOW_LOW_MEM_BUILD=1 if you know better"
  return 1
}

try_build() {
  enough_memory_to_build || return 1

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
#
# Tier 1 is a fast path, not a pin. It used to win unconditionally, so a
# machine that had ever cached a binary never saw a newer release again — a1
# sat on v0.1.0 after v0.1.1 shipped, reporting an old build as current, which
# is worse than being slow.
#
# Two ways past it now:
#
#   DOTS_FORCE_FETCH=1  skip the cache entirely. install.sh sets this, because
#                       an install is an explicit "make this machine right"
#                       action and one redirect request is a fair price.
#   CACHE_MAX_AGE_H     the cache goes stale on its own, so the curl route and
#                       anything else eventually upgrades without being told.
#
# Skipping tier 1 is cheap: try_download resolves the latest tag from the
# redirect and reuses an already-cached binary of that version rather than
# re-downloading it. The cost is one request, not 11MB.
CACHE_MAX_AGE_H="${DOTS_CACHE_MAX_AGE_H:-24}"

cache_is_fresh() {
  marker="$CACHE_DIR/current-version"
  [ -f "$marker" ] || return 1
  [ -z "$(find "$marker" -mmin "+$((CACHE_MAX_AGE_H * 60))" 2>/dev/null)" ]
}

if [ "${DOTS_FORCE_BUILD:-}" != "1" ]; then
  if [ "${DOTS_FORCE_FETCH:-}" != "1" ] && cache_is_fresh; then
    result=$(try_cached) && { printf '%s\n' "$result"; exit 0; }
  fi
  result=$(try_download) && { printf '%s\n' "$result"; exit 0; }
  # Download failed — offline, GitHub down. Fall back to whatever is cached,
  # however old, before resorting to a build.
  result=$(try_cached) && {
    # Say which version this actually is. "downloading v0.1.2..." followed by
    # silently running v0.1.1 reads as success — that is exactly what happened
    # installing seconds after a release, while the asset was still 404ing.
    log "dots-resolve: download failed — falling back to the cached $(cat "$CACHE_DIR/current-version" 2>/dev/null)"
    log "dots-resolve: re-run to pick up the newer release once it is reachable"
    printf '%s\n' "$result"
    exit 0
  }
else
  log "dots-resolve: --build requested — skipping the release tiers"
fi

result=$(try_build) && { printf '%s\n' "$result"; exit 0; }

try_fallback
exit 0
