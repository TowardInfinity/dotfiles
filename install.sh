#!/usr/bin/env bash
#
# Symlink this repo's configs into place, picking the right set for the OS.
#
#   ./install.sh          link everything
#   ./install.sh --dry    show what would happen, change nothing
#   ./install.sh --nvim   restore nvim plugins from lazy-lock.json (:Lazy restore)
#
#   common/   linked everywhere      (nvim, claude — no OS-specific anything)
#   macos/    linked on Darwin only  (zsh, tmux, ghostty)
#   linux/    linked on Linux only   (zsh, tmux — no ghostty, servers are headless)
#
# Existing files are moved aside to <path>.backup.<timestamp> — nothing is ever
# overwritten silently. Re-running is safe: correct links are left alone.

set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STAMP="$(date +%Y%m%dT%H%M%S)"
# Plain `~` in a ${var/pat/repl} replacement can be tilde-expanded by some bash
# builds; escaping it as \~ printed a literal backslash ("\~/.config/nvim").
# A variable sidesteps both.
TILDE="~"
DRY=false
COPY=false
NVIM=false
for arg in "$@"; do
  case "$arg" in
    --dry)  DRY=true ;;
    --copy) COPY=true ;;
    --nvim) NVIM=true ;;
    *) echo "Unknown option: $arg" >&2; exit 1 ;;
  esac
done

case "$(uname -s)" in
  Darwin) OS=macos ;;
  Linux)  OS=linux ;;
  *) echo "Unsupported OS: $(uname -s)" >&2; exit 1 ;;
esac

# --copy writes real files instead of symlinks, for machines where you don't
# want the repo hanging around (containers, throwaway boxes). The trade is
# real: edits are no longer tracked, and updating means re-running this — a
# symlinked install just needs `git pull`.
VERB=linked;   $COPY && VERB=copied
ACTION=link;   $COPY && ACTION=copy

MISSING=0

# How many timestamped backups to keep per destination. Symlink mode makes at
# most one (the pre-dotfiles original), so this is really about --copy, which
# had no "already correct" shortcut and so backed up a full recursive copy of
# every config on every run — on exactly the throwaway machines that are told
# to re-run in order to update.
BACKUPS_KEPT=2

prune_backups() {
  local dst="$1" old
  # Newest first, drop everything past the keep count. stderr is discarded
  # because "no backups yet" is the normal case, not an error.
  while IFS= read -r old; do
    [[ -n "$old" ]] || continue
    rm -rf "$old"
    printf '  \033[90mpruned\033[0m   %s\n' "$(basename "$old")"
  done < <(ls -1dt "$dst".backup.* 2>/dev/null | tail -n +$((BACKUPS_KEPT + 1)))
}

# Move $dst aside, never overwriting an existing backup. $STAMP has one-second
# resolution, so two runs within the same second would otherwise collide and
# the second `mv` would clobber the first run's backup.
backup_dst() {
  local dst="$1" bk="$1.backup.$STAMP" n=1
  while [[ -e "$bk" ]]; do
    bk="$dst.backup.$STAMP-$n"
    n=$((n + 1))
  done
  mv "$dst" "$bk"
  printf '  \033[33mbacked up\033[0m %s -> %s\n' "${dst/#$HOME/$TILDE}" "$(basename "$bk")"
  prune_backups "$dst"
}

