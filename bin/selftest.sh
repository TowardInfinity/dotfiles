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
  env PATH="$WORK/bin:$PATH" \
      XDG_CACHE_HOME="$WORK/cache" \
      FIXTURES="$FIXTURES" FIXTURE_VERSION="$FIXTURE_VERSION" \
      DOTS_RELEASE_BASE="https://example.invalid/releases/latest/download" \
      DOTS_SIGNATURE_MODE=warn \
      DOTS_FORCE_FETCH=1 DOTS_NO_BUILD=1 \
      sh "$REPO/bin/dots-resolve.sh" 2>"$WORK/err"
}

# ── resolver: verification is mandatory ───────────────────────
group "dots-resolve: unverifiable downloads are refused"

FIXTURE_VERSION="v9.9.9"
goos=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$(uname -m)" in x86_64|amd64) goarch=amd64 ;; aarch64|arm64) goarch=arm64 ;; *) goarch=unknown ;; esac
ASSET="dots_${goos}_${goarch}"

FIXTURES="$WORK/fx"; mkdir -p "$FIXTURES"
printf '#!/bin/sh\necho fake\n' > "$FIXTURES/$ASSET"
GOOD_SHA="$(sha_of "$FIXTURES/$ASSET")"

# 1. Everything present and correct -> accepted.
printf '%s  %s\n' "$GOOD_SHA" "$ASSET" > "$FIXTURES/checksums.txt"
got="$(resolve)"
[ -n "$got" ] && [ -x "$got" ] \
  && ok "a correctly-signed download is accepted" \
  || bad "a correctly-signed download is accepted" "got '$got'; stderr: $(tail -1 "$WORK/err")"

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
printf '%s  %s\n' "$GOOD_SHA" "some_other_asset" > "$FIXTURES/checksums.txt"
refused "asset missing from checksums.txt is refused" "not listed" "$(resolve)"

# 4. Digest disagrees. This one always worked; it is here so a future
#    refactor cannot quietly lose it.
printf '%s  %s\n' "0000000000000000000000000000000000000000000000000000000000000000" "$ASSET" \
  > "$FIXTURES/checksums.txt"
refused "checksum mismatch is refused" "mismatch" "$(resolve)"

# 5. A corrupted cache is discarded rather than reused forever.
printf '%s  %s\n' "$GOOD_SHA" "$ASSET" > "$FIXTURES/checksums.txt"
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
  env PATH="$WORK/bin:$PATH" \
      XDG_CACHE_HOME="$WORK/cache" \
      FIXTURES="$FIXTURES" FIXTURE_VERSION="$FIXTURE_VERSION" \
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

if sig_supported; then
  # Restore the good fixture set (test 5 above left the cache tampered).
  printf '#!/bin/sh\necho fake\n' > "$FIXTURES/$ASSET"
  GOOD_SHA="$(sha_of "$FIXTURES/$ASSET")"
  printf '%s  %s\n' "$GOOD_SHA" "$ASSET" > "$FIXTURES/checksums.txt"
  sign_fixture() {
    openssl pkeyutl -sign -inkey "$WORK/sigkey.pem" -rawin \
      -in "$FIXTURES/checksums.txt" -out "$FIXTURES/checksums.txt.sig" 2>/dev/null
  }
  sign_fixture

  # 1/2. A correct signature is accepted, and require mode must not be
  #      stricter than the signature actually being valid.
  accepted "a validly signed release is accepted (warn)"    "$(resolve_sig warn)"
  accepted "a validly signed release is accepted (require)" "$(resolve_sig require)"

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
  printf '%s  %s\n' \
    "1111111111111111111111111111111111111111111111111111111111111111" "$ASSET" \
    > "$FIXTURES/checksums.txt"
  refused "a signature over different bytes is refused (warn)" "FAILS" "$(resolve_sig warn)"
  refused "a signature over different bytes is refused (require)" "FAILS" "$(resolve_sig require)"

  # 6. Malformed — not 64 bytes, not a signature at all. Must fail closed
  #    (proven wrong), not open as "could not ask".
  printf '%s  %s\n' "$GOOD_SHA" "$ASSET" > "$FIXTURES/checksums.txt"
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
  printf '%s  %s\n' "$GOOD_SHA" "$ASSET" > "$FIXTURES/checksums.txt"
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

if [ "$(uname -s)" = "Darwin" ]; then
  env HOME="$DRYHOME" sh "$REPO/bootstrap.sh" --light --dry >/dev/null 2>&1
  [ $? -ne 0 ] && ok "--light is rejected on macOS" \
               || bad "--light is rejected on macOS" "it was accepted"
else
  $VERBOSE && printf '  \033[90mskip\033[0m  --light macOS guard (not on Darwin)\n'
fi

# ── syntax ────────────────────────────────────────────────────
group "syntax"
for f in bootstrap.sh install.sh dots.sh bin/dots bin/dots-resolve.sh; do
  if [ "$f" = "install.sh" ] || [ "$f" = "bin/dots" ]; then sh_bin=bash; else sh_bin=sh; fi
  $sh_bin -n "$REPO/$f" 2>/dev/null && ok "$f parses" || bad "$f parses"
done
python3 -m py_compile "$REPO/bin/merge-toml-block.py" 2>/dev/null \
  && ok "merge-toml-block.py parses" || bad "merge-toml-block.py parses"
find "$REPO/bin" -name '__pycache__' -type d -exec rm -rf {} + 2>/dev/null

printf '\n%d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
