#!/usr/bin/env sh
#
# Bootstrap TowardInfinity's dotfiles onto a new machine.
#
#   sh -c "$(curl -fsSL https://toin.in/install)"
#
# That URL is a Cloudflare Worker route on the toin.in site that proxies this
# very file. Keeping the public entry point behind a Worker is what lets this
# script be renamed or moved without breaking a command already in someone's
# shell history. /install.sh still works and serves the same bytes.
#
# This repo is public, so installing needs no GitHub account at all — it works
# on a box you will never log into GitHub from. If the machine *does* have an
# SSH key on the account, the clone uses SSH instead so you can push config
# changes back; otherwise it clones read-only over HTTPS. It never stops to ask.
#
# Options go after a bare `--`, e.g.
#   sh -c "$(curl -fsSL https://toin.in/install)" -- --deps
#
#   --deps    also install packages: brew on macOS, apt + Neovim tarball on Ubuntu
#   --copy    don't keep a repo — fetch a tarball, copy configs, discard it
#   --dry     print what would happen, change nothing
#   --nvim    also restore nvim plugins from the lockfile after linking
#
# POSIX sh on purpose: this runs before anything is installed, so it must not
# assume bash. install.sh next to this file, which does the linking, is bash
# and only ever runs from an already-cloned repo where bash is a safe bet.
#
# This script ships inside the repo it installs, so a clone carries its own
# bootstrap. Re-running the one-liner on an existing clone just pulls.

set -eu

REPO_SLUG="TowardInfinity/dotfiles"
REPO_SSH="git@github.com:${REPO_SLUG}.git"
REPO_HTTPS="https://github.com/${REPO_SLUG}.git"
REPO_TARBALL="https://codeload.github.com/${REPO_SLUG}/tar.gz/refs/heads/main"

DEPS=false
DRY=false
COPY=false
NVIM=false
for arg in "$@"; do
  case "$arg" in
    --deps) DEPS=true ;;
    --dry)  DRY=true ;;
    --copy) COPY=true ;;
    --nvim) NVIM=true ;;
    # Prints the header block above by *following* it — from line 2 until the
    # first line that is not a comment — rather than a hardcoded line range.
    # The range version silently truncated the help mid-sentence every time
    # the header grew by a line, which it did three times.
    -h|--help) awk 'NR>1 && /^#/ { sub(/^# ?/, ""); print; next } NR>1 { exit }' "$0"; exit 0 ;;
    *) echo "Unknown option: $arg" >&2; exit 1 ;;
  esac
done

# --nvim just gets forwarded — the configs repo's install.sh (bash) does the
# actual `:Lazy restore` call. Built as a plain string, not an array: this is
# POSIX sh, and an unquoted empty variable disappears in field splitting
# rather than passing through as an empty argument, so this stays a no-op
# when the flag was not given.
NVIM_ARGS=""
$NVIM && NVIM_ARGS="--nvim"

case "$(uname -s)" in
  Darwin) OS=macos; DEFAULT_DIR="$HOME/Codes/Projects/dotfiles" ;;
  Linux)  OS=linux; DEFAULT_DIR="$HOME/Codes/dotfiles" ;;
  *) echo "Unsupported OS: $(uname -s)" >&2; exit 1 ;;
esac
DEST="${DOTFILES_DIR:-$DEFAULT_DIR}"

say()  { printf '\033[36m==>\033[0m %s\n' "$1"; }
warn() { printf '\033[33m!!\033[0m  %s\n' "$1"; }
die()  { printf '\033[31mError:\033[0m %s\n' "$1" >&2; exit 1; }

say "os=$OS  dest=$DEST"
$DRY && warn "dry run — nothing will change"

# ── Clone transport ───────────────────────────────────────────
# The configs repo is public, so HTTPS always works with no account. This is
# only asking "could we do better than read-only?" — an SSH key on the account
# means the clone can push, which is what you want on a machine you actually
# edit configs from. Never fatal: no key just means read-only.
have_ssh_auth() {
  command -v ssh >/dev/null 2>&1 || return 1
  ssh -o BatchMode=yes -o ConnectTimeout=5 -o StrictHostKeyChecking=accept-new \
      -T git@github.com 2>&1 | grep -q "successfully authenticated"
}

if have_ssh_auth; then
  AUTH=ssh
  say "auth: ssh — clone will be pushable"
else
  AUTH=https
  say "auth: none — cloning read-only over https"
fi

# ── Packages (opt-in) ─────────────────────────────────────────
#
# The list is driven by what the shell configs actually reference. If .zshrc
# calls a tool, it belongs here — otherwise a fresh machine either errors on
# every prompt or silently loses a feature.
#
# Version managers (fnm, SDKMAN, uv) are installed rather than the languages
# themselves, so Node/Java versions stay per-project.

