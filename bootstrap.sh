#!/usr/bin/env bash
#
# One-shot bootstrap: clone (or update) this repo, optionally install the
# packages the configs expect, then symlink everything.
#
#   ./bootstrap.sh            clone/update + link
#   ./bootstrap.sh --deps     ...and install packages first (brew / apt)
#   ./bootstrap.sh --dry      show what would happen, change nothing
#
# The repo is PRIVATE, so this needs working GitHub auth before it can clone:
# either an SSH key on the machine, or `gh auth login`. See README.
#
# Destination defaults per OS; override with DOTFILES_DIR=/some/path.

set -euo pipefail

REPO_SLUG="TowardInfinity/dotfiles"
REPO_SSH="git@github.com:${REPO_SLUG}.git"

case "$(uname -s)" in
  Darwin) OS=macos; DEFAULT_DIR="$HOME/Codes/Projects/dotfiles" ;;
  Linux)  OS=linux; DEFAULT_DIR="$HOME/Codes/dotfiles" ;;
  *) echo "Unsupported OS: $(uname -s)" >&2; exit 1 ;;
esac
DEST="${DOTFILES_DIR:-$DEFAULT_DIR}"

DEPS=false
DRY=false
for arg in "$@"; do
  case "$arg" in
    --deps) DEPS=true ;;
    --dry)  DRY=true ;;
    -h|--help) sed -n '2,14p' "$0" | sed 's/^# \?//'; exit 0 ;;
    *) echo "Unknown option: $arg" >&2; exit 1 ;;
  esac
done

say()  { printf '\033[36m==>\033[0m %s\n' "$1"; }
warn() { printf '\033[33m!!\033[0m  %s\n' "$1"; }

# ── Preflight ─────────────────────────────────────────────────
command -v git >/dev/null || { echo "git is required" >&2; exit 1; }

say "os=$OS  dest=$DEST"
$DRY && warn "dry run — nothing will change"

# ── Dependencies (opt-in) ─────────────────────────────────────
if $DEPS; then
  say "Installing packages"
  if [[ $OS == macos ]]; then
    if ! command -v brew >/dev/null; then
      warn "Homebrew missing — install it first: https://brew.sh"
      exit 1
    fi
    PKGS=(neovim tmux fzf zoxide eza bat lazygit ripgrep git)
    if $DRY; then
      echo "    would: brew install ${PKGS[*]}"
      echo "    would: brew install --cask ghostty font-jetbrains-mono-nerd-font"
    else
      brew install "${PKGS[@]}"
      brew install --cask ghostty font-jetbrains-mono-nerd-font || true
    fi
  else
    # Ubuntu/Debian. Neovim from apt is usually far too old for this config,
    # so it is fetched as the upstream tarball instead.
    PKGS=(tmux zsh git curl build-essential python3-venv)
    if $DRY; then
      echo "    would: sudo apt-get install -y ${PKGS[*]}"
      echo "    would: install neovim from the upstream tarball if <0.11"
    else
      sudo apt-get update -qq
      sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq "${PKGS[@]}"
      # python3-venv is needed for Mason to install ruff.
      NEED_NVIM=true
      if command -v nvim >/dev/null; then
        v=$(nvim --version | head -1 | grep -oE '[0-9]+\.[0-9]+' | head -1)
        [[ ${v%%.*} -gt 0 || ${v#*.} -ge 11 ]] && NEED_NVIM=false
      fi
      if $NEED_NVIM; then
        say "Installing Neovim (apt's is too old)"
        tmp=$(mktemp -d)
        curl -fsSL -o "$tmp/nvim.tar.gz" \
          https://github.com/neovim/neovim/releases/latest/download/nvim-linux-x86_64.tar.gz
        sudo tar -C /opt -xzf "$tmp/nvim.tar.gz"
        sudo ln -sf /opt/nvim-linux-x86_64/bin/nvim /usr/local/bin/nvim
        rm -rf "$tmp"
      fi
    fi
  fi

  if [[ ! -d "$HOME/.oh-my-zsh" ]]; then
    if $DRY; then
      echo "    would: install Oh My Zsh"
    else
      say "Installing Oh My Zsh"
      RUNZSH=no CHSH=no sh -c \
        "$(curl -fsSL https://raw.githubusercontent.com/ohmyzsh/ohmyzsh/master/tools/install.sh)"
    fi
  fi
fi

# ── Clone or update ───────────────────────────────────────────
if [[ -d "$DEST/.git" ]]; then
  say "Updating $DEST"
  $DRY || git -C "$DEST" pull --ff-only
else
  say "Cloning $REPO_SLUG -> $DEST"
  if $DRY; then
    echo "    would: git clone $REPO_SSH $DEST"
  else
    mkdir -p "$(dirname "$DEST")"
    if ! git clone "$REPO_SSH" "$DEST" 2>/dev/null; then
      warn "SSH clone failed — the repo is private."
      if command -v gh >/dev/null && gh auth status >/dev/null 2>&1; then
        say "Falling back to gh"
        gh repo clone "$REPO_SLUG" "$DEST"
      else
        cat >&2 <<EOF

Cannot clone a private repo without auth. Do one of:

  gh auth login                       # then re-run this script
  ssh-keygen -t ed25519 && gh ssh-key add ~/.ssh/id_ed25519.pub

Verify with:  ssh -T git@github.com
EOF
        exit 1
      fi
    fi
  fi
fi

# ── Link ──────────────────────────────────────────────────────
say "Linking configs"
if $DRY; then
  [[ -x "$DEST/install.sh" ]] && "$DEST/install.sh" --dry || echo "    (repo not present yet — install.sh would run here)"
else
  "$DEST/install.sh"
fi
