---
title: Signing
group: Maintain
order: 25
summary: The offline release key — where it lives, how to sign, and how to recover it
---

# Release signing

## What the signature is for

Every machine here installs `dots` by downloading a binary from a GitHub
release and checking it against `checksums.txt` from **that same release**.
That proves the download was not corrupted in transit. It proves nothing about
who published it: anything able to replace the binary — a stolen token, a
compromised Actions run, a bad workflow edit — can replace its checksum line in
the same breath, and four machines would install it without a complaint.

The detached Ed25519 signature closes that. The private key never touches
GitHub. It is generated offline, stored encrypted, and used by hand. So a
release the fleet will execute requires something GitHub does not have.

`openssl` is the verifier because it is the only such tool present on every
machine here — the low-memory boxes have no `gh`, no `cosign`, no `minisign`.

## Where the key lives

| | |
|---|---|
| Private key | `~/.config/dots/release-key.pem` on the Mac, encrypted (AES-256) |
| Passphrase | password manager, nowhere else |
| Backup | two encrypted copies on separate removable media, in different places |
| Public key | `keys/release.pub`, committed, embedded in the binary |

Neither store can sign on its own: the media hold ciphertext, the password
manager holds a passphrase with nothing to open. Losing either one is
survivable. Losing both is the recovery path below.

## Generating it (once)

```sh
mkdir -p ~/.config/dots
openssl genpkey -algorithm ed25519 -aes256 -out ~/.config/dots/release-key.pem
chmod 600 ~/.config/dots/release-key.pem
openssl pkey -in ~/.config/dots/release-key.pem -pubout -out keys/release.pub
```

Copy the private key to both removable volumes, then — **before committing the
public key** — prove each copy actually works:

```sh
printf 'test' > /tmp/t
openssl pkeyutl -sign -inkey /Volumes/<media>/release-key.pem -rawin \
  -in /tmp/t -out /tmp/t.sig
openssl pkeyutl -verify -pubin -inkey keys/release.pub -rawin \
  -in /tmp/t -sigfile /tmp/t.sig
```

Both must print `Signature Verified Successfully`. A backup that has never been
read is not a backup, and the moment the public key is committed is the moment
losing the private one becomes expensive.

Then commit `keys/release.pub`. `dots doctor` prints its fingerprint, which you
can check against openssl directly:

```sh
openssl pkey -pubin -in keys/release.pub -outform DER \
  | openssl dgst -sha256 -binary | base64
```

## Cutting a signed release

CI builds the four binaries and `checksums.txt` and leaves them as a **draft**.
Drafts are excluded from `releases/latest/download` — that path always redirects
into the newest *published* release, and a draft's own asset URL is 404
anonymously. So no unsigned release is ever reachable by the fleet.

```sh
git tag v0.1.11 && git push origin v0.1.11   # CI builds a draft
./bin/sign-release.sh v0.1.11                # sign, verify, upload, publish
dots sync                                     # roll it out
```

`sign-release.sh` downloads **CI's own** `checksums.txt` rather than rebuilding
locally — Go output is not guaranteed bit-identical across machines, and signing
a local rebuild would sign bytes nobody tested. It then verifies its own
signature against the committed public key before publishing, so signing with
the wrong key fails here instead of failing simultaneously on four machines.

A signed release has **six** assets: four binaries, `checksums.txt`, and
`checksums.txt.sig`.

## The two modes

`bin/dots-resolve.sh` reads `DOTS_SIGNATURE_MODE`:

| | |
|---|---|
| `warn` (default) | verify when a signature is present; allow unsigned, loudly |
| `require` | a valid signature or no binary |

The order matters and is not cosmetic. A resolver that does not check
signatures ignores them, so the *verifying* resolver has to reach every machine
before signatures become mandatory:

- **v0.1.11** ships `warn` and is itself signed. Machines still running the old
  resolver install it on checksum alone; that is the point.
- **v0.1.12** flips the default to `require`. From then on a tampered or
  unsigned release fails closed — tier 2 declines and the resolver falls through
  to a local `go build` or the checked-in `bin/dots`.

Flipping to `require` before the fleet is on 11 would strand the low-memory
boxes, which cannot `go build` under their memory guard.

A signature that is *present and wrong* is refused in **both** modes. The only
difference between them is what a missing signature means.

## Recovery

### Planned rotation

1. Generate the new key alongside the old one.
2. Cut a compatibility release that trusts **both** keys, signed with the
   **old** one — machines still verify it with the key they already have.
3. Roll it out (`dots sync`), confirm `dots doctor` shows the new fingerprint
   everywhere.
4. Sign the next release with the new key and retire the old.

Skipping step 2 means every machine rejects the release carrying its own
replacement key.

### Key loss

If both removable copies and the passphrase are gone, the old key is
unrecoverable and nothing signed by it can be reproduced. Nothing already
installed breaks — the failure is only that no *new* release verifies.

1. Generate a replacement key and commit the new `keys/release.pub`.
2. Sign the next release with it.
3. On each machine run **`dots update` first**. `dots update` is a `git pull`
   followed by `install.sh`, so the pull brings the new public key down before
   `install.sh` asks the resolver to fetch that release. In the other order the
   machine verifies a new release against the old key and refuses it.

Dry-run step 3 on one machine before doing it on all four. The failure mode is
recoverable but tedious: fall back to `DOTS_SIGNATURE_MODE=warn` for one
update, or run `bin/dots` directly from the checkout.