# run <description> <command...> — skips when --dry, so every install path
# gets a dry-run preview for free.
run() {
  desc=$1; shift
  if $DRY; then
    echo "    would $desc"
  else
    say "$(printf '%s' "$desc" | awk '{print toupper(substr($0,1,1)) substr($0,2)}')"
    "$@"
  fi
}

# have <tool> — true if the tool exists, checking the places these configs put
# things as well as PATH. A bare `command -v` gives false negatives here: a
# non-interactive shell has not sourced .zshrc, so ~/.local/bin, ~/go/bin and
# the pnpm dir are all missing from PATH, and we would reinstall tools that
# are already present.
have() {
  command -v "$1" >/dev/null 2>&1 && return 0
  for d in "$HOME/.local/bin" "$HOME/go/bin" "$HOME/.local/share/pnpm" \
           /usr/local/bin /usr/local/go/bin /opt/homebrew/bin; do
    [ -x "$d/$1" ] && return 0
  done
  return 1
}

install_macos_pkgs() {
  command -v brew >/dev/null 2>&1 || die "Homebrew missing — install it first: https://brew.sh"
  # Everything the macOS .zshrc references, plus the editor stack.
  #   fzf zoxide eza bat lazygit  -> aliases and keybindings in .zshrc
  #   fnm                         -> Node version manager
  #   uv pnpm go                  -> toolchains
  #   gh                          -> convenience only; this installer no longer
  #                                  needs it, the configs repo is public
  if $DRY; then
    echo "    would: brew install neovim tmux git curl gh jq ripgrep fd fzf zoxide eza bat lazygit btop glow uv pnpm fnm go"
    echo "    would: brew install --cask ghostty font-jetbrains-mono-nerd-font"
  else
    say "Installing packages (brew)"
    brew install neovim tmux git curl gh jq ripgrep fd fzf zoxide eza bat \
      lazygit btop glow uv pnpm fnm go
    brew install --cask ghostty font-jetbrains-mono-nerd-font || true
  fi
}

