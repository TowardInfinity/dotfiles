#!/usr/bin/env sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
# shellcheck disable=SC1091
. "$SCRIPT_DIR/../tools/go-tools.env"

go_bin=$(command -v go 2>/dev/null || true)
if [ -z "$go_bin" ] && [ -x /usr/local/go/bin/go ]; then
	go_bin=/usr/local/go/bin/go
fi
if [ -z "$go_bin" ] && [ -x /opt/homebrew/bin/go ]; then
	go_bin=/opt/homebrew/bin/go
fi
if [ -z "$go_bin" ]; then
	printf '%s\n' 'go-tools: Go is required to install repository development tools' >&2
	exit 1
fi

gobin=$($go_bin env GOBIN)
if [ -z "$gobin" ]; then
	gobin="$($go_bin env GOPATH)/bin"
fi
target="$gobin/staticcheck"

if [ -x "$target" ]; then
	actual=$($target -version 2>/dev/null || true)
	case "$actual" in
		*"($STATICCHECK_VERSION)")
			printf 'go-tools: staticcheck %s already installed at %s\n' "$STATICCHECK_VERSION" "$target"
			exit 0
			;;
	esac
fi

printf 'go-tools: installing staticcheck %s\n' "$STATICCHECK_VERSION"
"$go_bin" install "$STATICCHECK_MODULE@$STATICCHECK_VERSION"

if [ ! -x "$target" ]; then
	printf 'go-tools: go install completed but %s was not created\n' "$target" >&2
	exit 1
fi
printf 'go-tools: installed %s\n' "$target"
