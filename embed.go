// Package dotfiles exists only to carry the docs into the binary.
//
// go:embed cannot reach outside the package directory, so this lives at the
// repo root rather than beside the command. The binary is then self-contained:
// it works on a machine with no checkout, which is exactly the machine you are
// most likely to be reading the reference on.
package dotfiles

import "embed"

//go:embed docs/*.md
var DocsFS embed.FS
