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
#   2. verify the manifest names the remote tag+commit and that every unsigned
#      input was uploaded by the release job, with the digest GitHub recorded
#   3. sign it with the offline Ed25519 key
#   4. VERIFY that signature against the committed public key, which is what
#      the fleet will use — proving the key that signed matches the key that
#      ships, before anything is published
#   5. upload checksums.txt.sig
#   6. publish the draft
#
# Step 4 is the one that matters. Signing with the wrong key is otherwise
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
command -v git >/dev/null 2>&1 || die "git is required to compare the tag's public key"
command -v openssl >/dev/null 2>&1 || die "openssl is required to sign"
[ -f "$PUBKEY" ] || die "no public key at $PUBKEY — commit it before signing anything"
[ -f "$KEY" ] || die "no private key at $KEY (override with DOTS_SIGNING_KEY)"

# Release binaries embed the public key from the tag that built them. Checking
# only the working tree here can publish a release signed by a newer key while
# its binary still reports (and its tagged source still contains) the old one.
# Keep both keys equal before the passphrase prompt, then verify the signature
# against the tag's copy below.
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
TAG_PUBKEY="$WORK/release.pub"
git show "$TAG:keys/release.pub" > "$TAG_PUBKEY" 2>/dev/null \
  || die "$TAG has no keys/release.pub — refusing to sign a release with no tagged public key"
cmp -s "$PUBKEY" "$TAG_PUBKEY" \
  || die "working-tree public key differs from $TAG:keys/release.pub — check out the tag's key before signing"

# CI built the remote tag, not whatever a local ref with the same name happens
# to point at. Compare both lightweight and annotated tags before trusting the
# tagged key or the manifest. Being offline is an explicit, noisy fail-open so
# an emergency signing run remains possible; a proved mismatch is fatal.
local_sha=$(git rev-parse "$TAG^{commit}" 2>/dev/null) \
  || die "cannot resolve $TAG to a commit"
remote_refs=""
if remote_refs=$(git ls-remote --tags origin "refs/tags/$TAG" "refs/tags/$TAG^{}" 2>/dev/null); then
  remote_sha=$(printf '%s\n' "$remote_refs" | sort -k2 | tail -1 | cut -f1)
  if [ -z "$remote_sha" ]; then
    say "warning: origin has no $TAG to compare — signing on the local tag" >&2
  elif [ "$remote_sha" != "$local_sha" ]; then
    die "local $TAG does not match origin's — CI built a different commit"
  fi
else
  say "warning: could not reach origin to compare $TAG — signing on the local tag" >&2
fi

# Refuse to touch a release that is already public. Re-signing a published
# release cannot un-publish the window it was exposed in, and silently
# replacing a signature on something machines may already have installed is
# worse than failing here.
state=$(gh release view "$TAG" --json isDraft -q .isDraft 2>/dev/null) \
  || die "no release found for $TAG"
[ "$state" = "true" ] || die "$TAG is already published — refusing to sign after the fact"

