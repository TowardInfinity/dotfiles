#!/usr/bin/env bash
# Regression tests for the parts of this repo that are not Go.
#
# `go test ./...` covers cmd/dots. Everything else — the resolver's refusal to
# run an unverified binary, the TOML merger's file mode, and bootstrap's
# --dry/--light guards — is shell and Python, and each of these has already
# been wrong once in a way that reached a real machine. This is the test that
# would have caught them.
#
# Hermetic: no network, no writes outside a temp dir. curl is replaced by a
# stub on PATH that serves fixtures, so the resolver's failure branches can be
# driven directly instead of hoping GitHub misbehaves.
#
#   ./bin/selftest.sh          run everything
#   ./bin/selftest.sh -v       show each check
set -uo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERBOSE=false
[ "${1:-}" = "-v" ] && VERBOSE=true

PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); $VERBOSE && printf '  \033[32mok\033[0m    %s\n' "$1"; return 0; }
bad()  { FAIL=$((FAIL+1)); printf '  \033[31mFAIL\033[0m  %s\n' "$1"; [ -n "${2:-}" ] && printf '        %s\n' "$2"; return 0; }
group(){ printf '\n\033[36m%s\033[0m\n' "$1"; }

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# ── curl stub ─────────────────────────────────────────────────
# Serves $FIXTURES/<basename of url>. Two behaviours the real curl has that
# the resolver depends on: -w '%{redirect_url}' must print a redirect target
# (that is how the version tag is discovered), and a missing file must exit
# non-zero the way --fail does.
mkdir -p "$WORK/bin"
cat > "$WORK/bin/curl" <<'STUB'
#!/usr/bin/env bash
url=""; out=""; want_redirect=false
while [ $# -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift 2 ;;
    -w) case "$2" in *redirect_url*) want_redirect=true ;; esac; shift 2 ;;
    --max-time|--max-redirs) shift 2 ;;
    -*) shift ;;
    *) url="$1"; shift ;;
  esac
done
[ -n "${CURL_LOG:-}" ] && printf '%s\n' "$url" >> "$CURL_LOG"
name="${url##*/}"
if $want_redirect; then
  [ -f "$FIXTURES/$name" ] || exit 22
  printf 'https://example.invalid/releases/download/%s/%s\n' "$FIXTURE_VERSION" "$name"
  exit 0
fi
[ -f "$FIXTURES/$name" ] || exit 22
[ -n "$out" ] && cp "$FIXTURES/$name" "$out" || cat "$FIXTURES/$name"
exit 0
STUB
chmod +x "$WORK/bin/curl"

sha_of() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | awk '{print $1}'
  else shasum -a 256 "$1" | awk '{print $1}'; fi
}

# Octal permission bits, GNU or BSD.
#
# Dispatch on the OS rather than chaining `stat -f ... || stat -c ...`. That
# chain looks portable and is not: GNU's -f is --file-system, so
# `stat -f '%Lp' file` reads the format string as a second FILENAME, prints
# filesystem info for the real one, and exits non-zero — so the fallback runs
# too and the caller gets a filesystem dump with the mode glued on the end.
# It failed this suite in CI while the code under test was perfectly correct.
file_mode() {
  case "$(uname -s)" in
    Darwin|FreeBSD) stat -f '%Lp' "$1" ;;
    *)              stat -c '%a' "$1" ;;
  esac
}

# Runs dots-resolve.sh against a fresh fixture set. Echoes stdout; stderr in
# $WORK/err. $1 names the scenario.
resolve() {
  rm -rf "$WORK/cache"
  : > "$WORK/curl.log"
  env PATH="$WORK/bin:$PATH" \
      XDG_CACHE_HOME="$WORK/cache" \
      FIXTURES="$FIXTURES" FIXTURE_VERSION="$FIXTURE_VERSION" \
      CURL_LOG="$WORK/curl.log" \
      DOTS_RELEASE_BASE="https://example.invalid/releases/latest/download" \
      DOTS_SIGNATURE_MODE=warn \
      DOTS_FORCE_FETCH=1 DOTS_NO_BUILD=1 \
      sh "$REPO/bin/dots-resolve.sh" 2>"$WORK/err"
}

# ── resolver: verification is mandatory ───────────────────────
group "dots-resolve: unverifiable downloads are refused"

FIXTURE_VERSION="v9.9.9"
FIXTURE_COMMIT="0123456789abcdef0123456789abcdef01234567"
goos=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$(uname -m)" in x86_64|amd64) goarch=amd64 ;; aarch64|arm64) goarch=arm64 ;; *) goarch=unknown ;; esac
ASSET="dots_${goos}_${goarch}"

FIXTURES="$WORK/fx"; mkdir -p "$FIXTURES"
printf '#!/bin/sh\necho fake\n' > "$FIXTURES/$ASSET"
GOOD_SHA="$(sha_of "$FIXTURES/$ASSET")"
write_manifest() { # write_manifest <sha> <asset> [tag] [commit]
  printf '# tag=%s commit=%s\n%s  %s\n' \
    "${3:-$FIXTURE_VERSION}" "${4:-$FIXTURE_COMMIT}" "$1" "$2" \
    > "$FIXTURES/checksums.txt"
}

# 1. Everything present and correct -> accepted.
write_manifest "$GOOD_SHA" "$ASSET"
got="$(resolve)"
[ -n "$got" ] && [ -x "$got" ] \
  && ok "a correctly checksummed manifest is accepted in warn mode" \
  || bad "a correctly checksummed manifest is accepted in warn mode" "got '$got'; stderr: $(tail -1 "$WORK/err")"

