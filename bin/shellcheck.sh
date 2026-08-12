#!/bin/sh
set -eu

if ! command -v shellcheck >/dev/null 2>&1; then
	printf '%s\n' 'shellcheck: ShellCheck is required (install it with --deps or your package manager)' >&2
	exit 1
fi

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
# The repository's shell tests intentionally use compact &&/|| assertions and
# a few dynamic-source patterns. Keep CI focused on ShellCheck's error class;
# style/info warnings are reviewed separately without making every test line a
# false red release gate.
exec shellcheck --severity=error \
	"$SCRIPT_DIR/../bootstrap.sh" \
	"$SCRIPT_DIR/../install.sh" \
	"$SCRIPT_DIR/../dots.sh" \
	"$SCRIPT_DIR/dots-resolve.sh" \
	"$SCRIPT_DIR/install-go-tools.sh" \
	"$SCRIPT_DIR/lint.sh" \
	"$SCRIPT_DIR/selftest.sh" \
	"$SCRIPT_DIR/sign-release.sh"
