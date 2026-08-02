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
    # The documented form is `sh -c "$(curl ...)" -- --deps`, where the `--` is
    # eaten by the outer `sh -c`. Running this file directly with the same
    # syntax — the natural thing to do when testing a clone — passed the `--`
    # through and hit "Unknown option". Accept and ignore it.
    --) ;;
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

# The Linux installs fetch upstream tarballs, which are per-architecture. This
# used to hardcode x86_64/amd64 and the servers these configs target are Oracle
# Ampere boxes — arm64. The failure was silent and nasty: the amd64 asset always
# exists, so curl succeeded, tar succeeded, the symlink got made, and the first
# actual `nvim` invocation died with "Exec format error". `have()` only tests
# the executable bit, so even the verify step called it installed.
#
# Two spellings because upstreams disagree: Neovim ships nvim-linux-arm64 /
# nvim-linux-x86_64, while Go and glow use the Debian arm64/amd64 names.
case "$(uname -m)" in
  x86_64|amd64)   NVIM_ARCH=x86_64; GO_ARCH=amd64 ;;
  aarch64|arm64)  NVIM_ARCH=arm64;  GO_ARCH=arm64 ;;
  *) NVIM_ARCH=""; GO_ARCH="" ;;
esac

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
  # .local/share/fnm is where the Linux fnm installer puts the binary, and
  # where linux/zsh/.zshrc invokes it from by absolute path. Leaving it out
  # made verify() report fnm missing on Linux even straight after a successful
  # --deps run. macOS was unaffected because brew's fnm lands in /opt/homebrew/bin.
  for d in "$HOME/.local/bin" "$HOME/go/bin" "$HOME/.local/share/pnpm" \
           "$HOME/.local/share/fnm" \
           /usr/local/bin /usr/local/go/bin /opt/homebrew/bin; do
    [ -x "$d/$1" ] && return 0
  done
  return 1
}

# Homebrew is the one thing this script bootstraps on macOS, because nothing
# else on a fresh Mac can be installed without it. Two things used to go wrong:
#
#   1. `command -v brew` reported it missing on machines that had it. On Apple
#      Silicon brew lives in /opt/homebrew/bin, which is not on the PATH of the
#      non-login `sh` this script runs under — that directory only gets added
#      by .zprofile in an interactive shell. The `have` helper above already
#      knows to look there; this one call did not use it.
#   2. Missing brew called `die`, which killed the entire run — so an opt-in
#      --deps failure also took out the linking step, which had nothing to do
#      with packages and would have worked fine.
#
# Now: find it, else install it, else warn and let the rest of the run proceed.
ensure_brew() {
  if ! command -v brew >/dev/null 2>&1; then
    for b in /opt/homebrew/bin/brew /usr/local/bin/brew; do
      [ -x "$b" ] || continue
      eval "$("$b" shellenv)"
      break
    done
  fi
  command -v brew >/dev/null 2>&1 && return 0

  if $DRY; then echo "    would: install Homebrew first"; return 0; fi

  say "Installing Homebrew"
  # NONINTERACTIVE=1 stops it prompting for RETURN; it still asks for sudo once.
  NONINTERACTIVE=1 /bin/bash -c \
    "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)" \
    || { warn "Homebrew install failed — skipping packages"; return 1; }

  for b in /opt/homebrew/bin/brew /usr/local/bin/brew; do
    [ -x "$b" ] || continue
    eval "$("$b" shellenv)"
    break
  done
  command -v brew >/dev/null 2>&1 \
    || { warn "Homebrew installed but not on PATH — skipping packages"; return 1; }
}

install_macos_pkgs() {
  ensure_brew || return 0
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
    # `brew install a b c` exits non-zero if ANY formula fails, and `set -e`
    # turns that into a dead script — one unavailable package used to abort the
    # run before the configs were ever linked. Swallow it here; the verify step
    # at the end reports what actually ended up missing, which is the thing
    # worth knowing anyway.
    brew install neovim tmux git curl gh jq ripgrep fd fzf zoxide eza bat \
      lazygit btop glow uv pnpm fnm go \
      || warn "some formulae failed — see brew's output above"
    brew install --cask ghostty font-jetbrains-mono-nerd-font \
      || warn "some casks failed (already installed by hand?)"
  fi
}

