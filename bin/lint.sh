#!/bin/sh
set -eu

# Keep the analyzer version explicit: Staticcheck's checks and Go-version
# support move together. CI installs this exact release before invoking us.

SCRIPT_DIR=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
# shellcheck disable=SC1091
. "$SCRIPT_DIR/../tools/go-tools.env"
required="$STATICCHECK_VERSION"

staticcheck_bin=''
if command -v staticcheck >/dev/null 2>&1; then
	staticcheck_bin=$(command -v staticcheck)
fi

# The shell configs add ~/go/bin for interactive Linux sessions, but lint is
# also called from a non-login bootstrap/CI shell. Find the conventional Go
# install location even when it is not on PATH; a custom GOBIN is discovered
# when `go` is available.
if [ -z "$staticcheck_bin" ] && command -v go >/dev/null 2>&1; then
	gobin=$(go env GOBIN)
	[ -n "$gobin" ] || gobin="$(go env GOPATH)/bin"
	[ -x "$gobin/staticcheck" ] && staticcheck_bin="$gobin/staticcheck"
fi
if [ -z "$staticcheck_bin" ]; then
	for candidate in "$HOME/go/bin/staticcheck" "$HOME/.local/bin/staticcheck" /usr/local/bin/staticcheck /opt/homebrew/bin/staticcheck; do
		if [ -x "$candidate" ]; then
			staticcheck_bin="$candidate"
			break
		fi
	done
fi

if [ -z "$staticcheck_bin" ]; then
	printf '%s\n' "lint: staticcheck $required is required (run ./bin/install-go-tools.sh)" >&2
	exit 1
fi

actual=$($staticcheck_bin -version)
case "$actual" in
	*"($required)") ;;
	*)
		printf '%s\n' "lint: expected staticcheck $required, found $actual" >&2
		exit 1
		;;
esac

exec "$staticcheck_bin" ./...