# A refusal is not "resolve fails" — it is "this tier declines, the next one
# runs". Tier 3 (go build) or tier 4 (bin/dots) then answers, and that is the
# point: the machine still gets a working dots, just not an unverified one.
# So the assertion is that the download was refused AND was not handed back,
# never that the whole resolve came up empty.
refused() { # $1 label, $2 expected message, $3 what resolve returned
  if ! grep -q "$2" "$WORK/err"; then
    bad "$1" "no refusal logged; stderr: $(tail -1 "$WORK/err")"; return
  fi
  case "$3" in
    "$WORK/cache"/*) bad "$1" "returned the unverified download: $3" ;;
    *) ok "$1" ;;
  esac
}

# 2. checksums.txt unreachable.
rm -f "$FIXTURES/checksums.txt"
refused "checksums.txt unreachable is refused" "refusing" "$(resolve)"

# 3. Asset absent from checksums.txt.
write_manifest "$GOOD_SHA" "some_other_asset"
refused "asset missing from checksums.txt is refused" "not listed" "$(resolve)"

# 4. Digest disagrees. This one always worked; it is here so a future
#    refactor cannot quietly lose it.
write_manifest "0000000000000000000000000000000000000000000000000000000000000000" "$ASSET"
refused "checksum mismatch is refused" "mismatch" "$(resolve)"

# DOTS_NO_BUILD is the seam that keeps every refusal above hermetic. It was set
# by this suite for months but ignored by the resolver, so Linux compiled the
# whole Go TUI after every negative case while macOS's warm cache hid the cost.
out=$(env PATH="$WORK/bin:$PATH" DOTS_FORCE_BUILD=1 DOTS_NO_BUILD=1 \
      sh "$REPO/bin/dots-resolve.sh" 2>"$WORK/err")
[ "$out" = "$REPO/bin/dots" ] && ! grep -q "building dots" "$WORK/err" \
  && ok "DOTS_NO_BUILD goes straight to the shell fallback" \
  || bad "DOTS_NO_BUILD goes straight to the shell fallback" "out=[$out] err=[$(cat "$WORK/err")]"

# 5. A corrupted cache is discarded rather than reused forever.
write_manifest "$GOOD_SHA" "$ASSET"
got="$(resolve)"
if [ -n "$got" ]; then
  printf 'tampered' >> "$got"
  again="$(env PATH="$WORK/bin:$PATH" XDG_CACHE_HOME="$WORK/cache" \
        FIXTURES="$FIXTURES" FIXTURE_VERSION="$FIXTURE_VERSION" \
        DOTS_RELEASE_BASE="https://example.invalid/releases/latest/download" \
        DOTS_SIGNATURE_MODE=warn \
        sh "$REPO/bin/dots-resolve.sh" 2>"$WORK/err")"
  [ "$(sha_of "$again")" = "$GOOD_SHA" ] \
    && ok "a corrupted cached binary is re-fetched" \
    || bad "a corrupted cached binary is re-fetched" "digest still wrong"
else
  bad "a corrupted cached binary is re-fetched" "setup failed"
fi

# ── resolver: release signatures ──────────────────────────────
# The checksum tests above prove the download is intact. They cannot prove who
# published it: checksums.txt ships from the same release as the binary, so
# whatever can replace one can replace the other. These tests cover the part
# that closes that — a detached Ed25519 signature made by a key that never
# touches GitHub.
#
# Hermetic: an ephemeral keypair is generated here, so a failure means the
# resolver is wrong, never that the real signing key rotated.
group "dots-resolve: release signatures"

sig_supported() {
  command -v openssl >/dev/null 2>&1 || return 1
  openssl genpkey -algorithm ed25519 -out "$WORK/sigkey.pem" 2>/dev/null || return 1
  openssl pkey -in "$WORK/sigkey.pem" -pubout -out "$WORK/sigkey.pub" 2>/dev/null || return 1
  return 0
}

# Same fixture set as above, plus a signature and a mode. Echoes stdout;
# stderr in $WORK/err.
resolve_sig() { # $1 = warn|require
  rm -rf "$WORK/cache"
  : > "$WORK/curl.log"
  env PATH="$WORK/bin:$PATH" \
      XDG_CACHE_HOME="$WORK/cache" \
      FIXTURES="$FIXTURES" FIXTURE_VERSION="$FIXTURE_VERSION" \
      CURL_LOG="$WORK/curl.log" \
      DOTS_RELEASE_BASE="https://example.invalid/releases/latest/download" \
      DOTS_RELEASE_PUBKEY="$WORK/sigkey.pub" \
      DOTS_SIGNATURE_MODE="$1" \
      DOTS_FORCE_FETCH=1 DOTS_NO_BUILD=1 \
      sh "$REPO/bin/dots-resolve.sh" 2>"$WORK/err"
}

accepted() { # $1 label, $2 what resolve returned
  case "$2" in
    "$WORK/cache"/*) ok "$1" ;;
    *) bad "$1" "not accepted; stderr: $(tail -1 "$WORK/err")" ;;
  esac
}

out="$(resolve_sig requrie)"; rc=$?
if [ "$rc" -ne 0 ] && grep -q "must be require or warn" "$WORK/err"; then
  ok "an invalid signature mode is rejected loudly"
else
  bad "an invalid signature mode is rejected loudly" "rc=$rc out=[$out] err=[$(cat "$WORK/err")]"
fi

if sig_supported; then
  # Restore the good fixture set (test 5 above left the cache tampered).
  printf '#!/bin/sh\necho fake\n' > "$FIXTURES/$ASSET"
  GOOD_SHA="$(sha_of "$FIXTURES/$ASSET")"
  write_manifest "$GOOD_SHA" "$ASSET"
  sign_fixture() {
    openssl pkeyutl -sign -inkey "$WORK/sigkey.pem" -rawin \
      -in "$FIXTURES/checksums.txt" -out "$FIXTURES/checksums.txt.sig" 2>/dev/null
  }
  sign_fixture

  # 1/2. A correct signature is accepted, and require mode must not be
  #      stricter than the signature actually being valid.
  accepted "a validly signed release is accepted (warn)"    "$(resolve_sig warn)"
  accepted "a validly signed release is accepted (require)" "$(resolve_sig require)"

  # Discovery is the only request allowed through mutable latest/download.
  # Once it yields a tag, the asset, manifest, and signature must all come from
  # that tag's directory, or a publication racing this run can mix releases.
  latest_count=$(grep -c "/releases/latest/download/$ASSET$" "$WORK/curl.log" 2>/dev/null)
  tagged_count=$(grep -c "/releases/download/$FIXTURE_VERSION/" "$WORK/curl.log" 2>/dev/null)
  if [ "$latest_count" = 1 ] && [ "$tagged_count" = 3 ] \
       && ! grep -q '/latest/download/checksums.txt' "$WORK/curl.log"; then
    ok "all post-discovery downloads are pinned to the resolved tag"
  else
    bad "all post-discovery downloads are pinned to the resolved tag" \
        "$(tr '\n' ' ' < "$WORK/curl.log")"
  fi

  # The signature alone identifies a manifest, not the release under which it
  # is being served. Bind the signed header to the redirect-resolved tag.
  write_manifest "$GOOD_SHA" "$ASSET" "v9.9.8"
  sign_fixture
  refused "a signed manifest for another tag is refused" "manifest is for" \
    "$(resolve_sig require)"

  printf '%s  %s\n' "$GOOD_SHA" "$ASSET" > "$FIXTURES/checksums.txt"
  sign_fixture
  refused "a signed manifest without an identity header is refused (require)" "missing or malformed" \
    "$(resolve_sig require)"
  refused "a signed manifest without an identity header is refused (warn)" "missing or malformed" \
    "$(resolve_sig warn)"

  # Restore the valid bytes before exercising signature-mode differences.
  write_manifest "$GOOD_SHA" "$ASSET"
  sign_fixture

  # 3. Unsigned: the migration release has to stay installable, so warn mode
  #    proceeds — but it must say so rather than passing silently.
  rm -f "$FIXTURES/checksums.txt.sig"
  got="$(resolve_sig warn)"
  if grep -q "unsigned" "$WORK/err"; then
    accepted "an unsigned release is accepted with a warning (warn)" "$got"
  else
    bad "an unsigned release is accepted with a warning (warn)" \
        "no warning logged; stderr: $(tail -1 "$WORK/err")"
  fi

  # 4. …and require mode refuses it. This is the flip that makes signing
  #    mandatory, and it is the only difference between the two modes.
  refused "an unsigned release is refused (require)" "refusing" "$(resolve_sig require)"

  # 5. A signature over different bytes. This is the attack the whole scheme
  #    exists for: a replaced checksums.txt carrying the old, real signature.
  sign_fixture
  write_manifest \
    "1111111111111111111111111111111111111111111111111111111111111111" "$ASSET"
  refused "a signature over different bytes is refused (warn)" "FAILS" "$(resolve_sig warn)"
  refused "a signature over different bytes is refused (require)" "FAILS" "$(resolve_sig require)"

  # 6. Malformed — not 64 bytes, not a signature at all. Must fail closed
  #    (proven wrong), not open as "could not ask".
  write_manifest "$GOOD_SHA" "$ASSET"
  printf 'not a signature' > "$FIXTURES/checksums.txt.sig"
  refused "a malformed signature is refused (warn)" "FAILS" "$(resolve_sig warn)"

  # 7. Right length, wrong content — a bit-flip, which is what a truncated or
  #    tampered upload looks like.
  #    Flip a bit rather than writing a random byte: /dev/urandom can hand back
  #    the byte that is already there, and a test that passes 255 times in 256
  #    is worse than no test.
  sign_fixture
  python3 - "$FIXTURES/checksums.txt.sig" <<'PY'
import sys
p = sys.argv[1]
b = bytearray(open(p, 'rb').read())
b[0] ^= 0x01
open(p, 'wb').write(bytes(b))
PY
  refused "an altered signature is refused (warn)" "FAILS" "$(resolve_sig warn)"

  # 8. A wrong-but-valid key: signed correctly, by someone else. Verifying
  #    against the committed key is the only thing that catches this.
  openssl genpkey -algorithm ed25519 -out "$WORK/other.pem" 2>/dev/null
  openssl pkeyutl -sign -inkey "$WORK/other.pem" -rawin \
    -in "$FIXTURES/checksums.txt" -out "$FIXTURES/checksums.txt.sig" 2>/dev/null
  refused "a signature from an unknown key is refused" "FAILS" "$(resolve_sig warn)"

  # 9. The cache must not launder provenance. A binary admitted on checksum
  #    alone during the warn window is byte-identical to a signed one once it
  #    is sitting in the cache, so require mode has to re-examine it — or the
  #    flip to require changes nothing on exactly the machines it protects.
  write_manifest "$GOOD_SHA" "$ASSET"
  rm -f "$FIXTURES/checksums.txt.sig"
  got="$(resolve_sig warn)"                 # cached with NO signature
  if [ -n "$got" ] && [ -f "$got" ]; then
    # Same cache, now under require. The unsigned entry must not be reused.
    out="$(env PATH="$WORK/bin:$PATH" XDG_CACHE_HOME="$WORK/cache" \
          FIXTURES="$FIXTURES" FIXTURE_VERSION="$FIXTURE_VERSION" \
          DOTS_RELEASE_BASE="https://example.invalid/releases/latest/download" \
          DOTS_RELEASE_PUBKEY="$WORK/sigkey.pub" DOTS_SIGNATURE_MODE=require \
          DOTS_NO_BUILD=1 sh "$REPO/bin/dots-resolve.sh" 2>"$WORK/err")"
    case "$out" in
      "$WORK/cache"/*) bad "an unsigned cached binary is not reused in require mode" \
                           "it was handed back: $out" ;;
      *) ok "an unsigned cached binary is not reused in require mode" ;;
    esac
  else
    bad "an unsigned cached binary is not reused in require mode" "setup failed"
  fi

  # 10. …and the same cache under warn mode is still fine. Re-verifying is not
  #     a licence to throw away a good cache on every run.
  sign_fixture
  got="$(resolve_sig require)"              # cached WITH a signature
  out="$(env PATH="$WORK/bin:$PATH" XDG_CACHE_HOME="$WORK/cache" \
        FIXTURES="$FIXTURES" FIXTURE_VERSION="$FIXTURE_VERSION" \
        DOTS_RELEASE_BASE="https://example.invalid/releases/latest/download" \
        DOTS_RELEASE_PUBKEY="$WORK/sigkey.pub" DOTS_SIGNATURE_MODE=require \
        DOTS_NO_BUILD=1 sh "$REPO/bin/dots-resolve.sh" 2>"$WORK/err")"
  accepted "a signature-verified cached binary is reused in require mode" "$out"

  # 11. Rotating the key invalidates what the old one admitted. Without this
  #     the marker would say "verified" forever, including by a key that has
  #     since been rotated away — which is the one moment trust most needs
  #     withdrawing, and the only moment a stale cache is dangerous.
  sign_fixture
  got="$(resolve_sig require)"              # cached, verified by sigkey
  openssl genpkey -algorithm ed25519 -out "$WORK/rot.pem" 2>/dev/null
  openssl pkey -in "$WORK/rot.pem" -pubout -out "$WORK/rot.pub" 2>/dev/null
  out="$(env PATH="$WORK/bin:$PATH" XDG_CACHE_HOME="$WORK/cache" \
        FIXTURES="$FIXTURES" FIXTURE_VERSION="$FIXTURE_VERSION" \
        DOTS_RELEASE_BASE="https://example.invalid/releases/latest/download" \
        DOTS_RELEASE_PUBKEY="$WORK/rot.pub" DOTS_SIGNATURE_MODE=require \
        DOTS_NO_BUILD=1 sh "$REPO/bin/dots-resolve.sh" 2>"$WORK/err")"
  case "$out" in
    "$WORK/cache"/*) bad "a rotated key invalidates the cache it admitted" \
                         "the old entry was reused: $out" ;;
    *) ok "a rotated key invalidates the cache it admitted" ;;
  esac

  # 12. Declining a cache entry must not DESTROY it. install.sh points
  #     ~/.local/bin/dots straight at this file, so deleting it before a
  #     replacement exists leaves the machine with no dots command at all —
  #     on a box whose next download is precisely the one being refused.
  #     This bricked v2 for real, via a rotated key and no valid release.
  sign_fixture
  got="$(resolve_sig require)"
  openssl genpkey -algorithm ed25519 -out "$WORK/rot2.pem" 2>/dev/null
  openssl pkey -in "$WORK/rot2.pem" -pubout -out "$WORK/rot2.pub" 2>/dev/null
  rm -f "$FIXTURES/checksums.txt.sig"     # and nothing valid to replace it with
  env PATH="$WORK/bin:$PATH" XDG_CACHE_HOME="$WORK/cache" \
      FIXTURES="$FIXTURES" FIXTURE_VERSION="$FIXTURE_VERSION" \
      DOTS_RELEASE_BASE="https://example.invalid/releases/latest/download" \
      DOTS_RELEASE_PUBKEY="$WORK/rot2.pub" DOTS_SIGNATURE_MODE=require \
      DOTS_NO_BUILD=1 sh "$REPO/bin/dots-resolve.sh" >/dev/null 2>"$WORK/err"
  [ -x "$got" ] \
    && ok "a declined cache entry survives, so the symlink does not dangle" \
    || bad "a declined cache entry survives, so the symlink does not dangle" \
          "$got was deleted — ~/.local/bin/dots would now point at nothing"

  # 13. A CORRUPT entry is still deleted. Declining and discarding are
  #     different judgements: these bytes changed after being verified, and
  #     leaving them reachable through the symlink means running them.
  sign_fixture
  got="$(resolve_sig require)"
  printf 'tampered' >> "$got"
  env PATH="$WORK/bin:$PATH" XDG_CACHE_HOME="$WORK/cache" \
      FIXTURES="$FIXTURES" FIXTURE_VERSION="$FIXTURE_VERSION" \
      DOTS_RELEASE_BASE="https://example.invalid/releases/latest/download" \
      DOTS_RELEASE_PUBKEY="$WORK/sigkey.pub" DOTS_SIGNATURE_MODE=require \
      DOTS_NO_BUILD=1 sh "$REPO/bin/dots-resolve.sh" >/dev/null 2>"$WORK/err"
  [ "$(sha_of "$got" 2>/dev/null)" = "$GOOD_SHA" ] \
    && ok "a corrupted cache entry is replaced, not kept" \
    || bad "a corrupted cache entry is replaced, not kept" "digest still wrong"

  rm -f "$FIXTURES/checksums.txt.sig"
else
  $VERBOSE && printf '  \033[90mskip\033[0m  release signatures (no openssl with ed25519)\n'
fi

# ── sign-release.sh: refuses before it publishes ──────────────
# Everything this script guards against is unrecoverable once it has run:
# publishing is not undoable, and a release signed with the wrong key bricks
# `dots update` on four machines at once. So the checks it makes before
# touching the network are worth testing without the network.
group "sign-release.sh: preflight refusals"

bash -n "$REPO/bin/sign-release.sh" 2>/dev/null \
  && ok "sign-release.sh parses" || bad "sign-release.sh parses"

out=$(bash "$REPO/bin/sign-release.sh" 2>&1); rc=$?
[ "$rc" -ne 0 ] && printf '%s' "$out" | grep -qi usage \
  && ok "a missing tag is rejected" \
  || bad "a missing tag is rejected" "rc=$rc: $out"

# With no public key committed there is nothing to verify against, so signing
# would be unfalsifiable. It must stop before the passphrase prompt, not after.
out=$(env HOME="$WORK" DOTS_SIGNING_KEY="$WORK/nope.pem" \
      bash "$REPO/bin/sign-release.sh" v0.0.0-selftest 2>&1); rc=$?
[ "$rc" -ne 0 ] \
  && ok "it refuses when a key is missing" \
  || bad "it refuses when a key is missing" "it continued: $out"

# Exercise the local-vs-remote tag comparison without a release, a signing key,
# or a network. The script must distinguish a proved mismatch from an origin it
# could not ask; both used to collapse into trusting the local ref silently.
SIGNBIN="$WORK/sign-bin"; mkdir -p "$SIGNBIN"
SIGN_GH_LOG="$WORK/sign-gh.log"
SIGN_MANIFEST="$WORK/sign-manifest.txt"
SIGN_KEY="$WORK/sign-key.pem"; : > "$SIGN_KEY"
SIGN_SHA="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
SIGN_HASH="eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
SIGN_TAG="v9.9.9"

cat > "$SIGNBIN/git" <<'STUB'
#!/usr/bin/env bash
case "${1:-}" in
  show) cat "$REAL_PUBKEY" ;;
  rev-parse) printf '%s\n' "$SIGN_SHA" ;;
  ls-remote)
    case "${GIT_REMOTE_MODE:-equal}" in
      equal) printf '%s\trefs/tags/%s\n' "$SIGN_SHA" "$SIGN_TAG" ;;
      annotated)
        printf '%s\trefs/tags/%s\n' "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" "$SIGN_TAG"
        printf '%s\trefs/tags/%s^{}\n' "$SIGN_SHA" "$SIGN_TAG"
        ;;
      mismatch) printf '%s\trefs/tags/%s\n' "cccccccccccccccccccccccccccccccccccccccc" "$SIGN_TAG" ;;
      empty) exit 0 ;;
      fail) exit 2 ;;
    esac
    ;;
  *) exit 2 ;;
esac
STUB

cat > "$SIGNBIN/gh" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$SIGN_GH_LOG"
[ "${GH_MODE:-fail}" = draft ] || exit 2
case "${1:-} ${2:-}" in
  "release view")
    case " $* " in
      *" isDraft "*) printf 'true\n' ;;
      *" assets "*)
        for a in checksums.txt dots_darwin_arm64 dots_darwin_amd64 dots_linux_arm64 dots_linux_amd64; do
          printf '%s\thttps://api.example.invalid/assets/%s\tsha256:%s\n' "$a" "$a" "${GH_DIGEST:-$SIGN_HASH}"
        done
        ;;
      *) exit 2 ;;
    esac
    ;;
  "release list") exit 0 ;;
  "release download")
    dest=""
    while [ $# -gt 0 ]; do
      if [ "$1" = --dir ]; then dest=$2; break; fi
      shift
    done
    [ -n "$dest" ] || exit 2
    cp "$SIGN_MANIFEST" "$dest/checksums.txt"
    ;;
  api*) printf '%s\n' "${GH_UPLOADER:-github-actions[bot]}" ;;
  *) exit 2 ;;
esac
STUB

cat > "$SIGNBIN/openssl" <<'STUB'
#!/bin/sh
exit 2
STUB
chmod +x "$SIGNBIN/git" "$SIGNBIN/gh" "$SIGNBIN/openssl"

run_sign_preflight() { # run_sign_preflight <remote-mode> <gh-mode>
  : > "$SIGN_GH_LOG"
  env PATH="$SIGNBIN:/usr/bin:/bin" \
      REAL_PUBKEY="$REPO/keys/release.pub" SIGN_SHA="$SIGN_SHA" SIGN_HASH="$SIGN_HASH" SIGN_TAG="$SIGN_TAG" \
      GIT_REMOTE_MODE="$1" GH_MODE="$2" SIGN_GH_LOG="$SIGN_GH_LOG" \
      GH_UPLOADER="${3:-github-actions[bot]}" \
      GH_DIGEST="${4:-$SIGN_HASH}" \
      SIGN_MANIFEST="$SIGN_MANIFEST" DOTS_SIGNING_KEY="$SIGN_KEY" \
      bash "$REPO/bin/sign-release.sh" "$SIGN_TAG" 2>&1
}

out=$(run_sign_preflight mismatch fail); rc=$?
[ "$rc" -ne 0 ] && printf '%s' "$out" | grep -q "does not match origin" \
  && [ ! -s "$SIGN_GH_LOG" ] \
  && ok "a local tag that differs from origin is refused before GitHub is touched" \
  || bad "a local tag that differs from origin is refused before GitHub is touched" "rc=$rc: $out"

out=$(run_sign_preflight equal fail); rc=$?
[ "$rc" -ne 0 ] && grep -q '^release view' "$SIGN_GH_LOG" \
  && ok "a matching lightweight remote tag passes the comparison" \
  || bad "a matching lightweight remote tag passes the comparison" "rc=$rc: $out"

out=$(run_sign_preflight annotated fail); rc=$?
[ "$rc" -ne 0 ] && grep -q '^release view' "$SIGN_GH_LOG" \
  && ok "an annotated remote tag is compared by its peeled commit" \
  || bad "an annotated remote tag is compared by its peeled commit" "rc=$rc: $out"

out=$(run_sign_preflight fail fail); rc=$?
[ "$rc" -ne 0 ] && printf '%s' "$out" | grep -q "could not reach origin" \
  && grep -q '^release view' "$SIGN_GH_LOG" \
  && ok "an unreachable origin is an explicit warning, not a silent pass" \
  || bad "an unreachable origin is an explicit warning, not a silent pass" "rc=$rc: $out"

printf '# tag=v9.9.8 commit=%s\n' "$SIGN_SHA" > "$SIGN_MANIFEST"
out=$(run_sign_preflight equal draft); rc=$?
[ "$rc" -ne 0 ] && printf '%s' "$out" | grep -q "is for v9.9.8, not $SIGN_TAG" \
  && ok "the signer refuses a draft manifest naming another tag" \
  || bad "the signer refuses a draft manifest naming another tag" "rc=$rc: $out"

printf '# tag=%s commit=%s\n' "$SIGN_TAG" "dddddddddddddddddddddddddddddddddddddddd" > "$SIGN_MANIFEST"
out=$(run_sign_preflight equal draft); rc=$?
[ "$rc" -ne 0 ] && printf '%s' "$out" | grep -q "not $SIGN_SHA" \
  && ok "the signer refuses a draft manifest naming another commit" \
  || bad "the signer refuses a draft manifest naming another commit" "rc=$rc: $out"

{
  printf '# tag=%s commit=%s\n' "$SIGN_TAG" "$SIGN_SHA"
  for a in dots_darwin_arm64 dots_darwin_amd64 dots_linux_arm64 dots_linux_amd64; do
    printf '%s  %s\n' "$SIGN_HASH" "$a"
  done
} > "$SIGN_MANIFEST"
out=$(run_sign_preflight equal draft TowardInfinity); rc=$?
[ "$rc" -ne 0 ] && printf '%s' "$out" | grep -q "not github-actions\[bot\]" \
  && ok "the signer refuses draft inputs replaced through a user token" \
  || bad "the signer refuses draft inputs replaced through a user token" "rc=$rc: $out"

out=$(run_sign_preflight equal draft 'github-actions[bot]' \
      ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff); rc=$?
[ "$rc" -ne 0 ] && printf '%s' "$out" | grep -q "does not match checksums.txt" \
  && ok "the signer refuses a draft binary whose server digest changed" \
  || bad "the signer refuses a draft binary whose server digest changed" "rc=$rc: $out"

# ── merge-toml-block.py: file mode ────────────────────────────
group "merge-toml-block.py: does not widen the file it rewrites"

BEGIN='# >>> t >>>'; END='# <<< t <<<'
SRC="$WORK/policy.toml"; printf 'model = "x"\n' > "$SRC"

# Existing private file keeps its mode. Ubuntu's umask is 002, which is what
# turned ~/.codex/config.toml into 0664 on three servers.
DST="$WORK/existing.toml"
printf 'model = "old"\n[projects."/p"]\nk = 1\n' > "$DST"
chmod 600 "$DST"
( umask 002; python3 "$REPO/bin/merge-toml-block.py" --src "$SRC" --dst "$DST" --begin "$BEGIN" --end "$END" >/dev/null )
mode=$(file_mode "$DST")
[ "$mode" = "600" ] && ok "an existing 0600 config stays 0600 under umask 002" \
                    || bad "an existing 0600 config stays 0600 under umask 002" "mode is $mode"

grep -q 'projects' "$DST" && ok "machine-written tables survive the merge" \
                          || bad "machine-written tables survive the merge"

# A file it creates is private, not umask-default.
NEW="$WORK/new.toml"
( umask 002; python3 "$REPO/bin/merge-toml-block.py" --src "$SRC" --dst "$NEW" --begin "$BEGIN" --end "$END" >/dev/null )
mode=$(file_mode "$NEW")
[ "$mode" = "600" ] && ok "a newly created agent config is 0600" \
                    || bad "a newly created agent config is 0600" "mode is $mode"

# Running twice must converge, or every install reports a change.
out=$(python3 "$REPO/bin/merge-toml-block.py" --src "$SRC" --dst "$DST" --begin "$BEGIN" --end "$END")
[ "$out" = "unchanged" ] && ok "a second merge is a no-op" \
                         || bad "a second merge is a no-op" "printed '$out'"

# ── bootstrap.sh guards ───────────────────────────────────────
group "bootstrap.sh: --dry writes nothing, --light is Linux-only"

DRYHOME="$WORK/dryhome"; mkdir -p "$DRYHOME/.ssh"
env HOME="$DRYHOME" sh "$REPO/bootstrap.sh" --dry >/dev/null 2>&1
[ ! -e "$DRYHOME/.ssh/known_hosts" ] \
  && ok "--dry does not write known_hosts" \
  || bad "--dry does not write known_hosts" "the ssh probe ran anyway"

INSTALL_DRY_HOME="$WORK/install-dry-home"; mkdir -p "$INSTALL_DRY_HOME"
env HOME="$INSTALL_DRY_HOME" bash "$REPO/install.sh" --dry >/dev/null 2>&1
dry_changes=$(find "$INSTALL_DRY_HOME" -mindepth 1 -print)
[ -z "$dry_changes" ] \
  && ok "install.sh --dry leaves an empty HOME entirely empty" \
  || bad "install.sh --dry leaves an empty HOME entirely empty" "$dry_changes"

if [ "$(uname -s)" = "Darwin" ]; then
  env HOME="$DRYHOME" sh "$REPO/bootstrap.sh" --light --dry >/dev/null 2>&1
  [ $? -ne 0 ] && ok "--light is rejected on macOS" \
               || bad "--light is rejected on macOS" "it was accepted"
else
  $VERBOSE && printf '  \033[90mskip\033[0m  --light macOS guard (not on Darwin)\n'
fi

# ── bootstrap.sh dependency recovery ─────────────────────────
#
# Exercise the full-deps state transitions without touching the host. Linux is
# forced through PATH and every privileged/package command is stubbed. Archive
# installs deliberately fail: linking still has to run after package trouble.
DEPSHOME="$WORK/depshome"
DEPSREPO="$WORK/depsrepo"
DEPSBIN="$WORK/depsbin"
DEPS_LINKED="$WORK/deps-linked"
mkdir -p "$DEPSHOME/.config/dots" "$DEPSHOME/.sdkman/bin" \
         "$DEPSHOME/.oh-my-zsh/custom/plugins/zsh-autosuggestions" \
         "$DEPSHOME/.oh-my-zsh/custom/plugins/zsh-syntax-highlighting" \
         "$DEPSREPO/.git" "$DEPSBIN"
printf 'light\n' > "$DEPSHOME/.config/dots/profile"
printf '# installed\n' > "$DEPSHOME/.oh-my-zsh/oh-my-zsh.sh"
cat > "$DEPSHOME/.sdkman/bin/sdkman-init.sh" <<'SDK'
sdk() {
  mkdir -p "$HOME/.sdkman/candidates/java/current/bin"
  printf '#!/bin/sh\n' > "$HOME/.sdkman/candidates/java/current/bin/java"
  chmod +x "$HOME/.sdkman/candidates/java/current/bin/java"
}
SDK
cat > "$DEPSREPO/install.sh" <<'INSTALL'
#!/bin/sh
: > "$DOTS_LINK_MARKER"
INSTALL
cat > "$DEPSBIN/uname" <<'STUB'
#!/bin/sh
case "${1:-}" in
  -s) echo Linux ;;
  -m) echo aarch64 ;;
  *)  echo Linux ;;
esac
STUB
cat > "$DEPSBIN/curl" <<'STUB'
#!/bin/sh
out= url=
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) out=$2; shift 2 ;;
    -*) shift ;;
    *) url=$1; shift ;;
  esac
done
[ -n "$out" ] && : > "$out"
case "$url" in
  *go.dev/VERSION*) printf 'go1.25.0\n' ;;
  *api.github.com/repos/charmbracelet/glow*) printf '{"tag_name":"v1.0.0"}\n' ;;
esac
STUB
cat > "$DEPSBIN/sudo" <<'STUB'
#!/bin/sh
case "${1:-}" in
  tar|rm) exit 1 ;;
  *)      exit 0 ;;
esac
STUB
for stub in git uv; do
  printf '#!/bin/sh\nexit 0\n' > "$DEPSBIN/$stub"
done
chmod +x "$DEPSREPO/install.sh" "$DEPSBIN"/*

DEPS_LOG="$WORK/deps.log"
env HOME="$DEPSHOME" PATH="$DEPSBIN:/usr/bin:/bin" \
    DOTFILES_DIR="$DEPSREPO" DOTS_LINK_MARKER="$DEPS_LINKED" \
    sh "$REPO/bootstrap.sh" --deps >"$DEPS_LOG" 2>&1
deps_rc=$?
[ ! -e "$DEPSHOME/.config/dots/profile" ] \
  && ok "--deps clears a stale light profile" \
  || bad "--deps clears a stale light profile"
[ -x "$DEPSHOME/.sdkman/candidates/java/current/bin/java" ] \
  && ok "--deps repairs SDKMAN when its JDK is missing" \
  || bad "--deps repairs SDKMAN when its JDK is missing"
[ "$deps_rc" -eq 0 ] && [ -e "$DEPS_LINKED" ] \
  && ok "Linux archive failures do not abort config linking" \
  || bad "Linux archive failures do not abort config linking" "rc=$deps_rc; $(tail -5 "$DEPS_LOG")"

# ── tmux model indicator ──────────────────────────────────────
#
# The point of this segment is to make an expensive model impossible to miss,
# so the failures that matter are the quiet ones: showing a model for a pane
# whose agent has exited, or showing the *configured* model when the session
# has since been switched to a dearer one. Both look completely normal.
group "tmux: model indicator"

MODEL_SH="$REPO/common/tmux/model.sh"
STATUS_SH="$REPO/common/claude/statusline.sh"
PANES="$WORK/panes"; mkdir -p "$PANES"
FAKE_CODEX="$WORK/codex"; mkdir -p "$FAKE_CODEX/sessions/2026/08/08"
printf 'model = "gpt-5.6-terra"\n' > "$FAKE_CODEX/config.toml"

QSTATE="$WORK/state"; mkdir -p "$QSTATE"

model_seg() {   # model_seg <pane_current_command> [pane_cwd]
  env DOTS_PANE_DIR="$PANES" CODEX_HOME="$FAKE_CODEX" DOTS_STATE_DIR="$QSTATE" \
    sh "$MODEL_SH" '%9' "$1" "${2:-}" 2>/dev/null
}
# write_quota <5h pct> <5h secs away> <7d pct> <7d secs away> [age secs]
write_quota() {
  now=$(date +%s)
  printf '%s\n%s\n%s\n%s\n%s\n' \
    "$((now - ${5:-0}))" "$1" "$((now + $2))" "$3" "$((now + $4))" \
    > "$QSTATE/claude-quota"
}

rm -f "$PANES/9"
[ -z "$(model_seg zsh)" ] \
  && ok "an ordinary pane gets no segment" \
  || bad "an ordinary pane gets no segment" "it printed something"

printf 'claude\t%s\topus\n' "$$" > "$PANES/9"
case "$(model_seg zsh)" in
  *opus*) ok "a live claude pane names its model" ;;
  *) bad "a live claude pane names its model" "$(model_seg zsh)" ;;
esac

# The alarm colour is the feature, not decoration — a grey "opus" is what the
# quota was already being spent on unnoticed.
case "$(model_seg zsh)" in
  *'#f7768e'*) ok "opus is coloured as an expensive tier" ;;
  *) bad "opus is coloured as an expensive tier" "$(model_seg zsh)" ;;
esac
printf 'claude\t%s\tsonnet\n' "$$" > "$PANES/9"
case "$(model_seg zsh)" in
  *'#9ece6a'*) ok "sonnet is coloured as on-policy" ;;
  *) bad "sonnet is coloured as on-policy" "$(model_seg zsh)" ;;
esac

# pid 999999 is not running; a marker outliving its session must not keep
# claiming the pane, or the bar reports a model nothing is being billed for.
printf 'claude\t999999\topus\n' > "$PANES/9"
out=$(model_seg zsh)
if [ -z "$out" ] && [ ! -f "$PANES/9" ]; then
  ok "a claude marker whose process died is dropped"
else
  bad "a claude marker whose process died is dropped" "printed [$out], marker present: $([ -f "$PANES/9" ] && echo yes || echo no)"
fi

# The codex wrapper cleans up on exit, but a killed terminal never gets to.
# tmux's own view of the pane is the authority.
printf 'codex\t%s\tgpt-5.6-sol\n' "$$" > "$PANES/9"
out=$(model_seg zsh)
[ -z "$out" ] && [ ! -f "$PANES/9" ] \
  && ok "a codex marker is dropped once the pane stops running codex" \
  || bad "a codex marker is dropped once the pane stops running codex" "printed [$out]"

# codex started without the wrapper still deserves a segment.
rm -f "$PANES/9"
case "$(model_seg codex)" in
  *terra*) ok "an unwrapped codex pane falls back to the configured model" ;;
  *) bad "an unwrapped codex pane falls back to the configured model" "$(model_seg codex)" ;;
esac

# The whole reason codex reads the rollout: /model mid-session changes what is
# being billed and changes nothing the launch-time marker can see.
ROLL="$FAKE_CODEX/sessions/2026/08/08/rollout-2026-08-08T10-00-00-abc.jsonl"
printf '%s\n' '{"type":"session_meta","payload":{"model_provider":"openai"}}' > "$ROLL"
printf '%s\n' '{"type":"turn_context","payload":{"model":"gpt-5.6-terra","model_reasoning_effort":"low","effort":"medium"}}' >> "$ROLL"
printf '%s\n' '{"type":"turn_context","payload":{"model":"gpt-5.6-sol","model_reasoning_effort":"low","effort":"high"}}' >> "$ROLL"
printf 'codex\t%s\tgpt-5.6-terra\n' "$$" > "$PANES/9"
out=$(model_seg codex)
case "$out" in
  *sol*) ok "a mid-session codex /model beats the launch marker" ;;
  *) bad "a mid-session codex /model beats the launch marker" "$out" ;;
esac
case "$out" in
  *'/hi'*) ok "codex effort rides along with the model" ;;
  *) bad "codex effort rides along with the model" "$out" ;;
esac
# "model_reasoning_effort" also ends in effort":" — anchoring on it would report
# the config value instead of the live one.
case "$out" in
  *'/lo'*) bad "effort is read from the live turn, not model_reasoning_effort" "$out" ;;
  *) ok "effort is read from the live turn, not model_reasoning_effort" ;;
esac

# A rollout from a session that ended hours ago must not colonise a fresh pane.
touch -t 202601010100 "$ROLL"
case "$(model_seg codex)" in
  *terra*) ok "a stale rollout is ignored in favour of the launch marker" ;;
  *) bad "a stale rollout is ignored in favour of the launch marker" "$(model_seg codex)" ;;
esac

# Two codex panes can both touch a rollout inside the same five-minute
# window — picking the single newest one across the whole machine means a
# busy pane elsewhere silently overwrites the model shown for this one. The
# pane's own cwd, matched against each rollout's recorded launch directory,
# is what should break the tie instead.
ROLL_MINE="$FAKE_CODEX/sessions/2026/08/08/rollout-2026-08-08T11-00-00-mine.jsonl"
ROLL_OTHER="$FAKE_CODEX/sessions/2026/08/08/rollout-2026-08-08T11-00-05-other.jsonl"
EVIL_CWD="$WORK/model'; touch $WORK/model-pwned; #"
printf '{"type":"session_meta","payload":{"cwd":"%s"}}\n' "$EVIL_CWD" > "$ROLL_MINE"
printf '%s\n' '{"type":"turn_context","payload":{"model":"gpt-5.6-terra","effort":"medium"}}' >> "$ROLL_MINE"
sleep 1   # ROLL_OTHER must land with a strictly newer mtime, by mtime alone
printf '%s\n' '{"type":"session_meta","payload":{"cwd":"/tmp/other"}}' > "$ROLL_OTHER"
printf '%s\n' '{"type":"turn_context","payload":{"model":"gpt-5.6-sol","effort":"high"}}' >> "$ROLL_OTHER"
printf 'codex\t%s\tgpt-5.6-terra\n' "$$" > "$PANES/9"
out=$(model_seg codex "$EVIL_CWD")
case "$out" in
  *terra*) ok "a pane's own cwd wins over a fresher rollout from elsewhere" ;;
  *) bad "a pane's own cwd wins over a fresher rollout from elsewhere" "$out" ;;
esac

# tmux.conf no longer interpolates the path into #() shell source. model.sh
# asks tmux for it as data instead; a quote and semicolon therefore remain a
# directory name rather than becoming a command on every status refresh.
TMUXBIN="$WORK/tmux-bin"; mkdir -p "$TMUXBIN"
cat > "$TMUXBIN/tmux" <<'STUB'
#!/bin/sh
printf '%s\n' "$TMUX_TEST_CWD"
STUB
chmod +x "$TMUXBIN/tmux"
out=$(env PATH="$TMUXBIN:$PATH" TMUX_TEST_CWD="$EVIL_CWD" \
      DOTS_PANE_DIR="$PANES" CODEX_HOME="$FAKE_CODEX" DOTS_STATE_DIR="$QSTATE" \
      sh "$MODEL_SH" '%9' codex 2>/dev/null)
case "$out" in
  *terra*)
    [ ! -e "$WORK/model-pwned" ] \
      && ok "model cwd lookup treats shell punctuation as data" \
      || bad "model cwd lookup treats shell punctuation as data" "the sentinel was created" ;;
  *) bad "model cwd lookup treats shell punctuation as data" "$out" ;;
esac

# No cwd match (or no cwd passed at all — an older tmux.conf) falls back to
# the previous behaviour: newest by mtime, rather than showing nothing.
out=$(model_seg codex /tmp/nowhere)
case "$out" in
  *sol*) ok "an unmatched cwd falls back to the newest rollout" ;;
  *) bad "an unmatched cwd falls back to the newest rollout" "$out" ;;
esac
rm -f "$ROLL_MINE" "$ROLL_OTHER"

# statusline.sh must report the *running* model. settings.json only holds the
# default for new sessions, so reading it would miss every /model switch —
# exactly the case worth catching.
sl_marker() {   # sl_marker <model id> [effort level]
  printf '{"model":{"id":"%s","display_name":"D"},"effort":{"level":"%s"},"workspace":{"current_dir":"/tmp/x"}}' \
    "$1" "${2:-}" \
    | env DOTS_PANE_DIR="$PANES" TMUX_PANE='%9' sh "$STATUS_SH" >/dev/null 2>&1
  cat "$PANES/9" 2>/dev/null
}
case "$(sl_marker claude-opus-5)" in
  claude*opus) ok "statusline records the model from its payload" ;;
  *) bad "statusline records the model from its payload" "$(sl_marker claude-opus-5)" ;;
esac
case "$(sl_marker claude-haiku-4-5-20251001)" in
  claude*haiku) ok "a dated model id still reduces to its family" ;;
  *) bad "a dated model id still reduces to its family" "$(sl_marker claude-haiku-4-5-20251001)" ;;
esac
[ "$(sl_marker claude-sonnet-5 | awk -F'\t' '{print NF}')" = 3 ] \
  && ok "the marker has the three fields model.sh reads" \
  || bad "the marker has the three fields model.sh reads"

# Effort is the reasoning lever on Opus 5 / Sonnet 5, so the same model at max
# and at low are quite different amounts of spend and the bar should say which.
case "$(sl_marker claude-opus-5 xhigh)" in
  *opus/xh) ok "effort rides along with the claude model" ;;
  *) bad "effort rides along with the claude model" "$(sl_marker claude-opus-5 xhigh)" ;;
esac
# The label goes through statusline first so this tests the real string, but
# the owner has to be re-stamped: sl_marker runs inside a command substitution,
# so the pid it recorded is already gone and model.sh would rightly drop it.
printf 'claude\t%s\t%s\n' "$$" "$(sl_marker claude-opus-5 xhigh | cut -f3)" > "$PANES/9"
case "$(model_seg zsh)" in
  *'#f7768e'*opus/xh*) ok "an effort suffix does not defeat the tier colour" ;;
  *) bad "an effort suffix does not defeat the tier colour" "$(model_seg zsh)" ;;
esac

# @tsv plus `IFS=<tab> read a b c d` collapses a run of tabs, because tab is IFS
# whitespace — so one empty field shifts every later field left. With no effort
# set that put the working directory into the effort slot and rendered
# "opus//t". Empty fields are the normal case here, not an edge one.
case "$(sl_marker claude-opus-5 '')" in
  *"$(printf '\t')"opus) ok "an absent effort leaves the model alone" ;;
  *) bad "an absent effort leaves the model alone" "$(sl_marker claude-opus-5 '')" ;;
esac
out=$(printf '{"model":{"id":"claude-haiku-4-5","display_name":""},"effort":{"level":"low"},"workspace":{"current_dir":"/tmp/x"}}' \
        | env DOTS_PANE_DIR="$PANES" TMUX_PANE='%9' sh "$STATUS_SH" 2>/dev/null; cat "$PANES/9")
case "$out" in
  *haiku/lo) ok "an empty earlier field does not shift effort" ;;
  *) bad "an empty earlier field does not shift effort" "$out" ;;
esac

# settings.json is shared with a1/v1/v2, and a baked-in /Users/... path is the
# mistake the Obsidian hook already made once.
if grep -q '"\$HOME/.claude/statusline.sh"' "$REPO/common/claude/settings.json"; then
  ok "the statusLine command is \$HOME-relative"
else
  bad "the statusLine command is \$HOME-relative" \
      "a machine-specific path would break on the Linux boxes"
fi

# ── quota gauge ───────────────────────────────────────────────
#
# This is the number the whole model policy exists to move, so the failure that
# matters most is a comfortable-looking figure that is no longer true.
rm -f "$PANES/9"

write_quota 23 4200 5 500000
case "$(model_seg zsh)" in
  *'5h 23%'*) ok "quota shows on a pane with no agent in it" ;;
  *) bad "quota shows on a pane with no agent in it" "$(model_seg zsh)" ;;
esac

# The binding constraint is whichever window caps first, not whichever is
# checked first — a 7-day at 88% stops you while the 5-hour reads a calm 12%.
write_quota 12 4200 88 259200
case "$(model_seg zsh)" in
  *'7d 88%'*) ok "the window closer to capping is the one shown" ;;
  *) bad "the window closer to capping is the one shown" "$(model_seg zsh)" ;;
esac

write_quota 91 720 5 500000
out=$(model_seg zsh)
case "$out" in
  *'#f7768e'*'91%'*) ok "a nearly-spent window is coloured as an alarm" ;;
  *) bad "a nearly-spent window is coloured as an alarm" "$out" ;;
esac
case "$out" in
  *'12m'*) ok "time to reset appears once the number matters" ;;
  *) bad "time to reset appears once the number matters" "$out" ;;
esac
write_quota 23 4200 5 500000
case "$(model_seg zsh)" in
  *m*|*'23% '*) bad "a comfortable window stays quiet" "$(model_seg zsh)" ;;
  *'#565f89'*'23%'*) ok "a comfortable window stays quiet" ;;
  *) bad "a comfortable window stays quiet" "$(model_seg zsh)" ;;
esac

# Stale is worse than absent: a 20-minute-old reading presented as live is how
# you talk yourself into one more Opus session.
write_quota 91 720 5 500000 1200
[ -z "$(model_seg zsh)" ] \
  && ok "a stale quota reading is dropped, not shown" \
  || bad "a stale quota reading is dropped, not shown" "$(model_seg zsh)"

printf 'garbage\n' > "$QSTATE/claude-quota"
[ -z "$(model_seg zsh)" ] \
  && ok "a corrupt quota file prints nothing rather than junk" \
  || bad "a corrupt quota file prints nothing rather than junk" "$(model_seg zsh)"
rm -f "$QSTATE/claude-quota"
[ -z "$(model_seg zsh)" ] \
  && ok "no quota file means no segment" \
  || bad "no quota file means no segment" "$(model_seg zsh)"

# statusline.sh is the only writer; if it stops recording the burn the gauge
# goes quietly blank rather than wrong, which is easy not to notice.
printf '{"model":{"id":"claude-opus-5"},"effort":{"level":"high"},"rate_limits":{"five_hour":{"used_percentage":44,"resets_at":1900000000},"seven_day":{"used_percentage":7,"resets_at":1900000000}}}' \
  | env DOTS_PANE_DIR="$PANES" DOTS_STATE_DIR="$QSTATE" TMUX_PANE='%9' \
        sh "$STATUS_SH" >/dev/null 2>&1
[ "$(sed -n 2p "$QSTATE/claude-quota" 2>/dev/null)" = 44 ] \
  && ok "statusline records the quota it was handed" \
  || bad "statusline records the quota it was handed" \
         "$(cat "$QSTATE/claude-quota" 2>/dev/null | tr '\n' ' ')"

# A real payload carried 28.999999999999996. `test -ge` calls a float a syntax
# error, and the sanitiser that catches that read it as 0 — showing a nearly
# spent window as empty, which is the worst direction for this to be wrong in.
printf '{"model":{"id":"claude-opus-5"},"rate_limits":{"five_hour":{"used_percentage":28.999999999999996,"resets_at":1900000000},"seven_day":{"used_percentage":6.0,"resets_at":1900000000}}}' \
  | env DOTS_PANE_DIR="$PANES" DOTS_STATE_DIR="$QSTATE" TMUX_PANE='%9' \
        sh "$STATUS_SH" >/dev/null 2>&1
[ "$(sed -n 2p "$QSTATE/claude-quota" 2>/dev/null)" = 29 ] \
  && ok "a fractional percentage is rounded, not discarded" \
  || bad "a fractional percentage is rounded, not discarded" \
         "got [$(sed -n 2p "$QSTATE/claude-quota" 2>/dev/null)]"
# And a file left behind by an older statusline must still read as a number.
now=$(date +%s)
printf '%s\n28.999999999999996\n%s\n6\n%s\n' "$now" "$((now + 4200))" "$((now + 500000))" \
  > "$QSTATE/claude-quota"
case "$(model_seg zsh)" in
  *'5h 28%'*) ok "an unrounded quota file is still read as a number" ;;
  *) bad "an unrounded quota file is still read as a number" "$(model_seg zsh)" ;;
esac

# A payload with no rate_limits at all must not blank an existing reading —
# the last true number beats no number until it ages out on its own.
now=$(date +%s)
printf '%s\n44\n%s\n7\n%s\n' "$now" "$((now + 4200))" "$((now + 500000))" \
  > "$QSTATE/claude-quota"
printf '{"model":{"id":"claude-opus-5"},"effort":{"level":"high"}}' \
  | env DOTS_PANE_DIR="$PANES" DOTS_STATE_DIR="$QSTATE" TMUX_PANE='%9' \
        sh "$STATUS_SH" >/dev/null 2>&1
[ "$(sed -n 2p "$QSTATE/claude-quota" 2>/dev/null)" = 44 ] \
  && ok "a payload without rate_limits leaves the last reading alone" \
  || bad "a payload without rate_limits leaves the last reading alone"
rm -f "$QSTATE/claude-quota" "$PANES/9"

# ── resumed sessions ignore the policy ────────────────────────
#
# settings.json's "model" applies to a NEW session only. Resume one and it
# keeps whatever it was already on — verified: a haiku session resumed with no
# --model came back on haiku, not on the configured sonnet. So a session
# started before a policy change goes on ignoring it for as long as you keep
# resuming, and the long-lived sessions are the expensive ones. The hook cannot
# fix that (nothing can set the model from outside); its whole job is to stop
# it being silent, so the failure that matters is staying quiet when it should
# not — and nagging when the session is fine, which trains you to ignore it.
# ── the policy keys themselves ────────────────────────────────
#
# Claude Code ignores a setting it does not recognise AND a setting that is
# simply absent, both without a word. So a policy key dropped in an edit looks
# exactly like a policy key being honoured — which is how alwaysThinkingEnabled
# sat missing from this file for the whole rollout while everything appeared
# fine. The values are asserted, not just the keys: "model": "opus" would pass
# a presence check and undo the entire point of the file.
group "claude: the model policy is actually in the file"

SETTINGS="$REPO/common/claude/settings.json"
jq -e . "$SETTINGS" >/dev/null 2>&1 \
  && ok "settings.json is valid JSON" \
  || bad "settings.json is valid JSON" "Claude Code would fall back to defaults"

policy_key() {  # policy_key <jq path> <expected> <why it matters>
  got=$(jq -r "$1 | tostring" "$SETTINGS" 2>/dev/null)
  [ "$got" = "$2" ] \
    && ok "$1 is $2" \
    || bad "$1 is $2" "got [$got] — $3"
}
policy_key '.model' sonnet \
  "the default would go back to the dearer tier on every new session"
policy_key '.effortLevel' high \
  "effort is the only reasoning lever left on Opus 5 and Sonnet 5"
policy_key '.alwaysThinkingEnabled' null \
  "must stay absent: fbec4b3 proved false is a kill switch that disables thinking before effortLevel applies"
policy_key '.fallbackModel[0]' haiku \
  "without it an overloaded Sonnet escalates instead of falling back cheap"
policy_key '.env.CLAUDE_CODE_AUTO_COMPACT_WINDOW' 250000 \
  "contexts drift back toward 800k, and every turn re-reads the whole window"
policy_key '.advisorModel' opus \
  "fable is 5x sonnet and would turn every advisor() call into the same mistake"
policy_key '.availableModels | contains(["fable"])' false \
  "fable was 46% of spend as a deliberate main-model pick — this is what stops a repeat, not just the prose rule"
policy_key '.fastModePerSessionOptIn' true \
  "fast mode runs Opus; without this a /fast toggle can persist as the default for sessions after the one that needed it"
policy_key '.env.CLAUDE_CODE_MAX_CONCURRENT_SUBAGENTS' 3 \
  "the unset default is 20 concurrent subagents, effectively no cap at all"
policy_key '.env.CLAUDE_CODE_MAX_SUBAGENT_SPAWN_DEPTH' 1 \
  "the unset default is 3 — subagents spawning subagents spawning subagents, the cost shape agent teams warns about"

# CLAUDE_CODE_SUBAGENT_MODEL is a hard override that beats both the per-call
# model parameter and an agent's frontmatter, collapsing the whole tiering onto
# one model. Agent teams run ~7x a normal session. Neither belongs in a file
# that is symlinked onto four machines.
for forbidden in CLAUDE_CODE_SUBAGENT_MODEL CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS; do
  grep -q "$forbidden" "$SETTINGS" \
    && bad "$forbidden is not set" "it would override the per-call model tiering" \
    || ok "$forbidden is not set"
done

# Shared with a1/v1/v2, so anything naming this Mac breaks there. The Obsidian
# Stop hook made this mistake once with a baked-in /Users/... path.
if grep -q '/Users/towardinfinity' "$SETTINGS"; then
  bad "no machine-specific paths in the shared settings" \
      "a /Users/... path does not exist on the Linux boxes"
else
  ok "no machine-specific paths in the shared settings"
fi

group "claude usage: session starts survive turn-window filtering"

USAGE_ROOT="$WORK/usage/projects"
mkdir -p "$USAGE_ROOT/project"
old_ts=$(python3 -c 'import datetime; print((datetime.date.today() - datetime.timedelta(days=30)).isoformat() + "T12:00:00Z")')
new_ts=$(python3 -c 'import datetime; print(datetime.date.today().isoformat() + "T12:00:00Z")')
usage_turn() {
  printf '{"timestamp":"%s","message":{"model":"claude-sonnet-5","usage":{"input_tokens":1,"output_tokens":1}}}\n' "$1"
}
{
  usage_turn "$old_ts"
  usage_turn "$new_ts"
} > "$USAGE_ROOT/project/resumed.jsonl"
usage_turn "$new_ts" > "$USAGE_ROOT/project/fresh.jsonl"

usage_out=$(python3 "$REPO/bin/claude-usage.py" --root "$USAGE_ROOT" --days 7 \
  --policy 2000-01-01T00:00)
case "$usage_out" in
  *"sessions STARTED in window (1)"*"NOT YET VALID: 1/10"*)
    ok "an old session resumed in-window is not counted as a fresh start" ;;
  *)
    bad "an old session resumed in-window is not counted as a fresh start" "$usage_out" ;;
esac

group "claude: resumed sessions off policy"

HOOK_SH="$REPO/common/claude/session-start.sh"
HCFG="$WORK/hookcfg"; mkdir -p "$HCFG"
HT="$WORK/hook-transcript.jsonl"

hpolicy() { printf '{"model":"%s","effortLevel":"%s"}\n' "$1" "$2" > "$HCFG/settings.json"; }
hturn()   { printf '{"type":"assistant","effort":"%s","message":{"model":"%s","role":"assistant"}}\n' \
                   "$2" "$1" > "$HT"; }
hrun()    { printf '{"source":"%s","transcript_path":"%s","hook_event_name":"SessionStart"}' \
                   "${1:-resume}" "$HT" \
              | env CLAUDE_CONFIG_DIR="$HCFG" sh "$HOOK_SH" 2>/dev/null; }

hpolicy sonnet high

hturn claude-opus-5 high
case "$(hrun | jq -r '.systemMessage // ""' 2>/dev/null)" in
  *opus*sonnet*) ok "an off-policy resume says so" ;;
  *) bad "an off-policy resume says so" "$(hrun)" ;;
esac
# The multiplier is the part that changes behaviour: "opus" is a fact, "2.5x"
# is a reason. Claimed for sonnet it would just train you to skip the line.
case "$(hrun)" in
  *2.5x*) ok "the expensive tier is named with its multiplier" ;;
  *) bad "the expensive tier is named with its multiplier" "$(hrun)" ;;
esac

hturn claude-sonnet-5 high
[ -z "$(hrun)" ] \
  && ok "a resume that is already on policy stays quiet" \
  || bad "a resume that is already on policy stays quiet" "$(hrun)"

hturn claude-opus-5 high
[ -z "$(hrun startup)" ] \
  && ok "a fresh start is not second-guessed" \
  || bad "a fresh start is not second-guessed" "a new session already obeys settings.json"

# opusplan is Opus while planning and Sonnet while executing, so a session on
# either is obeying it.
hpolicy opusplan high
for m in claude-opus-5 claude-sonnet-5; do
  hturn "$m" high
  [ -z "$(hrun)" ] \
    && ok "opusplan accepts ${m#claude-}" \
    || bad "opusplan accepts ${m#claude-}" "$(hrun)"
done
hturn claude-fable-5 high
[ -n "$(hrun)" ] \
  && ok "opusplan still rejects fable" \
  || bad "opusplan still rejects fable"

# Effort is the only reasoning lever left on these models, so the right model
# at the wrong effort is still off policy.
hpolicy sonnet high
hturn claude-sonnet-5 low
case "$(hrun | jq -r '.systemMessage // ""' 2>/dev/null)" in
  *effort*low*high*) ok "the right model at the wrong effort is caught" ;;
  *) bad "the right model at the wrong effort is caught" "$(hrun)" ;;
esac

# A hook that errors breaks starting Claude at all, which is far worse than a
# missed warning. Every path has to exit 0 and print nothing rather than junk.
for bad_in in 'not json' '' '{"source":"resume","transcript_path":"/nonexistent"}'; do
  out=$(printf '%s' "$bad_in" | env CLAUDE_CONFIG_DIR="$HCFG" sh "$HOOK_SH" 2>/dev/null)
  rc=$?
  [ "$rc" = 0 ] && [ -z "$out" ] \
    && ok "malformed input [${bad_in:-<empty>}] is survived silently" \
    || bad "malformed input [${bad_in:-<empty>}] is survived silently" "rc=$rc out=$out"
done
printf 'not json at all\n' > "$HT"
[ -z "$(hrun)" ] \
  && ok "an unparseable transcript is survived silently" \
  || bad "an unparseable transcript is survived silently" "$(hrun)"

# The output is injected into the session, so malformed JSON would be a broken
# hook on every resume. jq builds it precisely so a quote cannot do that.
hturn claude-opus-5 high
hrun | jq -e '.hookSpecificOutput.hookEventName == "SessionStart"' >/dev/null 2>&1 \
  && ok "the hook emits well-formed SessionStart output" \
  || bad "the hook emits well-formed SessionStart output" "$(hrun)"

if grep -q '"\$HOME/.claude/session-start.sh"' "$REPO/common/claude/settings.json"; then
  ok "the SessionStart hook is \$HOME-relative"
else
  bad "the SessionStart hook is \$HOME-relative" \
      "an absolute path would break on the Linux boxes"
fi
grep -q 'link common/claude/session-start.sh' "$REPO/install.sh" \
  && ok "install.sh links the SessionStart hook" \
  || bad "install.sh links the SessionStart hook"

group "tmux: model indicator (cont.)"
for c in macos linux; do
  grep -q 'tmux-model' "$REPO/$c/tmux/tmux.conf" \
    && ok "$c tmux.conf calls tmux-model" \
    || bad "$c tmux.conf calls tmux-model"
done
grep -q 'link common/tmux/model.sh' "$REPO/install.sh" \
  && ok "install.sh puts tmux-model on PATH" \
  || bad "install.sh puts tmux-model on PATH"
grep -q 'link common/claude/statusline.sh' "$REPO/install.sh" \
  && ok "install.sh links statusline.sh" \
  || bad "install.sh links statusline.sh"

# The project picker uses a positional parameter and a NUL-delimited pipeline;
# the selected path never becomes part of sh -c's source. Exercise that exact
# shell shape with a hostile directory name, then pin the config syntax so a
# later refactor cannot leave the behavioural test while reintroducing `{}`.
PICKBIN="$WORK/picker-bin"; mkdir -p "$PICKBIN"
PICK_LOG="$WORK/picker.log"
cat > "$PICKBIN/tmux" <<'STUB'
#!/bin/sh
printf '%s\n' "$*" >> "$PICK_LOG"
[ "${1:-}" = has-session ] && exit 1
exit 0
STUB
chmod +x "$PICKBIN/tmux"
EVIL_PICK="$WORK/project'; touch $WORK/picker-pwned; #"
mkdir -p "$EVIL_PICK"
env PATH="$PICKBIN:$PATH" PICK_LOG="$PICK_LOG" sh -c '
  dir=$1
  n=$(basename "$dir" | tr . _)
  tmux has-session -t "$n" 2>/dev/null || tmux new-session -ds "$n" -c "$dir"
  tmux switch-client -t "$n"
' sh "$EVIL_PICK"
if [ ! -e "$WORK/picker-pwned" ] && grep -Fq -- "$EVIL_PICK" "$PICK_LOG"; then
  ok "the project picker treats shell punctuation as one path argument"
else
  bad "the project picker treats shell punctuation as one path argument" "$(cat "$PICK_LOG" 2>/dev/null)"
fi

MAC_TMUX="$REPO/macos/tmux/tmux.conf"
if grep -Fq -- '-print0' "$MAC_TMUX" \
   && grep -Fq -- 'fzf --read0 --print0' "$MAC_TMUX" \
   && grep -Fq -- 'xargs -0 -r sh -c' "$MAC_TMUX" \
   && grep -Fq -- 'dir=\$1' "$MAC_TMUX" \
   && ! grep -Fq -- '-I{} sh -c' "$MAC_TMUX"; then
  ok "the configured project picker passes a NUL-delimited positional argument"
else
  bad "the configured project picker passes a NUL-delimited positional argument"
fi

safe_jobs=true
for c in macos linux; do
  conf="$REPO/$c/tmux/tmux.conf"
  grep -Fq '#(cd #{q:pane_current_path}' "$conf" || safe_jobs=false
  grep -Fq 'tmux-model #{q:pane_id} #{q:pane_current_command}' "$conf" || safe_jobs=false
  grep 'tmux-model' "$conf" | grep -Fq "'#{pane_" && safe_jobs=false
  grep 'tmux-model' "$conf" | grep -q 'pane_current_path' && safe_jobs=false
  grep -Fq "cd '#{pane_current_path}'" "$conf" && safe_jobs=false
done
$safe_jobs \
  && ok "all four status jobs keep pane paths out of shell source" \
  || bad "all four status jobs keep pane paths out of shell source"

if [ "$(grep -c '^    permissions:$' "$REPO/.github/workflows/release.yml")" = 2 ] \
   && grep -A2 '^  test:$' "$REPO/.github/workflows/release.yml" | grep -q 'contents: read' \
   && grep -A4 '^  release:$' "$REPO/.github/workflows/release.yml" | grep -q 'contents: write' \
   && ! grep -A2 '^permissions:$' "$REPO/.github/workflows/release.yml" | grep -q 'contents: write'; then
  ok "only the release job receives contents: write"
else
  bad "only the release job receives contents: write"
fi

grep -Fq "printf '# tag=%s commit=%s\\n' \"\$VERSION\" \"\$GITHUB_SHA\"" \
  "$REPO/.github/workflows/release.yml" \
  && ok "CI puts the tag and commit in every release manifest" \
  || bad "CI puts the tag and commit in every release manifest"

# ── syntax ────────────────────────────────────────────────────
group "syntax"
for f in bootstrap.sh install.sh dots.sh bin/dots bin/dots-resolve.sh \
         common/tmux/model.sh common/claude/statusline.sh \
         common/claude/session-start.sh; do
  if [ "$f" = "install.sh" ] || [ "$f" = "bin/dots" ]; then sh_bin=bash; else sh_bin=sh; fi
  $sh_bin -n "$REPO/$f" 2>/dev/null && ok "$f parses" || bad "$f parses"
done
for f in merge-toml-block.py claude-usage.py; do
  python3 -m py_compile "$REPO/bin/$f" 2>/dev/null \
    && ok "$f parses" || bad "$f parses"
done
find "$REPO/bin" -name '__pycache__' -type d -exec rm -rf {} + 2>/dev/null

printf '\n%d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