install_linux_pkgs() {
  if $DRY; then
    echo "    would: apt-get install tmux zsh git curl unzip build-essential python3-venv jq ripgrep"
    echo "    would: install Neovim, uv, fnm, pnpm, Go, glow, gh (apt's are missing or too old)"
    return 0
  fi

  say "Installing packages (apt)"
  sudo apt-get update -qq
  # python3-venv matters: without it Mason cannot install ruff.
  sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq \
    tmux zsh git curl unzip build-essential python3-venv jq ripgrep

  # Neovim — apt's is far too old for this config.
  need_nvim=true
  if command -v nvim >/dev/null 2>&1; then
    maj=$(nvim --version | head -1 | sed -E 's/[^0-9]*([0-9]+)\.([0-9]+).*/\1/')
    min=$(nvim --version | head -1 | sed -E 's/[^0-9]*([0-9]+)\.([0-9]+).*/\2/')
    if [ "$maj" -gt 0 ] || [ "$min" -ge 11 ]; then need_nvim=false; fi
  fi
  if $need_nvim; then
    say "Installing Neovim from the upstream tarball"
    tmp=$(mktemp -d)
    curl -fsSL -o "$tmp/nvim.tar.gz" \
      https://github.com/neovim/neovim/releases/latest/download/nvim-linux-x86_64.tar.gz
    sudo tar -C /opt -xzf "$tmp/nvim.tar.gz"
    sudo ln -sf /opt/nvim-linux-x86_64/bin/nvim /usr/local/bin/nvim
    rm -rf "$tmp"
  fi

  # uv — official installer, lands in ~/.local/bin
  have uv || { say "Installing uv"; curl -fsSL https://astral.sh/uv/install.sh | sh; }

  # fnm — .zshrc expects it at ~/.local/share/fnm
  [ -d "$HOME/.local/share/fnm" ] \
    || { say "Installing fnm"; curl -fsSL https://fnm.vercel.app/install | bash -s -- --skip-shell; }

  # pnpm — standalone installer, no Node needed first. Note it may already be
  # present as an fnm/corepack shim from a Node install, which `have` will see.
  have pnpm || { say "Installing pnpm"; curl -fsSL https://get.pnpm.io/install.sh | SHELL=/bin/sh sh -; }

  # Go — .zshrc puts /usr/local/go/bin on PATH
  if [ ! -x /usr/local/go/bin/go ]; then
    say "Installing Go"
    gov=$(curl -fsSL https://go.dev/VERSION?m=text | head -1)
    tmp=$(mktemp -d)
    curl -fsSL -o "$tmp/go.tar.gz" "https://go.dev/dl/${gov}.linux-amd64.tar.gz"
    sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf "$tmp/go.tar.gz"
    rm -rf "$tmp"
  fi

  # glow — the linux .zshrc pipes markdown through it in its `cat` wrapper
  have glow || {
    say "Installing glow"
    gv=$(curl -fsSL https://api.github.com/repos/charmbracelet/glow/releases/latest \
         | grep '"tag_name"' | head -1 | sed -E 's/.*"v?([^"]+)".*/\1/')
    tmp=$(mktemp -d)
    if curl -fsSL -o "$tmp/glow.deb" \
        "https://github.com/charmbracelet/glow/releases/latest/download/glow_${gv}_amd64.deb"; then
      sudo dpkg -i "$tmp/glow.deb" >/dev/null 2>&1 || warn "glow install failed — skipping"
    else
      warn "could not fetch glow — skipping"
    fi
    rm -rf "$tmp"
  }

  # gh — needed for --copy and as the clone fallback
  have gh || {
    say "Installing gh"
    sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq gh 2>/dev/null \
      || warn "gh not in apt — see https://cli.github.com if you want it"
  }
}

# Oh My Zsh is NOT an optional package — both .zshrc files hard-require it
# (`source $ZSH/oh-my-zsh.sh`), so linking a .zshrc without installing it gives
# you an error on every single shell start. That is why this runs on every
# install and not just under --deps, exactly like TPM in install.sh: both are
# runtime dependencies of a config being linked, not conveniences.
#
# KEEP_ZSHRC=yes is load-bearing. Without it Oh My Zsh's installer moves any
# existing ~/.zshrc to ~/.zshrc.pre-oh-my-zsh and drops its own template in
# place — which, once we have symlinked, means it silently replaces the symlink
# with a stock file and your config vanishes. With it, ours is left alone and
# the ordering of these two steps stops mattering.
install_omz() {
  if [ ! -d "$HOME/.oh-my-zsh" ]; then
    run "install Oh My Zsh" sh -c \
      'KEEP_ZSHRC=yes RUNZSH=no CHSH=no sh -c "$(curl -fsSL https://raw.githubusercontent.com/ohmyzsh/ohmyzsh/master/tools/install.sh)"'
  fi
  # These two are NOT bundled with Oh My Zsh, but linux/zsh/.zshrc lists them
  # in `plugins=(...)`. Without them zsh complains on every start. The macOS
  # .zshrc uses plugins=(git macos pip), so it does not need them.
  [ "$OS" = linux ] || return 0
  custom="${ZSH_CUSTOM:-$HOME/.oh-my-zsh/custom}"
  for plug in zsh-autosuggestions zsh-syntax-highlighting; do
    if [ ! -d "$custom/plugins/$plug" ]; then
      run "install $plug" git clone -q --depth 1 \
        "https://github.com/zsh-users/$plug" "$custom/plugins/$plug"
    fi
  done
}

if $DEPS; then
  say "Installing packages"
  if [ "$OS" = macos ]; then install_macos_pkgs; else install_linux_pkgs; fi
fi

# Always — see the comment on install_omz. A linked .zshrc without Oh My Zsh
# is a broken shell, so this is not something --deps should gate.
install_omz

# ── Copy mode: no repo kept ───────────────────────────────────
if $COPY; then
  say "Copy mode — no repo will be kept"
  if $DRY; then
    echo "    would: fetch tarball, copy configs into place, discard it"
    exit 0
  fi
  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' EXIT INT TERM
  say "Fetching $REPO_SLUG"
  # codeload serves public tarballs anonymously — no gh, no token, no git.
  curl -fsSL "$REPO_TARBALL" -o "$tmp/d.tar.gz" \
    || die "could not fetch $REPO_TARBALL"
  tar -xzf "$tmp/d.tar.gz" -C "$tmp"
  src=$(find "$tmp" -mindepth 1 -maxdepth 1 -type d | head -1)
  [ -d "$src" ] || die "tarball did not extract as expected"
  say "Copying configs"
  "$src/install.sh" --copy $NVIM_ARGS
  echo
  warn "Copied, not linked — these files are no longer tracked."
  warn "Re-run this command to update them."
  exit 0
fi

# ── Clone or update ───────────────────────────────────────────
if [ -d "$DEST/.git" ]; then
  say "Updating $DEST"
  $DRY || git -C "$DEST" pull --ff-only
else
  say "Cloning $REPO_SLUG -> $DEST"
  if $DRY; then
    echo "    would: clone via $AUTH"
  else
    mkdir -p "$(dirname "$DEST")"
    if [ "$AUTH" = ssh ]; then
      git clone "$REPO_SSH" "$DEST"
    else
      git clone "$REPO_HTTPS" "$DEST"
    fi
  fi
fi

# ── Link ──────────────────────────────────────────────────────
say "Linking configs"
if $DRY; then
  if [ -x "$DEST/install.sh" ]; then "$DEST/install.sh" --dry $NVIM_ARGS; else echo "    (would run install.sh)"; fi
else
  "$DEST/install.sh" $NVIM_ARGS
fi
