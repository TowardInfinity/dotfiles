// Package dotfiles exists only to carry the docs into the binary.
//
// The go:embed directive cannot reach outside the package directory, so this lives at the
// repo root rather than beside the command. The binary is then self-contained:
// it works on a machine with no checkout, which is exactly the machine you are
// most likely to be reading the reference on.
package dotfiles

import "embed"

//go:embed docs/*.md
var DocsFS embed.FS

// KeysFS carries the release-signing public key so `dots doctor` can report
// which key this binary trusts without needing a checkout.
//
// The directory is embedded rather than keys/release.pub itself: go:embed
// rejects a pattern matching no files, and the key is generated offline after
// this code. Embedding the directory compiles both before and after it lands.
// keys/README.md explains the rest.
//
//go:embed keys
var KeysFS embed.FS
