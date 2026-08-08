#!/usr/bin/env bash
# Sign a draft release and publish it.
#
#   ./bin/sign-release.sh v0.1.11
#
# The private key never reaches GitHub. CI builds the binaries, computes
# checksums.txt and leaves the whole thing as a DRAFT — drafts are excluded
# from /releases/latest/download (verified empirically: that path always
# redirects into the newest PUBLISHED release, and a draft's own asset URL is
# 404 anonymously). So nothing the fleet can reach exists until this script
# runs. There is no window in which an unsigned release is Latest.
#
# What this does:
#
#   1. download CI's own checksums.txt from the draft — not a local rebuild,
#      because Go output is not guaranteed bit-identical across machines and
#      signing a locally-rebuilt artifact would sign bytes nobody tested
#   2. sign it with the offline Ed25519 key
#   3. VERIFY that signature against the committed public key, which is what
#      the fleet will use — proving the key that signed matches the key that
#      ships, before anything is published
#   4. upload checksums.txt.sig
#   5. publish the draft
#
# Step 3 is the one that matters. Signing with the wrong key is otherwise
# silent until four machines simultaneously refuse to update.
set -euo pipefail

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PUBKEY="$REPO_DIR/keys/release.pub"
KEY="${DOTS_SIGNING_KEY:-$HOME/.config/dots/release-key.pem}"

die() { printf '\033[31merror\033[0m  %s\n' "$*" >&2; exit 1; }
say() { printf '\033[36m==>\033[0m %s\n' "$*"; }

TAG="${1:-}"
[ -n "$TAG" ] || die "usage: sign-release.sh <tag>   (e.g. v0.1.11)"

command -v gh >/dev/null 2>&1 || die "gh is required to read and publish the draft"
command -v openssl >/dev/null 2>&1 || die "openssl is required to sign"
[ -f "$PUBKEY" ] || die "no public key at $PUBKEY — commit it before signing anything"
[ -f "$KEY" ] || die "no private key at $KEY (override with DOTS_SIGNING_KEY)"

# Refuse to touch a release that is already public. Re-signing a published
# release cannot un-publish the window it was exposed in, and silently
# replacing a signature on something machines may already have installed is
# worse than failing here.
state=$(gh release view "$TAG" --json isDraft -q .isDraft 2>/dev/null) \
  || die "no release found for $TAG"
[ "$state" = "true" ] || die "$TAG is already published — refusing to sign after the fact"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

say "downloading CI's checksums.txt from the $TAG draft"
gh release download "$TAG" --pattern checksums.txt --dir "$WORK" \
  || die "the draft has no checksums.txt — did the build finish?"

# The four binaries must be present too, or a signed checksums.txt would
# describe a release that cannot be installed.
missing=""
for a in dots_darwin_arm64 dots_darwin_amd64 dots_linux_arm64 dots_linux_amd64; do
  grep -q "  $a\$" "$WORK/checksums.txt" || missing="$missing $a"
done
[ -z "$missing" ] || die "checksums.txt does not list:$missing"

have=$(gh release view "$TAG" --json assets -q '[.assets[].name] | join(" ")')
for a in dots_darwin_arm64 dots_darwin_amd64 dots_linux_arm64 dots_linux_amd64; do
  case " $have " in *" $a "*) : ;; *) die "the draft is missing asset $a" ;; esac
done

say "signing (the key's passphrase is required)"
openssl pkeyutl -sign -inkey "$KEY" -rawin \
  -in "$WORK/checksums.txt" -out "$WORK/checksums.txt.sig" \
  || die "signing failed"

say "verifying against the COMMITTED public key"
openssl pkeyutl -verify -pubin -inkey "$PUBKEY" -rawin \
  -in "$WORK/checksums.txt" -sigfile "$WORK/checksums.txt.sig" >/dev/null 2>&1 \
  || die "the signature does not verify against $PUBKEY — wrong key; nothing was published"

say "uploading checksums.txt.sig"
gh release upload "$TAG" "$WORK/checksums.txt.sig" --clobber

say "publishing $TAG"
gh release edit "$TAG" --draft=false --latest

printf '\n\033[32mdone\033[0m  %s is published and signed (%d assets)\n' \
  "$TAG" "$(gh release view "$TAG" --json assets -q '.assets | length')"
