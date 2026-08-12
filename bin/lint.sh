#!/bin/sh
set -eu

# Keep the analyzer version explicit: Staticcheck's checks and Go-version
# support move together. CI installs this exact release before invoking us.
required='v0.7.0'
if ! command -v staticcheck >/dev/null 2>&1; then
	printf '%s\n' "lint: staticcheck $required is required (go install honnef.co/go/tools/cmd/staticcheck@$required)" >&2
	exit 1
fi

actual=$(staticcheck -version)
case "$actual" in
	*"($required)") ;;
	*)
		printf '%s\n' "lint: expected staticcheck $required, found $actual" >&2
		exit 1
		;;
esac

exec staticcheck ./...
