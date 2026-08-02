#!/usr/bin/env sh
#
# toin.in/dots — read the terminal setup reference on a machine that has none
# of this installed.
#
#   sh -c "$(curl -fsSL https://toin.in/dots)"                     browse
#   sh -c "$(curl -fsSL https://toin.in/dots)" -- tmux             one topic
#   sh -c "$(curl -fsSL https://toin.in/dots)" -- search clipboard search
#
# Arguments go after a bare `--`, same as the installer: the `--` is eaten by
# the outer `sh -c`, which sets it as $0 and passes the rest through.
#
# This is a launcher, not the tool. It keeps a shallow clone in the cache
# directory and runs bin/dots out of it, so there is exactly one copy of the
# docs and it is whatever is on main — no published artifact to fall behind.
#
# POSIX sh, because it runs on machines where nothing has been set up yet.

set -eu

CACHE="${XDG_CACHE_HOME:-$HOME/.cache}/toin-dots"
REPO_HTTPS="https://github.com/TowardInfinity/dotfiles.git"
MAX_AGE_HOURS=6

# Already installed? Use that. It reads the live repo, it is faster, and
# keeping a second copy alongside a real checkout is how they drift apart.
if command -v dots >/dev/null 2>&1; then
  exec dots "$@"
fi

command -v git  >/dev/null 2>&1 || { echo "dots: git is required" >&2; exit 1; }
command -v bash >/dev/null 2>&1 || { echo "dots: bash is required" >&2; exit 1; }

stale() {
  stamp="$CACHE/.git/FETCH_HEAD"
  [ -f "$stamp" ] || return 0
  # find is more portable here than stat, whose flags differ between BSD and GNU.
  [ -n "$(find "$stamp" -mmin "+$((MAX_AGE_HOURS * 60))" 2>/dev/null)" ]
}

if [ -d "$CACHE/.git" ]; then
  # Refresh at most every few hours. A failure here is not fatal — running the
  # cached copy offline is the entire point of keeping one.
  if stale; then
    git -C "$CACHE" fetch -q --depth 1 origin main 2>/dev/null \
      && git -C "$CACHE" reset -q --hard FETCH_HEAD 2>/dev/null \
      || echo "dots: could not refresh — using the cached copy" >&2
  fi
else
  mkdir -p "$(dirname "$CACHE")"
  git clone -q --depth 1 "$REPO_HTTPS" "$CACHE" \
    || { echo "dots: could not fetch the docs" >&2; exit 1; }
fi

[ -x "$CACHE/bin/dots" ] || { echo "dots: cache is broken — rm -rf $CACHE and retry" >&2; exit 1; }

exec "$CACHE/bin/dots" "$@"