link() {
  local src="$REPO/$1" dst="$2"

  # A missing source used to `return 1`, and under `set -e` a bare call to this
  # function then killed the script on the spot. One absent file left the home
  # directory half-linked — later entries never ran, TPM never installed, and
  # the closing instructions never printed, so the only clue was a single red
  # line scrolled off the top. Record it and carry on; the summary at the end
  # reports it and the exit status still reflects the failure.
  if [[ ! -e "$src" ]]; then
    printf '  \033[31mmissing\033[0m  %s\n' "$src"
    MISSING=$((MISSING + 1))
    return 0
  fi

  if ! $COPY && [[ -L "$dst" && "$(readlink "$dst")" == "$src" ]]; then
    printf '  \033[90mok\033[0m       %s\n' "${dst/#$HOME/$TILDE}"
    return 0
  fi

  # Copy mode's equivalent of the shortcut above. If what is already there is
  # byte-for-byte what we would write, it is our own previous copy: there is
  # nothing to preserve and nothing to do. This is what stops the backup pile.
  if $COPY && [[ -e "$dst" ]] && diff -rq "$src" "$dst" >/dev/null 2>&1; then
    printf '  \033[90mok\033[0m       %s\n' "${dst/#$HOME/$TILDE}"
    return 0
  fi

  if $DRY; then
    if [[ -e "$dst" || -L "$dst" ]]; then
      printf '  \033[33mwould back up + %s\033[0m  %s\n' "$ACTION" "${dst/#$HOME/$TILDE}"
    else
      printf '  \033[36mwould %s\033[0m  %s\n' "$ACTION" "${dst/#$HOME/$TILDE}"
    fi
    return 0
  fi

  mkdir -p "$(dirname "$dst")"

  if [[ -e "$dst" || -L "$dst" ]]; then
    backup_dst "$dst"
  fi

  if $COPY; then
    cp -R "$src" "$dst"
  else
    ln -s "$src" "$dst"
  fi
  printf '  \033[32m%s\033[0m   %s\n' "$VERB" "${dst/#$HOME/$TILDE}"
}

echo "dotfiles: $REPO"
echo "os:       $OS"
$DRY && echo "(dry run — nothing will change)"
echo

# ── Shared ────────────────────────────────────────────────────
link common/nvim "$HOME/.config/nvim"

# ── dots ──────────────────────────────────────────────────────
# The reference/maintenance CLI. Linked into ~/.local/bin, which both .zshrc
# files already put on PATH. It resolves back through this symlink to find the
# repo, so `dots update` works from anywhere.
link bin/dots "$HOME/.local/bin/dots"

# --copy keeps no repo, so `dots` has nothing to read its docs out of — its
# own parent directory becomes ~/.local. The docs have to travel with it.
# Symlink installs skip this: there the repo is right there and current.
if $COPY; then
  link docs "$HOME/.local/share/dots/docs"
fi

# ── Claude Code ───────────────────────────────────────────────
# Only the delegation policy is shared. Each box's ~/.claude/CLAUDE.md stays
# untracked and machine-specific (different toolchains, different guardrails)
# and pulls this in with a single `@~/.claude/delegation.md` line. settings.json
# holds machine-local permissions and is deliberately not tracked either.
link common/claude/delegation.md "$HOME/.claude/delegation.md"

# ── tmux ──────────────────────────────────────────────────────
# Linked as individual files, not the whole directory, so TPM plugins and
# resurrect snapshots keep living in ~/.config/tmux untracked.
mkdir -p "$HOME/.config/tmux"
link "$OS/tmux/tmux.conf" "$HOME/.config/tmux/tmux.conf"

# ── Shell ─────────────────────────────────────────────────────
link "$OS/zsh/.zshrc" "$HOME/.zshrc"

# ── macOS only ────────────────────────────────────────────────
if [[ $OS == macos ]]; then
  link macos/ghostty "$HOME/.config/ghostty"
fi

# A legacy ~/.tmux.conf does not shadow the XDG path (tmux 3.1+ prefers
# ~/.config/tmux/tmux.conf), but leaving one around is confusing — you edit it
# and nothing changes. Move it aside.
if [[ -f "$HOME/.tmux.conf" && ! -L "$HOME/.tmux.conf" ]]; then
  if $DRY; then
    printf '  \033[33mwould retire\033[0m  ~/.tmux.conf (superseded by the XDG path)\n'
  else
    mv "$HOME/.tmux.conf" "$HOME/.tmux.conf.backup.$STAMP"
    printf '  \033[33mretired\033[0m  ~/.tmux.conf (superseded by the XDG path)\n'
  fi
