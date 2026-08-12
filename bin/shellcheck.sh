#!/bin/sh
set -eu

if ! command -v shellcheck >/dev/null 2>&1; then
	printf '%s\n' 'shellcheck: ShellCheck is required (install it with --deps or your package manager)' >&2
	exit 1
fi

SCRIPT_DIR=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
# Discover the scripts rather than maintaining a second hand-written list.
# The three top-level entry points are explicit because they live outside
# bin/; every future bin/*.sh is picked up automatically.
set -- \
	"$SCRIPT_DIR/../bootstrap.sh" \
	"$SCRIPT_DIR/../install.sh" \
	"$SCRIPT_DIR/../dots.sh"
for script in "$SCRIPT_DIR"/*.sh; do
	set -- "$@" "$script"
done

# Warning-level findings catch word-splitting and command-resolution mistakes,
# not just syntax errors. Intentional patterns are narrowly marked in place.
exec shellcheck --severity=warning "$@"
