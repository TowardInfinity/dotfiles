#!/usr/bin/env bash
#
# Symlink this repo's configs into place.
#
#   ./install.sh          link everything
#   ./install.sh --dry    show what would happen, change nothing
#
# Existing files are moved aside to <path>.backup.<timestamp> — nothing is
# ever overwritten silently. Re-running is safe: correct links are left alone.

set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STAMP="$(date +%Y%m%dT%H%M%S)"
DRY=false
[[ "${1:-}" == "--dry" ]] && DRY=true

link() {
  local src="$REPO/$1" dst="$2"

  if [[ ! -e "$src" ]]; then
    printf '  \033[31mmissing\033[0m  %s\n' "$src"
    return 1
  fi

  # Already pointing where we want it
  if [[ -L "$dst" && "$(readlink "$dst")" == "$src" ]]; then
    printf '  \033[90mok\033[0m       %s\n' "$dst"
    return 0
  fi

  if $DRY; then
    if [[ -e "$dst" || -L "$dst" ]]; then
      printf '  \033[33mwould backup + link\033[0m  %s\n' "$dst"
    else
      printf '  \033[36mwould link\033[0m  %s\n' "$dst"
    fi
    return 0
  fi

  mkdir -p "$(dirname "$dst")"

  if [[ -e "$dst" || -L "$dst" ]]; then
    mv "$dst" "$dst.backup.$STAMP"
    printf '  \033[33mbacked up\033[0m  %s -> %s\n' "$dst" "$dst.backup.$STAMP"
  fi

  ln -s "$src" "$dst"
  printf '  \033[32mlinked\033[0m   %s\n' "$dst"
}

echo "dotfiles: $REPO"
$DRY && echo "(dry run — nothing will change)"
echo

link nvim        "$HOME/.config/nvim"
link ghostty     "$HOME/.config/ghostty"
link zsh/.zshrc  "$HOME/.zshrc"

# tmux: link the two config files individually rather than the whole directory,
# so TPM plugins and resurrect snapshots keep living in ~/.config/tmux
# untracked, instead of landing inside the repo.
mkdir -p "$HOME/.config/tmux"
link tmux/tmux.conf     "$HOME/.config/tmux/tmux.conf"
link tmux/vm-tmux.conf  "$HOME/.config/tmux/vm-tmux.conf"

echo
if $DRY; then
  echo "Dry run complete."
  exit 0
fi

# TPM — tmux's plugin manager isn't vendored here, so fetch it if absent.
TPM="$HOME/.config/tmux/plugins/tpm"
if [[ ! -d "$TPM" ]]; then
  echo "Installing TPM..."
  git clone --depth 1 -q https://github.com/tmux-plugins/tpm "$TPM"
  echo "  done — press prefix + I inside tmux to install plugins"
fi

cat <<'EOF'

Done. To finish:
  1. exec zsh                       reload the shell
  2. tmux source-file ~/.config/tmux/tmux.conf
  3. nvim                           lazy.nvim installs plugins on first launch
  4. :MasonToolsInstall             inside nvim, installs LSPs + formatters

Ghostty picks up its config on restart (or Cmd+R to reload).
EOF
