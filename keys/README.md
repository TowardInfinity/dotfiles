# Release signing keys

`release.pub` is the Ed25519 **public** key that `bin/dots-resolve.sh` verifies
`checksums.txt.sig` against, and that `dots doctor` reports the fingerprint of.
It is embedded into the `dots` binary (see `embed.go`), so a machine with no
checkout still carries the key it trusts.

The **private** key is never here, never in CI, and never in a GitHub secret.
It currently lives encrypted on one Mac and has no backup yet. `docs/signing.md`
is the single source of truth for its custody and recovery status.

## Why this directory exists with only a README in it

`//go:embed` refuses a pattern that matches nothing, so embedding `release.pub`
directly would stop the whole repo compiling on any checkout made before the key
was generated. Embedding the *directory* works either way: this README makes it
non-empty today, and `release.pub` joins it later with no code change. Doctor
reports "no signing key committed" until it does.

Nothing else belongs in here. A private key in this directory would be embedded
into a public release binary.