fi

if $NVIM && $DRY; then
  printf '  \033[36mwould restore\033[0m  nvim plugins from lazy-lock.json\n'
fi

report_missing() {
  (( MISSING > 0 )) || return 0
  printf '\033[31m%d source file(s) were missing\033[0m — those configs are NOT installed.\n' "$MISSING"
  echo "A partial checkout will do this; try: git -C \"$REPO\" status"
}

echo
if $DRY; then
  echo "Dry run complete."
  report_missing
  (( MISSING == 0 ))
  exit $?
fi

# TPM — not vendored here, fetch if absent.
#
# The presence check is `-d "$TPM/.git"`, not `-d "$TPM"`, and a failed clone
# cleans up after itself. A clone interrupted mid-transfer (dropped link, lid
# closed, Ctrl-C) leaves the directory behind; with the looser check that
# directory then counted as "installed" forever, so tmux.conf's `run '.../tpm'`
# silently did nothing on every start and re-running this never repaired it.
TPM="$HOME/.config/tmux/plugins/tpm"
if [[ ! -d "$TPM/.git" ]]; then
  echo "Installing TPM..."
  rm -rf "$TPM"
  if git clone --depth 1 -q https://github.com/tmux-plugins/tpm "$TPM"; then
    echo "  done — press prefix + I inside tmux to install plugins"
  else
    rm -rf "$TPM"
    printf '  \033[33mwarning\033[0m  TPM clone failed — tmux plugins will not load; re-run to retry\n'
  fi
  echo
fi

# nvim plugins — best-effort commit sync, NOT a guarantee of convergence.
#
# Know what this does and does not do. Lazy only ever runs `git checkout
# <sha>`, never `git checkout <branch>`, so the `branch` field in
# lazy-lock.json is decorative — restore does not honour it. Which branch a
# plugin tracks comes from its spec's `branch =`, or absent that, from
# whatever the remote's default branch was *when that machine first cloned
# it*. Two boxes cloning months apart can therefore sit on different
# branches with nobody running a wrong command.
#
# Worse, lazy regenerates the whole lockfile from on-disk state after every
# operation — including this one — so a plugin that is already off-branch
# gets its drift written back in as truth.
#
# The only thing that actually pins a plugin is `branch =` in the spec.
# nvim-treesitter, base46 and ui are pinned there for exactly this reason;
# anything else you care about should be too. Treat this step as "line up
# commits within the branches the specs already chose".
if $NVIM; then
  if command -v nvim >/dev/null 2>&1; then
    echo "Restoring nvim plugins from lazy-lock.json..."
    nvim --headless "+Lazy! restore" +qa
  else
    printf '  \033[33mwarning\033[0m  nvim not on PATH — skipping plugin restore\n'
  fi
fi

cat <<EOF
Done. To finish:
  1. exec zsh
  2. tmux source-file ~/.config/tmux/tmux.conf     (never kill-server on a
                                                    box with live sessions)
  3. nvim, :Lazy restore      lines plugins up with lazy-lock.json (commits
                              only — branches come from the specs; see the
                              caveat above install.sh's restore step)
  4. :MasonToolsInstall       inside nvim, installs LSPs + formatters
EOF

if [[ $OS == macos ]]; then
  echo "  Ghostty reloads its config on restart, or Cmd+R."
else
  cat <<'EOF'

Machine-specific shell bits go in ~/.zshrc.local — it is sourced if present
and stays untracked, so this repo's .zshrc is identical on every server.
EOF
fi

# Surfaced at the end rather than only at the point of failure, where a single
# red line scrolls away behind everything that follows.
if (( MISSING > 0 )); then
  echo
  report_missing
  exit 1
fi
