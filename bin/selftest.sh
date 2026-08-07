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

# Runs dots-resolve.sh against a fresh fixture set. Echoes stdout; stderr in
# $WORK/err. $1 names the scenario.
resolve() {
  rm -rf "$WORK/cache"
  env PATH="$WORK/bin:$PATH" \
      XDG_CACHE_HOME="$WORK/cache" \
      FIXTURES="$FIXTURES" FIXTURE_VERSION="$FIXTURE_VERSION" \
      DOTS_RELEASE_BASE="https://example.invalid/releases/latest/download" \
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
        sh "$REPO/bin/dots-resolve.sh" 2>"$WORK/err")"
  [ "$(sha_of "$again")" = "$GOOD_SHA" ] \
    && ok "a corrupted cached binary is re-fetched" \
    || bad "a corrupted cached binary is re-fetched" "digest still wrong"
else
  bad "a corrupted cached binary is re-fetched" "setup failed"
fi

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
mode=$(stat -f '%Lp' "$DST" 2>/dev/null || stat -c '%a' "$DST")
[ "$mode" = "600" ] && ok "an existing 0600 config stays 0600 under umask 002" \
                    || bad "an existing 0600 config stays 0600 under umask 002" "mode is $mode"

grep -q 'projects' "$DST" && ok "machine-written tables survive the merge" \
                          || bad "machine-written tables survive the merge"

# A file it creates is private, not umask-default.
NEW="$WORK/new.toml"
( umask 002; python3 "$REPO/bin/merge-toml-block.py" --src "$SRC" --dst "$NEW" --begin "$BEGIN" --end "$END" >/dev/null )
mode=$(stat -f '%Lp' "$NEW" 2>/dev/null || stat -c '%a' "$NEW")
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
