#!/usr/bin/env bash
#
# Symlink this repo's configs into place, picking the right set for the OS.
#
#   ./install.sh          link everything
#   ./install.sh --dry    show what would happen, change nothing
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
DRY=false
COPY=false
for arg in "$@"; do
  case "$arg" in
    --dry)  DRY=true ;;
    --copy) COPY=true ;;
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

link() {
  local src="$REPO/$1" dst="$2"

  if [[ ! -e "$src" ]]; then
    printf '  \033[31mmissing\033[0m  %s\n' "$src"
    return 1
  fi

  if ! $COPY && [[ -L "$dst" && "$(readlink "$dst")" == "$src" ]]; then
    printf '  \033[90mok\033[0m       %s\n' "${dst/#$HOME/\~}"
    return 0
  fi

  if $DRY; then
    if [[ -e "$dst" || -L "$dst" ]]; then
      printf '  \033[33mwould back up + %s\033[0m  %s\n' "$ACTION" "${dst/#$HOME/\~}"
    else
      printf '  \033[36mwould %s\033[0m  %s\n' "$ACTION" "${dst/#$HOME/\~}"
    fi
    return 0
  fi

  mkdir -p "$(dirname "$dst")"

  if [[ -e "$dst" || -L "$dst" ]]; then
    mv "$dst" "$dst.backup.$STAMP"
    printf '  \033[33mbacked up\033[0m %s -> %s\n' "${dst/#$HOME/\~}" "$(basename "$dst").backup.$STAMP"
  fi

  if $COPY; then
    cp -R "$src" "$dst"
  else
    ln -s "$src" "$dst"
  fi
  printf '  \033[32m%s\033[0m   %s\n' "$VERB" "${dst/#$HOME/\~}"
}

echo "dotfiles: $REPO"
echo "os:       $OS"
$DRY && echo "(dry run — nothing will change)"
echo

# ── Shared ────────────────────────────────────────────────────
link common/nvim "$HOME/.config/nvim"

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

echo
if $DRY; then
  echo "Dry run complete."
  exit 0
fi

# TPM — not vendored here, fetch if absent.
TPM="$HOME/.config/tmux/plugins/tpm"
if [[ ! -d "$TPM" ]]; then
  echo "Installing TPM..."
  git clone --depth 1 -q https://github.com/tmux-plugins/tpm "$TPM"
  echo "  done — press prefix + I inside tmux to install plugins"
  echo
fi

cat <<EOF
Done. To finish:
  1. exec zsh
  2. tmux source-file ~/.config/tmux/tmux.conf     (never kill-server on a
                                                    box with live sessions)
  3. nvim                     lazy.nvim installs plugins on first launch
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
