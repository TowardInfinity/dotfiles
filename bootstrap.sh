#!/usr/bin/env bash
#
# One-shot bootstrap: clone (or update) this repo, optionally install the
# packages the configs expect, then symlink everything.
#
#   ./bootstrap.sh            clone/update + symlink   (recommended)
#   ./bootstrap.sh --deps     ...and install packages first (brew / apt)
#   ./bootstrap.sh --dry      show what would happen, change nothing
#   ./bootstrap.sh --copy     no repo kept: fetch a tarball, copy files, discard
#
# Prefer the default. The symlinks are what make ~/.config/nvim and the repo
# the same files, so edits are tracked and updating is `git pull`. --copy
# breaks that on purpose, for machines you will throw away.
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
COPY=false
for arg in "$@"; do
  case "$arg" in
    --deps) DEPS=true ;;
    --dry)  DRY=true ;;
    --copy) COPY=true ;;
    -h|--help) sed -n '2,16p' "$0" | sed 's/^# \?//'; exit 0 ;;
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

# ── No-clone mode ─────────────────────────────────────────────
# Fetch a tarball, copy the configs into place, delete the tarball. Nothing is
# left behind. Good for containers and boxes you will throw away; bad for one
# you actually work on, because the configs stop being tracked and updating
# means re-running this instead of `git pull`.
if $COPY; then
  say "Copy mode — no repo will be kept"
  if $DRY; then
    echo "    would: fetch tarball via gh, copy configs into place, discard it"
    exit 0
  fi
  command -v gh >/dev/null || { echo "copy mode needs the gh CLI (private repo)" >&2; exit 1; }
  gh auth status >/dev/null 2>&1 || { echo "run: gh auth login" >&2; exit 1; }

  TMP=$(mktemp -d)
  trap 'rm -rf "$TMP"' EXIT
  say "Fetching $REPO_SLUG"
  gh api "repos/${REPO_SLUG}/tarball/main" > "$TMP/d.tar.gz"
  tar -xzf "$TMP/d.tar.gz" -C "$TMP"
  SRC=$(find "$TMP" -mindepth 1 -maxdepth 1 -type d | head -1)
  [[ -d "$SRC" ]] || { echo "tarball did not extract as expected" >&2; exit 1; }

  say "Copying configs"
  "$SRC/install.sh" --copy
  echo
  warn "Copied, not linked — these files are no longer tracked."
  warn "Re-run this command to update them."
  exit 0
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