# `releases/latest/download` is what every resolver follows. Publishing an old
# draft as Latest would silently roll the fleet back to a signed but obsolete
# binary, so refuse before signing or uploading anything.
version_is_older() { # version_is_older <candidate> <current>; tags are vX.Y.Z
  local candidate="${1#v}" current="${2#v}"
  local cmaj cmin cpatch lmaj lmin lpatch
  IFS=. read -r cmaj cmin cpatch <<< "$candidate"
  IFS=. read -r lmaj lmin lpatch <<< "$current"
  [[ $cmaj =~ ^[0-9]+$ && $cmin =~ ^[0-9]+$ && $cpatch =~ ^[0-9]+$ &&
     $lmaj =~ ^[0-9]+$ && $lmin =~ ^[0-9]+$ && $lpatch =~ ^[0-9]+$ ]] \
    || die "cannot compare non-SemVer tags: $1 and $2"
  (( 10#$cmaj < 10#$lmaj )) && return 0
  (( 10#$cmaj > 10#$lmaj )) && return 1
  (( 10#$cmin < 10#$lmin )) && return 0
  (( 10#$cmin > 10#$lmin )) && return 1
  (( 10#$cpatch < 10#$lpatch )) && return 0
  return 1
}

latest=$(gh release list --limit 100 --json tagName,isLatest \
  --jq '.[] | select(.isLatest) | .tagName' 2>/dev/null) \
  || die "could not determine which release is Latest — refusing to publish"
if [ -n "$latest" ] && version_is_older "$TAG" "$latest"; then
  die "$latest currently holds Latest; refusing to publish older draft $TAG"
fi

say "downloading CI's checksums.txt from the $TAG draft"
gh release download "$TAG" --pattern checksums.txt --dir "$WORK" \
  || die "the draft has no checksums.txt — did the build finish?"

# The signature covers the manifest exactly as downloaded. Authenticate CI's
# identity claim before signing it: the first line must name this tag and the
# commit that both the local and remote tag checks above resolved to.
marker=""; tag_field=""; commit_field=""; extra=""
IFS=' ' read -r marker tag_field commit_field extra < "$WORK/checksums.txt" \
  || die "checksums.txt has no manifest header"
[ "$marker" = "#" ] && [ -z "$extra" ] \
  && [[ "$tag_field" == tag=* ]] && [[ "$commit_field" == commit=* ]] \
  || die "checksums.txt first line must be: # tag=<tag> commit=<commit>"
manifest_tag=${tag_field#tag=}
manifest_commit=${commit_field#commit=}
[ "$manifest_tag" = "$TAG" ] \
  || die "checksums.txt is for $manifest_tag, not $TAG"
[ "$manifest_commit" = "$local_sha" ] \
  || die "checksums.txt is for commit $manifest_commit, not $local_sha"

# The four binaries must be present too, or a signed checksums.txt would
# describe a release that cannot be installed.
missing=""
for a in dots_darwin_arm64 dots_darwin_amd64 dots_linux_arm64 dots_linux_amd64; do
  grep -q "  $a\$" "$WORK/checksums.txt" || missing="$missing $a"
done
[ -z "$missing" ] || die "checksums.txt does not list:$missing"

# A tag/commit header binds identity, but it is still only a claim inside the
# unsigned draft until this script approves it. Require all five CI inputs to
# have been uploaded by github-actions[bot], and compare GitHub's server-side
# digest for every binary with the manifest. A user/release token that replaces
# a draft asset before signing therefore cannot trick the offline key into
# blessing it. A compromised release workflow remains inside the trust boundary
# and is documented as such.
asset_meta=$(gh release view "$TAG" --json assets \
  --jq '.assets[] | [.name, .apiUrl, .digest] | @tsv') \
  || die "could not inspect the draft assets"
for a in checksums.txt dots_darwin_arm64 dots_darwin_amd64 dots_linux_arm64 dots_linux_amd64; do
  meta=$(printf '%s\n' "$asset_meta" | awk -F '\t' -v a="$a" '$1==a {print; exit}')
  [ -n "$meta" ] || die "the draft is missing asset $a"
  api_url=$(printf '%s\n' "$meta" | cut -f2)
  release_digest=$(printf '%s\n' "$meta" | cut -f3)
  uploader=$(gh api "$api_url" --jq .uploader.login 2>/dev/null) \
    || die "could not verify who uploaded $a"
  [ "$uploader" = "github-actions[bot]" ] \
    || die "$a was uploaded by $uploader, not github-actions[bot] — refusing to sign mutable draft assets"
  if [ "$a" != checksums.txt ]; then
    expected=$(awk -v f="$a" '$2==f {print $1; exit}' "$WORK/checksums.txt")
    [ "$release_digest" = "sha256:$expected" ] \
      || die "GitHub's digest for $a does not match checksums.txt"
  fi
done

say "signing (the key's passphrase is required)"
openssl pkeyutl -sign -inkey "$KEY" -rawin \
  -in "$WORK/checksums.txt" -out "$WORK/checksums.txt.sig" \
  || die "signing failed"

say "verifying against $TAG's public key"
openssl pkeyutl -verify -pubin -inkey "$TAG_PUBKEY" -rawin \
  -in "$WORK/checksums.txt" -sigfile "$WORK/checksums.txt.sig" >/dev/null 2>&1 \
  || die "the signature does not verify against $TAG:keys/release.pub — wrong key; nothing was published"

say "uploading checksums.txt.sig"
gh release upload "$TAG" "$WORK/checksums.txt.sig" --clobber

say "publishing $TAG"
gh release edit "$TAG" --draft=false --latest

printf '\n\033[32mdone\033[0m  %s is published and signed (%d assets)\n' \
  "$TAG" "$(gh release view "$TAG" --json assets -q '.assets | length')"