install_linux_pkgs() {
  if $DRY; then
    echo "    would: apt-get install tmux zsh git curl unzip build-essential python3-venv jq ripgrep"
    echo "    would: install Neovim, uv, fnm, pnpm, Go, glow, gh (apt's are missing or too old)"
    return 0
  fi

  say "Installing packages (apt)"
  # Guarded like brew's. An apt failure — a network blip on a VM's first
  # update, one unavailable package, or no sudo at all on a minimal cloud
  # image — must not abort the run: linking the configs does not depend on
  # any of this, and losing it to an opt-in package step is the same bug that
  # `die`-on-missing-brew used to cause.
  sudo apt-get update -qq || warn "apt-get update failed — package installs may fail"
  # python3-venv matters: without it Mason cannot install ruff.
  sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq \
    tmux zsh git curl unzip build-essential python3-venv jq ripgrep \
    || warn "some apt packages failed — see above"

  [ -n "$NVIM_ARCH" ] \
    || warn "unrecognised architecture $(uname -m) — skipping nvim/go/glow tarballs"

  # Neovim — apt's is far too old for this config.
  need_nvim=true
  if command -v nvim >/dev/null 2>&1; then
    maj=$(nvim --version | head -1 | sed -E 's/[^0-9]*([0-9]+)\.([0-9]+).*/\1/')
    min=$(nvim --version | head -1 | sed -E 's/[^0-9]*([0-9]+)\.([0-9]+).*/\2/')
    if [ "$maj" -gt 0 ] || [ "$min" -ge 11 ]; then need_nvim=false; fi
  fi
  if $need_nvim && [ -n "$NVIM_ARCH" ]; then
    say "Installing Neovim from the upstream tarball ($NVIM_ARCH)"
    tmp=$(mktemp -d)
    if curl -fsSL -o "$tmp/nvim.tar.gz" \
        "https://github.com/neovim/neovim/releases/latest/download/nvim-linux-${NVIM_ARCH}.tar.gz"; then
      sudo tar -C /opt -xzf "$tmp/nvim.tar.gz"
      sudo ln -sf "/opt/nvim-linux-${NVIM_ARCH}/bin/nvim" /usr/local/bin/nvim
    else
      warn "could not fetch Neovim for $NVIM_ARCH — skipping"
    fi
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
  if [ ! -x /usr/local/go/bin/go ] && [ -n "$GO_ARCH" ]; then
    say "Installing Go"
    gov=$(curl -fsSL https://go.dev/VERSION?m=text | head -1)
    tmp=$(mktemp -d)
    if curl -fsSL -o "$tmp/go.tar.gz" "https://go.dev/dl/${gov}.linux-${GO_ARCH}.tar.gz"; then
      # Only remove the old install once the new tarball is actually in hand —
      # `rm -rf /usr/local/go` before a failed download leaves no Go at all.
      sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf "$tmp/go.tar.gz"
    else
      warn "could not fetch Go for $GO_ARCH — skipping"
    fi
    rm -rf "$tmp"
  fi

  # glow — the linux .zshrc pipes markdown through it in its `cat` wrapper
  if ! have glow && [ -n "$GO_ARCH" ]; then
    say "Installing glow"
    gv=$(curl -fsSL https://api.github.com/repos/charmbracelet/glow/releases/latest \
         | grep '"tag_name"' | head -1 | sed -E 's/.*"v?([^"]+)".*/\1/')
    tmp=$(mktemp -d)
    if curl -fsSL -o "$tmp/glow.deb" \
        "https://github.com/charmbracelet/glow/releases/latest/download/glow_${gv}_${GO_ARCH}.deb"; then
      sudo dpkg -i "$tmp/glow.deb" >/dev/null 2>&1 || warn "glow install failed — skipping"
    else
      warn "could not fetch glow — skipping"
    fi
    rm -rf "$tmp"
  fi

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
    # ZSH= is pinned explicitly because the Oh My Zsh installer honours an
    # exported $ZSH and will install there instead — and .zshrc exports it on
    # line 6, *before* sourcing, so it is set in any shell using these configs
    # even when Oh My Zsh is not actually installed. Without pinning, the
    # directory we test for and the directory it installs into can differ, and
    # the installer bails with "The $ZSH folder already exists".
    # --keep-zshrc and --unattended are passed as ARGUMENTS, not as the
    # KEEP_ZSHRC / RUNZSH / CHSH environment variables. Those env vars are
    # ignored by the current installer — verified directly: with KEEP_ZSHRC=yes
    # exported it still printed "Using the Oh My Zsh template file" and wrote
    # its own ~/.zshrc. With the flag it prints "Found .zshrc. Keeping..." and
    # leaves it alone.
    #
    # That matters because ~/.zshrc here is a symlink into the repo: the
    # template path would replace the symlink with a stock file and the tracked
    # config would silently stop being used. The empty "" is $0 for the script.
    run "install Oh My Zsh" sh -c \
      'ZSH="$HOME/.oh-my-zsh" sh -c "$(curl -fsSL https://raw.githubusercontent.com/ohmyzsh/ohmyzsh/master/tools/install.sh)" "" --keep-zshrc --unattended'
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

# Python tools that belong to uv rather than to the system package manager.
#
# JupyterLab specifically must NOT come from brew/apt. Those builds ship their
# own externally-managed Python, so the single kernel they register points into
# the package's own site-packages: you cannot install project dependencies into
# it, and an upgrade replaces the lot. `uv tool install` keeps one JupyterLab
# on PATH while each project registers its own venv as a kernel — one Jupyter,
# many kernels — which is the thing conda is usually dragged in to solve.
install_uv_tools() {
  have uv || { warn "uv not installed — skipping uv tools"; return 0; }
  if $DRY; then
    echo "    would: uv tool install jupyterlab"
    return 0
  fi
  # `uv tool install` is idempotent and cheap when already present.
  say "Installing uv tools (jupyterlab)"
  uv tool install --quiet jupyterlab || warn "uv tool install jupyterlab failed"
}

if $DEPS; then
  say "Installing packages"
  if [ "$OS" = macos ]; then install_macos_pkgs; else install_linux_pkgs; fi
  install_uv_tools
fi


# The list is what the linked configs actually CALL, taken from grepping each
# .zshrc — not the --deps package list. That distinction is the whole point:
# gating these checks on --deps meant a plain install linked a .zshrc whose
# aliases were already broken and said nothing, which is how
# `zsh: command not found: eza` got discovered by hand instead of reported here.
# A tool the config references is missing whether or not you asked for packages.
verify() {
  $DRY && return 0
  needed="zsh git nvim tmux"
  if [ "$OS" = macos ]; then
    needed="$needed bat eza fzf zoxide lazygit fnm"
  else
    needed="$needed glow go uv pnpm fnm"
  fi
  # Only expected when packages were requested — jupyter is not a shell
  # dependency, so its absence should not warn on a plain install.
  $DEPS && needed="$needed jupyter"

  missing=
  for t in $needed; do have "$t" || missing="$missing $t"; done
  [ -d "$HOME/.oh-my-zsh" ] || missing="$missing oh-my-zsh"

  [ -n "$missing" ] || return 0
  echo
  warn "not installed:$missing"
  if $DEPS; then
    warn "these should have installed — scroll up for the failure, then re-run"
  else
    warn "the linked configs call these, so some aliases will not work."
    warn "install them with:"
    warn "  sh -c \"\$(curl -fsSL https://toin.in/install)\" -- --deps"
  fi
}

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
  install_omz
  verify
  exit 0
fi

# ── Clone or update ───────────────────────────────────────────
# Both branches are guarded because `set -e` turns a git failure into a dead
# script, and the two most likely failures are both things a returning user
# hits rather than exotic edge cases:
#
#   pull  — you edited a config locally and committed, so history diverged and
#           --ff-only refuses. Re-running the installer should not be how you
#           discover that, and it certainly should not skip linking.
#   clone — $DEST exists but is not a git repo (a stray directory, an earlier
#           interrupted run, or DOTFILES_DIR pointing somewhere occupied).
#
# In both cases the existing checkout is left exactly as it is and the run
# continues to linking, which is still useful and cannot make things worse.
if [ -d "$DEST/.git" ]; then
  say "Updating $DEST"
  if ! $DRY && ! git -C "$DEST" pull --ff-only; then
    warn "could not fast-forward $DEST — leaving it alone"
    warn "linking will use the checkout as it stands; resolve with: git -C \"$DEST\" status"
  fi
else
  say "Cloning $REPO_SLUG -> $DEST"
  if $DRY; then
    echo "    would: clone via $AUTH"
  else
    mkdir -p "$(dirname "$DEST")"
    if [ "$AUTH" = ssh ]; then url="$REPO_SSH"; else url="$REPO_HTTPS"; fi
    git clone "$url" "$DEST" \
      || die "clone into $DEST failed — is that path already occupied? Set DOTFILES_DIR=... to use another."
  fi
fi

# Nothing below can work without the repo, so stop here rather than running
# install.sh from a path that may not exist.
$DRY || [ -x "$DEST/install.sh" ] \
  || die "$DEST/install.sh not found — the clone or update did not leave a usable repo"

# ── Link ──────────────────────────────────────────────────────
say "Linking configs"
if $DRY; then
  if [ -x "$DEST/install.sh" ]; then "$DEST/install.sh" --dry $NVIM_ARGS; else echo "    (would run install.sh)"; fi
else
  "$DEST/install.sh" $NVIM_ARGS
fi

# ── Verify ────────────────────────────────────────────────────
# Say what is missing NOW, at the end of the run, instead of letting you find
# out later as `zsh: command not found: eza` from an alias that looks broken.
# Checked by binary name, not formula name, because those differ (neovim/nvim,
# ripgrep/rg) and the formula being "installed" is not the question.

# Oh My Zsh last, deliberately.
#
# It is not optional — both .zshrc files source it, so a linked config without
# it is a broken shell, which is why this runs on every install and not only
# under --deps. But it has to run AFTER linking: --keep-zshrc keeps an existing
# ~/.zshrc, and on a fresh machine there is nothing there yet, so running it
# first meant it always wrote its own template, which linking then had to back
# up and replace. Link first and it finds our symlink, keeps it, and no stray
# backup is created at all.
install_omz

verify
